# Deploy de Agentes con ACB

Guía completa para poner en marcha los agentes Hermes con integración ACB (Agent Communication Bus).

---

## Arquitectura General

```
┌─────────────┐     REST API      ┌─────────┐
│ Orchestrator│ ──────────────────→│   ACB   │
│ (Orquestador│     localhost:8090  │  (Go)   │
│             │ ←──── state ──────│         │
└──────┬──────┘     watcher        └────┬────┘
       │                                │
       │  Dispatch por webhook          │ Cada agente hace
       │  + polling cada 15min          │ claim/start/complete
       │                                │
  ┌────┴──────┬──────────┬──────────┐
  │           │          │          │
  ▼           ▼          ▼          ▼
Agent-1     Agent-2   Agent-3
(coding)    (infra)    (osint)

acb-state-watcher (cada 15min, no_agent)
  → Notifica al operador solo cambios reales (nueva/completada/fallida/blocked)
```

Cada agente es un contenedor Docker corriendo Hermes Agent con:
- Su propio `config.yaml`
- Su propio `jobs.json` (cron tasks)
- Su propio `.env` con tokens y API keys
- Script `acb-task-checker.py` para consultar tareas

---

## Prerrequisitos

- Docker + Docker Compose
- Redis (contenedor `redis` en red compartida)
- ACB corriendo (`acb-service` en `localhost:8090`)
- Agentes registrados en la BD del ACB (tabla `agents`)

---

## 1. Desplegar el ACB

```bash
cd ~/src/acb-go
cp .env.example .env
# Editar .env con los valores de producción
docker compose up -d --build
```

Variables clave del `.env`:

| Variable | Default | Descripción |
|----------|---------|------------|
| `ACB_PORT` | `8090` | Puerto HTTP del ACB |
| `ACB_DB_PATH` | `/var/lib/acb/acb.db` | Path de la base de datos SQLite |
| `ACB_REDIS_ADDR` | `localhost:6379` | Dirección de Redis |
| `ACB_REDIS_PASS` | | Contraseña Redis (vacío = sin auth) |
| `ACB_ALLOW_HTTP_WEBHOOKS` | `0` | **Importante:** poner a `1` en red interna |
| `ACB_LOG_LEVEL` | `info` | Nivel de log |

> ⚠️ En red interna (Docker host network), los webhooks usan `http://`. Si no pones `ACB_ALLOW_HTTP_WEBHOOKS=1`, el dispatcher rechaza las URLs HTTP.

---

## 2. Registrar Agentes en el ACB

Cada agente necesita un token Bearer y sus skills registrados en la BD:

```sql
-- Conectarse a la BD del ACB
sqlite3 /var/lib/acb/acb.db

-- Registrar cada agente (nombres genéricos)
INSERT INTO agents (name, port, token, skills, webhook_url)
VALUES ('agent-1', 8647, '<AGENT_1_TOKEN>', '["coding","security","go","testing","devops","python"]', 'http://localhost:8647/webhook/orchestrator');

INSERT INTO agents (name, port, token, skills, webhook_url)
VALUES ('agent-2', 8645, '<AGENT_2_TOKEN>', '["sysadmin","coding","docker","linux","review","security","infra","go"]', 'http://localhost:8645/webhook/orchestrator');

INSERT INTO agents (name, port, token, skills, webhook_url)
VALUES ('agent-3', 8646, '<AGENT_3_TOKEN>', '["osint","hacking","security","research","celery"]', 'http://localhost:8646/webhook/orchestrator');
```

> Los tokens deben coincidir exactamente con lo que usa cada agente en `acb-task-checker.py`.

---

## 3. Configurar cada Agente

### Estructura de directorios

```
~/src/{agent}/
├── docker-compose.yml
├── Dockerfile
├── .env                    # API keys, modelos
├── data/                   # HERMES_HOME (montado como /opt/data)
│   ├── config.yaml         # Config Hermes (modelo, tools, cron)
│   ├── SOUL.md              # Personalidad del agente
│   ├── USER.md              # Info del usuario
│   ├── cron/
│   │   ├── jobs.json        # Cronjobs (incluido ACB checker)
│   │   ├── output/          # Output de ejecuciones
│   │   └── .tick.lock
│   ├── HEARTBEAT.md         # Instrucciones del heartbeat
│   └── acb-task-checker.py  # Script de polling ACB
```

### docker-compose.yml (ejemplo: Agent-1)

```yaml
services:
  agent-1:
    build:
      context: .
      dockerfile: Dockerfile
    container_name: agent-1
    restart: unless-stopped
    env_file:
      - .env
    environment:
      HERMES_HOME: /opt/data
      SEARXNG_URL: http://localhost:8081
    network_mode: "host"
    volumes:
      - ./data:/opt/data
      - /home/amphora/src:/home/amphora/src:rw
      - /home/amphora/buzon_intercambio:/opt/buzon:rw
    command: ["gateway", "run"]
```

> Importante: `network_mode: "host"` permite a los agentes reachear `localhost:8090` (ACB).

### config.yaml — Ajustes clave

```yaml
cron_mode: allow          # Permite que Hermes ejecute cronjobs
```

---

## 4. Instalar el Script ACB Task Checker

El script `acb-task-checker.py` se copia dentro del contenedor en `/opt/data/scripts/`:

> ⚠️ Hermes resuelve el campo `script` de jobs.json como `/opt/data/scripts/<script>`. Debe estar en ese subdirectorio.

```bash
# Desde el host, copiar el script
mkdir -p ~/src/{agent}/data/scripts/
cp scripts/acb-task-checker.py ~/src/{agent}/data/scripts/acb-task-checker.py
```

O mediante Docker:

```bash
docker exec {container} mkdir -p /opt/data/scripts/
docker cp acb-task-checker.py {container}:/opt/data/scripts/acb-task-checker.py
```

El script consulta el ACB por tareas asignadas al agente y las muestra en formato legible. Si no hay tareas, sale silenciosamente (sin output).

### Uso

```bash
python3 /opt/data/scripts/acb-task-checker.py <agent_name>
# Ejemplo: python3 /opt/data/scripts/acb-task-checker.py agent-1
```

The script shows active tasks with clear status indicators and includes the specific curl commands each agent needs to close the loop (claim → start → complete/fail).

> **Critical rule:** Agents MUST mark tasks as `completed` or `failed` when done. Leaving tasks in `in_progress` after finishing work is a broken state. The HEARTBEAT.md in each agent enforces this cycle.

---

## 5. Crear el Cronjob ACB en cada Agente

Cada agente necesita un cronjob que ejecute el checker cada 15 minutos. El formato es el de Hermes `jobs.json`:

### Opción A: Editar jobs.json directamente

Añadir a `/opt/data/cron/jobs.json`:

```json
{
  "id": "<unique-id>",
  "name": "acb-task-check",
  "prompt": "Execute: python3 /opt/data/scripts/acb-task-checker.py <AGENT_NAME>. If no tasks (script returns nothing), respond [SILENT]. If tasks found, work on them. CRITICAL: when done, mark task completed: curl -X POST http://localhost:8090/tasks/<TASK_ID>/complete -H 'Authorization: Bearer <YOUR_TOKEN>' -H 'Content-Type: application/json' -d '{\"summary\": \"what you did\"}'. NEVER leave tasks in_progress after finishing. Always close the loop: claim-start-complete/fail. Report progress.",
  "skills": [],
  "skill": null,
  "model": null,
  "provider": null,
  "base_url": null,
  "script": "acb-task-checker.py",
  "context_from": null,
  "schedule": {
    "kind": "cron",
    "expr": "*/15 * * * *",
    "display": "*/15 * * * *"
  },
  "schedule_display": "*/15 * * * *",
  "repeat": { "times": null, "completed": 0 },
  "enabled": true,
  "state": "scheduled",
  "deliver": "origin",
  "origin": {
    "platform": "telegram",
    "chat_id": "<CHAT_ID>",
    "chat_name": "",
    "thread_id": null
  }
}
```

### Opción B: Crear via Docker exec

```bash
# Copiar script al contenedor
docker cp scripts/acb-task-checker.py {container}:/opt/data/acb-task-checker.py

# Escribir el jobs.json (python genera el JSON con ID único)
# Ver sección "Script de Provisionamiento" abajo
```

> Después de modificar `jobs.json`, reiniciar el contenedor para que Hermes recarga el scheduler:
> ```bash
> docker restart {container}
> ```

---

## 6. Script de Provisionamiento

Para automatizar el setup de un nuevo agente, usar `provision-agent.sh`:

```bash
#!/bin/bash
# provision-agent.sh <agent_name> <chat_id>
# Ejemplo: ./provision-agent.sh agent-1 <CHAT_ID>

AGENT_NAME="$1"
CHAT_ID="$2"
CONTAINER="${AGENT_NAME}-agent"
ACB_URL="http://localhost:8090"

if [ -z "$AGENT_NAME" ] || [ -z "$CHAT_ID" ]; then
    echo "Uso: $0 <agent_name> <chat_id>"
    exit 1
fi

# 1. Copiar el checker script
docker cp scripts/acb-task-checker.py "${CONTAINER}:/opt/data/acb-task-checker.py"
docker exec "$CONTAINER" chmod +x /opt/data/acb-task-checker.py

# 2. Generar y escribir el cronjob
python3 -c "
import json, uuid
job_id = uuid.uuid4().hex[:12]
with open('/tmp/acb_cron_job.json') as f:
    template = json.load(f)
template['id'] = job_id
template['name'] = 'acb-task-check'
template['prompt'] = f'Execute: python3 /opt/data/scripts/acb-task-checker.py {AGENT_NAME}. If no tasks, respond [SILENT]. If tasks found, work on them. CRITICAL: when done, mark completed: curl -X POST http://localhost:8090/tasks/<TASK_ID>/complete -H "Authorization: Bearer <YOUR_TOKEN>" -H "Content-Type: application/json" -d \'{{"summary": "what you did"}}\'. NEVER leave tasks in_progress. Always: claim-start-complete/fail.'
template['schedule'] = {'kind': 'cron', 'expr': '*/15 * * * *', 'display': '*/15 * * * *'}
template['schedule_display'] = '*/15 * * * *'
template['origin']['chat_id'] = '${CHAT_ID}'

# Leer jobs existentes
import subprocess
result = subprocess.run(['docker', 'exec', '${CONTAINER}', 'cat', '/opt/data/cron/jobs.json'], capture_output=True, text=True)
if result.returncode == 0 and result.stdout.strip():
    data = json.loads(result.stdout)
else:
    data = {'jobs': []}

# Añadir nuevo job
data['jobs'].append(template)

with open('/tmp/acb_cron_jobs.json', 'w') as f:
    json.dump(data, f, indent=2, ensure_ascii=False)
"
docker cp /tmp/acb_cron_jobs.json "${CONTAINER}:/opt/data/cron/jobs.json"

# 3. Reiniciar el contenedor
docker restart "$CONTAINER"

echo "✅ Agente ${AGENT_NAME} provisionado con ACB checker cada 15min"
```

---

## 7. Flujo de Trabajo

### Crear una tarea (orquestador)

```bash
curl -X POST http://localhost:8090/tasks \
  -H "Authorization: Bearer <ACB_ADMIN_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Implementar endpoint POST /upload",
    "required_skills": ["go", "coding"],
    "priority": 3,
    "body_goal": "Añadir endpoint POST /upload al MCP Orchestrator",
    "body_context": "Repo: ~/src/mcp-orchestrator",
    "body_deliverable_format": "pull request"
  }'
```

### El agente reclama la tarea

```bash
curl -X POST http://localhost:8090/tasks/{task_id}/claim \
  -H "Authorization: Bearer <AGENT_TOKEN>"
```

### El agente marca como completada

```bash
curl -X POST http://localhost:8090/tasks/{task_id}/complete \
  -H "Authorization: Bearer <AGENT_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{"summary": "Endpoint implementado y testeado"}'
```

### Ciclo automático

1. **Orchestrator crea tarea** → ACB guarda tarea y dispatcha webhook al agente
2. **Cada 15 minutos** → El cronjob del agente ejecuta `acb-task-checker.py`
3. **Si hay tareas** → El agente lee el output y empieza a trabajar
4. **El agente actualiza estados** → `claim` → `start` → **`complete`/`fail`** ← **CRITICAL: must close the loop**
5. **acb-state-watcher** → Detecta cambio de estado y notifica al operador por Telegram

---

## 8. Troubleshooting

### El agente no recoge tareas

1. Verificar que el checker script existe:
   ```bash
   docker exec {container} ls -la /opt/data/acb-task-checker.py
   ```

2. Verificar que el cronjob está en `jobs.json`:
   ```bash
   docker exec {container} cat /opt/data/cron/jobs.json | python3 -m json.tool | grep -A5 acb
   ```

3. Verificar conectividad al ACB desde el contenedor:
   ```bash
   docker exec {container} curl -s http://localhost:8090/health
   ```

4. Verificar que `cron_mode: allow` está en el config:
   ```bash
   docker exec {container} grep cron_mode /opt/data/config.yaml
   ```

### Webhooks fallan con "unknown platform"

Los webhooks del ACB usan `deliver: log` por defecto. Para que un webhook genere una sesión en el agente, debe configurarse como plataforma válida en el config del agente (`platforms.webhook`).

### El dispatcher rechaza URLs HTTP

En red interna, poner `ACB_ALLOW_HTTP_WEBHOOKS=1` en el `.env` del ACB.

---

## 9. Puertos de los Agentes

| Agente | Puerto Gateway | Token ACB |
|--------|---------------|-----------|
| Agent-1 (coding) | 8647 | `<ACB_AGENT_1_TOKEN>` |
| Agent-2 (infra) | 8645 | `<ACB_AGENT_2_TOKEN>` |
| Agent-3 (osint) | 8646 | `<ACB_AGENT_3_TOKEN>` |

El orquestador usa `ACB_ADMIN_TOKEN` para crear tareas y consultar estado (no es agente registrado).

Todos usan `network_mode: host` → comparten `localhost`.

> Los tokens se generan mediante el script `scripts/gen-env.sh` o la API admin del ACB.

---

## 10. Scripts de Automatización

El ecosistema ACB tiene dos componentes de automatización:

| Script | Función | Dónde corre | Método |
|--------|---------|-------------|--------|
| `acb-state-watcher.py` | Notifica al operador (vía Telegram) solo cuando hay cambios reales en tareas (nueva, completada, fallida, blocked) | Host (orchestrator) | `no_agent`, script-only |
| `acb-task-checker.py` | Checker pasivo — cada agente consulta sus tareas asignadas | Dentro de cada contenedor de agente | LLM-driven |

### Flujo de automatización

```
Orchestrator crea tarea → ACB guarda y dispatcha webhook al agente con skills match
                                              │
                                              ├── Canal 1: Webhook (from-orchestrator)
                                              │   Agente Hermes recibe notificación y procesa
                                              │
                                              └── Canal 2: Polling cada 15min
                                                  acb-task-checker.py consulta tareas del agente

Agente: claim → start → work → complete/fail
                                    │
                         acb-state-watcher detecta cambio → Notifica al operador por Telegram
```

> **Diseño:** ACB hace dispatch por webhook cuando se crea una tarea. Los agentes tienen **dos canales** para recibirla: webhook directo y polling cada 15min como fallback. El orquestador solo recibe notificaciones cuando algo cambia (no spam).

### Tokens de autenticación

Todos los scripts usan variables de entorno para los tokens (ver `.env.example`):

| Rol | Variable de entorno | Descripción |
|-----|---------------------|-------------|
| Orchestrator | `ACB_ADMIN_TOKEN` | Token maestro (admin) |
| Agent-2 (infra) | `ACB_AGENT_2_TOKEN` | Token de agente |
| Agent-3 (osint) | `ACB_AGENT_3_TOKEN` | Token de agente |
| Agent-1 (coding) | `ACB_AGENT_1_TOKEN` | Token de agente |

> ⚠️ Todos los tokens se configuran mediante variables de entorno (ver `.env.example`). No hay tokens hardcodeados en los scripts.

### Script del orquestador (host)

`acb-state-watcher.py` corre en el host como cronjob de Hermes (`no_agent=true`). Compara el estado actual del ACB con el estado anterior (`/tmp/acb-watcher-state.json`) y solo genera output cuando hay cambios — sin cambios, es silencioso (no notifica). Se encuentra en `~/.hermes/scripts/`.

---

## 11. Ficheros Clave del Repo

```
acb-go/
├── scripts/
│   ├── acb-state-watcher.py   # Notificador de cambios (reemplaza 3 scripts eliminados)
│   ├── acb-task-checker.py    # Checker pasivo para agentes
│   ├── gen-env.sh             # Generador de .env
│   ├── provision-agent.sh     # Provisionamiento de nuevos agentes
│   └── test.sh                # Tests E2E
├── docs/
│   ├── deploy-agents.md       # Esta guía
│   ├── api-reference.md       # Referencia API del ACB
│   ├── agent-integration.md   # Guía de integración de agentes
│   └── dispatcher-architecture.md
├── docker-compose.yml
├── .env.example
└── main.go
```

> **Nota:** Los scripts `acb-status-monitor.py`, `acb-poll-and-notify.py` y `acb-notify-agents.sh` fueron eliminados en mayo 2026. Sus funciones están consolidadas en `acb-state-watcher.py`.

## Notas

- El buzón de intercambio (`~/buzon_intercambio/`) está marcado como **FUTURE DEPRECATED**. Las comunicaciones entre agentes deben ir por ACB.
- Los deliverables se entregan como artifacts via `POST /tasks/{id}/artifacts` cuando sea posible.
- El buzón solo se mantiene para la transición.