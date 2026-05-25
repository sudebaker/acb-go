# Especificación Técnica: Agent Communication Bus (ACB)

## 1. Visión General
El Agent Communication Bus (ACB) es un servicio independiente desarrollado en Go que actúa como centro neurálgico para la orquestación de tareas entre agentes autónomos y un orquestador central. 

El ACB no sustituye al MCP orchestrator; este último sigue proporcionando las herramientas técnicas (search, scrape, etc.), mientras que el ACB gestiona el ciclo de vida del flujo de trabajo, la trazabilidad de las tareas, las dependencias y la intervención humana mediante mecanismos de "gates".

### Pilares Arquitectónicos
- **Persistencia**: SQLite (`acb.db`) para el estado duradero de tareas y auditoría.
- **Señalización**: Redis Pub/Sub para notificaciones en tiempo real (sin almacenamiento de estado).
- **Almacenamiento**: RustFS (S3-like) para la gestión estructurada de artefactos voluminosos o binarios.

---

## 2. Modelo de Datos (SQLite)

El archivo de base de datos se ubicará en `/var/lib/acb/acb.db`.

### 2.1 Tabla `tasks`
| Campo | Tipo | Descripción |
|---|---|---|
| `id` | TEXT (PK) | UUID de la tarea (ej. `t_a1b2c3d4`). |
| `title` | TEXT | Nombre breve de la tarea. |
| `assignee` | TEXT | Agente asignado. **NULL** hasta que un agente reclame la tarea. |
| `status` | TEXT | `pending`, `claimed`, `in_progress`, `blocked`, `completed`, `failed`. |
| `priority` | INTEGER | Importancia (1-5). |
| `required_skills` | TEXT (JSON) | Skills necesarios para ejecutar la tarea (ej. `["docker","python"]`). El ACB verifica que el agente que reclama tenga todos estos skills. |
| `tags` | TEXT (JSON) | Categorización flexible (ej. `["urgent","production"]`). |
| `parents` | TEXT (JSON) | IDs de tareas predecesoras necesarias. |
| `body_goal` | TEXT | Definición del objetivo. |
| `body_context` | TEXT | Contexto necesario para la ejecución. |
| `body_deliverable_format` | TEXT | Formato esperado (`markdown`, `csv`, `binary`). |
| `body_deliverable_path` | TEXT | Ruta en RustFS o Shared FS. |
| `created_at` | TEXT | ISO8601 timestamp. |
| `summary` | TEXT | Resumen final del resultado. |
| `artifacts_json` | TEXT (JSON) | Lista de artefactos generados `{key, bucket, size}`. |

### 2.2 Tabla `gates`
| Campo | Tipo | Descripción |
|---|---|---|
| `gate_id` | TEXT (PK) | Identificador único del punto de decisión. |
| `task_id` | TEXT (FK) | Referencia a la tarea. |
| `question` | TEXT | Pregunta específica que requiere resolución humana. |
| `ask` | TEXT | Destinatario de la pregunta (normalmente `human`). |
| `status` | TEXT | `pending`, `asked`, `answered`, `resolved`. |
| `answer` | TEXT | Respuesta recibida. |

### 2.3 Tabla `agents`
| Campo | Tipo | Descripción |
|---|---|---|
| `name` | TEXT (PK) | Nombre del agente. |
| `port` | INTEGER | Puerto HTTP expuesto por el agente (opcional). |
| `token` | TEXT | Token Bearer para autenticación con el ACB. |
| `skills` | TEXT (JSON) | Capacidades del agente, definidas en el despliegue (ej. `["docker","linux","osint","python"]`). No autodeclarado por el agente. |
| `last_heartbeat` | TEXT | Último timestamp de señal de vida. |

---

## 3. Ciclo de Vida y Flujo de Eventos

### 3.1 Flujo Nominal
1. **Creación**: El orquestador envía `POST /tasks` con `required_skills` y sin `assignee`. ACB inserta registro → `status=pending` → Publica en Redis `tasks:pending` (broadcast a todos los agentes).
2. **Reclamación validada**: Cualquier agente puede intentar reclamar con `POST /tasks/:id/claim`. El ACB verifica que el agente autenticado tenga **todos** los skills en `required_skills`. Si no cumple → `403 Forbidden`. Si cumple → `status=claimed`, `assignee=<agent_name>`. El claim es atómico: otro agente que intente reclamar recibe `409 Conflict`.
3. **Ejecución**: Agente llama a `POST /tasks/:id/start` → `status=in_progress`.
4. **Bloqueo (Gate)**: Agente llama a `POST /tasks/:id/block`.
   - Estado → `blocked`.
   - Notificación → Redis `tasks:gates` → Intervención humana → `POST /tasks/:id/unblock`.
5. **Finalización**: Agente llama a `POST /tasks/:id/complete` → Adjunta resumen y claves de artefactos → `status=completed`.

### 3.2 Notificaciones Redis (JSON)
Ejemplos de mensajes transportados por el bus:

- `{"event":"new_task","task_id":"t_123","required_skills":["docker","python"]}`
- `{"event":"task_blocked","task_id":"t_123","gate_id":"g1"}`
- `{"event":"task_completed","task_id":"t_123","summary":"Análisis finalizado"}`

**Canales de publicación:**
| Canal | Propósito | Suscriptor típico |
|-------|-----------|-------------------|
| `tasks:pending` | Broadcast de todas las tareas nuevas | Todos los agentes |
| `agent:<name>` | Notificaciones dirigidas a un agente concreto | Agente específico |
| `tasks:gates` | Tareas bloqueadas esperando resolución humana | Orquestadores, notificadores |

---

## 4. Contratos de API (REST)

### 4.1 Gestión de Tareas
- **`POST /tasks`**: Crea una nueva tarea.
- **`POST /tasks/:id/claim`**: Reclama la tarea para el agente actual. El ACB verifica que el agente autenticado tenga **todos** los skills de `required_skills`. Si no → `403 Forbidden`.
- **`POST /tasks/:id/start`**: Marca el inicio efectivo del trabajo.
- **`POST /tasks/:id/block`**: Solicita resolución de un gate.
- **`POST /tasks/:id/unblock`**: Proporciona la respuesta al gate (Usado por el orquestador).
- **`POST /tasks/:id/complete`**: Finaliza la tarea con éxito.
- **`POST /tasks/:id/fail`**: Marca la tarea como fallida con motivo.

**Ejemplo de Error (409 Conflict):**
```json
{
  "error": "task_already_claimed",
  "message": "La tarea t_123 ya ha sido reclamada por el agente 'agent-a'",
  "current_status": "in_progress"
}
```

### 4.2 Gestión de Agentes
- **`POST /agents/heartbeat`**: Envía señal de vida del agente.
- **`GET /agents/:name`**: Consulta estado y puerto de un agente específico.

---

## 5. Defensa de RustFS como Storage

RustFS (S3-compatible) se incluye como componente opcional para la gestión de artefactos por las siguientes razones técnicas:

1. **Abstracción de Direccionamiento**: Sustituye rutas absolutas frágiles por un modelo de `Bucket/Key`. Esto evita conflictos de nombrado y errores de ruta entre diferentes agentes.
2. **Integridad de Binarios**: RustFS garantiza que los artefactos pesados (modelos, dumps, imágenes) sean tratados como objetos inmutables una vez subidos, evitando la corrupción por escrituras concurrentes.
3. **Escalabilidad y Auditoría**: Permite implementar cuotas de almacenamiento y políticas de retención (TTL) automatizadas, separando el almacenamiento de artefactos del sistema de archivos del SO.

---

## 6. Implementación y Seguridad
- **Autenticación**: Uso de tokens Bearer únicos por agente, validados en cada petición al ACB.
- **Aislamiento**: El ACB no modifica el MCP Orchestrator; se comunica con él mediante el flujo estándar de herramientas.
- **Despliegue**: Los agentes se ejecutan en contenedores Docker con un UID compartido para garantizar la propiedad de archivos en volúmenes montados.

---

## 7. Configuración de SQLite

- **WAL mode**: `PRAGMA journal_mode=WAL` — permite lecturas concurrentes sin bloqueos de escritura.
- **Busy timeout**: `PRAGMA busy_timeout=5000` — espera hasta 5s antes de abortar por bloqueo.
- **Single-writer**: `SetMaxOpenConns(1)` — SQLite no soporta escritura concurrente segura.
- **Índice**: `idx_agents_last_heartbeat` en `agents(last_heartbeat)` para consultas eficientes de agentes inactivos.

---

## 8. Rate Limiting

El endpoint `POST /agents/heartbeat` está limitado a **10 peticiones por minuto por agente**.

Implementado con `golang.org/x/time/rate`:
- **Rate**: `rate.Every(6 * time.Second)` (1 cada 6s)
- **Burst**: 1
- **Aislamiento**: cada agente tiene su propio limitador

Respuesta `429 Too Many Requests`:
```json
{"error": "rate_limited", "message": "too many heartbeats"}
```

---

## 9. Optimización N+1

Todos los métodos de transición de estado (`ClaimTask`, `StartTask`, `BlockTask`, `CompleteTask`, `FailTask`) retornan `(*models.Task, error)` con un objeto mínimo construido en memoria después del UPDATE, eliminando la necesidad de un SELECT posterior para obtener el estado actualizado. Esto reduce las queries de N+1 a 1 por transición.

---

## 10. Seguridad — Post-Audit

Hallazgos críticos corregidos tras el security audit:

| Hallazgo | Severidad | Corrección |
|----------|-----------|------------|
| `CompleteTask` sin verificar RowsAffected | CRÍTICA | Se agregó verificación `RowsAffected() == 0` → error |
| `FailTask` permitía fallar desde cualquier estado | CRÍTICA | Se agregó `AND status = 'in_progress'` |
| `ClaimTask` ignoraba errores JSON | MEDIA | Se agregó validación `Decode` con error 400 |
| `StartTask` ignoraba errores JSON | MEDIA | Se agregó validación `Decode` con error 400 |
| Auth bypass por path no normalizado | BAJA | Se agregó `/health/` al bypass |
| `GetByName` exponía el token | BAJA | `agent.Token = ""` antes de retornar |
| Dockerfile usaba `scratch` sin CGO | ALTA | Se cambió a `alpine:3.19` |

