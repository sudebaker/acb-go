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
| `assignee` | TEXT | Nombre del agente asignado (ej. `agent-a`, `agent-b`). |
| `status` | TEXT | `pending`, `claimed`, `in_progress`, `blocked`, `completed`, `failed`. |
| `priority` | INTEGER | Importancia (1-5). |
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
| `port` | INTEGER | Puerto HTTP expuesto por el agente. |
| `token` | TEXT | Token Bearer para autenticación con el ACB. |
| `last_heartbeat` | TEXT | Último timestamp de señal de vida. |

---

## 3. Ciclo de Vida y Flujo de Eventos

### 3.1 Flujo Nominal
1. **Creación**: El orquestador envía `POST /tasks`. ACB inserta registro $\\rightarrow$ `status=pending` $\\rightarrow$ Publica en Redis `agent:<name>`.
2. **Reclamación**: Agente llama a `POST /tasks/:id/claim` $\rightarrow$ `status=claimed`.
3. **Ejecución**: Agente llama a `POST /tasks/:id/start` $\rightarrow$ `status=in_progress`.
4. **Bloqueo (Gate)**: Agente llama a `POST /tasks/:id/block`.
   - Estado $\rightarrow$ `blocked`.
- Notificación $\\rightarrow$ Redis `orchestrator` $\\rightarrow$ Intervención humana $\\rightarrow$ `POST /tasks/:id/unblock`.
5. **Finalización**: Agente llama a `POST /tasks/:id/complete` $\rightarrow$ Adjunta resumen y claves de artefactos $\rightarrow$ `status=completed`.

### 3.2 Notificaciones Redis (JSON)
Ejemplos de mensajes transportados por el bus:
- `{"event":"new_task", "task_id":"t_123"}`
- `{"event":"task_blocked", "task_id":"t_123", "gate_id":"g1"}`
- `{"event":"task_completed", "task_id":"t_123", "summary":"Análisis finalizado"}`

---

## 4. Contratos de API (REST)

### 4.1 Gestión de Tareas
- **`POST /tasks`**: Crea una nueva tarea.
- **`POST /tasks/:id/claim`**: Reclama la tarea para el agente actual.
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

A pesar de existir un sistema de archivos compartido (`/opt/data`), se mantiene la implementación de RustFS por las siguientes razones técnicas:

1. **Abstracción de Direccionamiento**: Sustituye rutas absolutas frágiles por un modelo de `Bucket/Key`. Esto evita conflictos de nombrado y errores de ruta entre diferentes agentes.
2. **Integridad de Binarios**: RustFS garantiza que los artefactos pesados (modelos, dumps, imágenes) sean tratados como objetos inmutables una vez subidos, evitando la corrupción por escrituras concurrentes.
3. **Escalabilidad y Auditoría**: Permite implementar cuotas de almacenamiento y políticas de retención (TTL) automatizadas, separando el almacenamiento de artefactos del sistema de archivos del SO.

---

## 6. Implementación y Seguridad
- **Identidad**: Todos los agentes operan bajo `uid 1000` para asegurar la propiedad de archivos en los volúmenes montados.
- **Autenticación**: Uso de tokens Bearer únicos por agente, validados en cada petición al ACB.
- **Aislamiento**: El ACB no modifica el MCP Orchestrator; se comunica con él mediante el flujo estándar de herramientas.
