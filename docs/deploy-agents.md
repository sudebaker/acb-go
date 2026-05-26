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

acb-agent-poller.py (cada 15min)
  → Detecta tareas nuevas via dispatch + cambios de estado, silencioso sin cambios
```

Cada agente es un contenedor Docker corriendo Hermes Agent con:
- Su propio `config.yaml`
- Su propio `jobs.json` (cron tasks)
- Su propio `.env` con tokens y API keys
- Script `acb-agent-poller.py` para consultar tareas y detectar cambios

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

> Los tokens se configuran via `ACB_TOKEN` en el entorno del agente.

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
│   └── acb-agent-poller.py  # Script de polling + state tracking ACB
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

## 4. Instalar el Script ACB Agent Poller

El script `acb-agent-poller.py` se copia dentro del contenedor en `/opt/data/scripts/`:

> ⚠️ Hermes resuelve el campo `script` de jobs.json como `/opt/data/scripts/<script>`. Debe estar en ese subdirectorio.

```bash
# Desde el host, copiar el script
mkdir -p ~/src/{agent}/data/scripts/
cp scripts/acb-agent-poller.py ~/src/{agent}/data/scripts/acb-agent-poller.py
```

O mediante Docker:

```bash
docker exec {container} mkdir -p /opt/data/scripts/
docker cp acb-agent-poller.py {container}:/opt/data/scripts/acb-agent-poller.py
```

El script consulta el ACB por tareas asignadas al agente (via `GET /tasks/dispatch` y `GET /tasks`), compara con el estado anterior y solo genera output cuando hay cambios. Si no hay cambios, sale silenciosamente (sin output).

### Uso

```bash
ACB_TOKEN=<token> python3 /opt/data/scripts/acb-agent-poller.py <agent_name>
# Ejemplo: ACB_TOKEN=<AGENT_TOKEN> python3 /opt/data/scripts/acb-agent-poller.py agent-1
```

Output si hay cambios:

```
[DISPATCH] New task available: Implementar endpoint POST /upload
[NEW] Task "Fix DB migration" (pending) -> agent-1
[CHANGED] Task "Review PR #42": in_progress -> completed
```

Sin cambios: sin output (ideal para cronjobs silenciosos).

> **Critical rule:** Agents MUST mark tasks as `completed` or `failed` when done. Leaving tasks in `in_progress` after finishing work is a broken state. The HEARTBEAT.md in each agent enforces this cycle.

---

## 5. Crear el Cronjob ACB en cada Agente (Hermes)

Cada agente necesita un cronjob que ejecute el poller cada 15 minutos. Para plataforma Hermes, usar `provision-hermes-cron.sh`:

```bash
ACB_AGENT_NAME="agent-1" \
ACB_EXEC_PREFIX="docker exec agent-1-agent" \
ACB_CP_PREFIX="docker cp" \
ACB_CHAT_ID="123456789" \
ACB_CHAT_NAME="Chat Name" \
  ./scripts/provision-hermes-cron.sh
```

Esto crea/actualiza el cronjob en `/opt/data/cron/jobs.json` con el formato Hermes.

Variables:

| Variable | Default | Descripción |
|----------|---------|------------|
| `ACB_AGENT_NAME` | (requerido) | Nombre del agente |
| `ACB_EXEC_PREFIX` | (requerido) | Prefijo de ejecución (ej: `docker exec agent-1`) |
| `ACB_CP_PREFIX` | (requerido) | Prefijo de copia (ej: `docker cp`) |
| `ACB_CHAT_ID` | (requerido) | Chat ID de Telegram |
| `ACB_CHAT_NAME` | (requerido) | Nombre del chat |
| `ACB_CRON_EXPR` | `*/15 * * * *` | Expresión cron |
| `ACB_PLATFORM` | `telegram` | Plataforma de entrega |

> Después de crear el cronjob, el hook envía `SIGHUP` al PID 1 del contenedor. Si el contenedor no recarga con HUP, reiniciar manualmente:
> ```bash
> docker restart {container}
> ```

---

## 6. Script de Provisionamiento

Para automatizar el setup de un nuevo agente, usar `provision-agent.sh`.

**Capa genérica** — no sabe de Docker, Hermes ni Telegram. Recibe todo por entorno:

```bash
ACB_EXEC_PREFIX="docker exec agent-1-agent" \
ACB_CP_PREFIX="docker cp" \
ACB_CRON_HOOK="./scripts/provision-hermes-cron.sh" \
  ./scripts/provision-agent.sh agent-1
```

Variables de entorno:

| Variable | Default | Descripción |
|----------|---------|------------|
| `ACB_EXEC_PREFIX` | (requerido) | Cómo ejecutar comandos en el target |
| `ACB_CP_PREFIX` | (requerido) | Cómo copiar archivos al target |
| `ACB_CP_DEST` | `${AGENT_NAME}:` | Prefijo del destino. Docker: `"container:"`, SSH: `"user@host:"`, local: `""` |
| `ACB_CRON_HOOK` | `""` | Script hook para configurar el cron (ej: `provision-hermes-cron.sh`) |
| `ACB_URL` | `http://localhost:8090` | URL del ACB para healthcheck |
| `ACB_SCRIPTS_DIR` | `dirname $0` | Directorio con los scripts a copiar |

El script:
1. Verifica reachability del target
2. Verifica conectividad ACB desde el target
3. Crea `/opt/data/scripts/` en el target
4. Copia `acb-agent-poller.py`
5. Si `ACB_CRON_HOOK` está configurado, lo ejecuta con `ACB_AGENT_NAME`, `ACB_EXEC_PREFIX`, `ACB_CP_PREFIX` heredados

### Ejemplo con Docker:

```bash
export ACB_EXEC_PREFIX="docker exec agent-1-agent"
export ACB_CP_PREFIX="docker cp"
export ACB_CP_DEST="agent-1-agent:"
export ACB_CRON_HOOK="./scripts/provision-hermes-cron.sh"
export ACB_CHAT_ID="123456789"
export ACB_CHAT_NAME="Mi Chat"

./scripts/provision-agent.sh agent-1
```

### Ejemplo con SSH:

```bash
export ACB_EXEC_PREFIX="ssh user@agent-host"
export ACB_CP_PREFIX="scp"
export ACB_CP_DEST="user@agent-host:"

./scripts/provision-agent.sh agent-1
```

Sin `ACB_CRON_HOOK`, solo copia el script. El cron se configura externamente (crontab, systemd timer, K8s CronJob, etc.).

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
2. **Cada 15 minutos** → El cronjob del agente ejecuta `acb-agent-poller.py`
3. **Si hay tareas** → El agente lee el output y empieza a trabajar
4. **El agente actualiza estados** → `claim` → `start` → **`complete`/`fail`** ← **CRITICAL: must close the loop**
5. **acb-agent-poller.py** → Detecta cambios de estado y genera output para el cronjob

---

## 8. Troubleshooting

### El agente no recoge tareas

1. Verificar que el checker script existe:
   ```bash
    docker exec {container} ls -la /opt/data/scripts/acb-agent-poller.py
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

| Script | Función | Dónde corre | Método |
|--------|---------|-------------|--------|
| `acb-agent-poller.py` | Merge de checker + watcher — consulta tareas via dispatch, detecta cambios de estado, silencioso si no hay novedades | Dentro del agente (o host para `no_agent`) | Cron-driven |
| `provision-agent.sh` | Copia `acb-agent-poller.py` al target, verifica conectividad | Host | Manual o CI |
| `provision-hermes-cron.sh` | Crea/actualiza cronjob en Hermes `jobs.json` | Host | Hook de `provision-agent.sh` |

### Flujo de automatización

```
Orchestrator crea tarea → ACB guarda y dispatcha webhook al agente con skills match
                                              │
                                              ├── Canal 1: Webhook (from-orchestrator)
                                              │   Agente Hermes recibe notificación y procesa
                                              │
                                              └── Canal 2: Polling cada 15min
                                                  acb-agent-poller.py consulta dispatch + tareas

Agente: claim → start → work → complete/fail
                                    │
                          acb-agent-poller.py detecta cambios → output para cronjob
```

> **Diseño:** ACB hace dispatch por webhook cuando se crea una tarea. Los agentes tienen **dos canales** para recibirla: webhook directo y polling cada 15min como fallback.

### Tokens de autenticación

Todos los scripts usan `ACB_TOKEN` para el token del agente (ver `.env.example`):

| Rol | Variable | Descripción |
|-----|----------|-------------|
| Orchestrator | `ACB_ADMIN_TOKEN` | Token maestro (admin) |
| Agente | `ACB_TOKEN` | Token del agente (Bearer auth) |

> ⚠️ Todos los tokens se configuran mediante variables de entorno. No hay tokens hardcodeados en los scripts.

---

## 11. Ficheros Clave del Repo

```
acb-go/
├── scripts/
│   ├── acb-agent-poller.py     # Poller + state tracker (merge checker/watcher)
│   ├── gen-env.sh              # Generador de .env
│   ├── provision-agent.sh      # Provisionamiento genérico
│   ├── provision-hermes-cron.sh # Hook Hermes para cronjobs
│   └── test.sh                 # Tests E2E
├── docs/
│   ├── deploy-agents.md       # Esta guía
│   ├── api-reference.md       # Referencia API del ACB
│   ├── agent-integration.md   # Guía de integración de agentes
│   └── dispatcher-architecture.md
├── docker-compose.yml
├── .env.example
└── main.go
```

> **Nota:** Los scripts `acb-task-checker.py`, `acb-state-watcher.py`, `acb-status-monitor.py`, `acb-poll-and-notify.py` y `acb-notify-agents.sh` fueron eliminados. Sus funciones están consolidadas en `acb-agent-poller.py`.

## Notas

- El buzón de intercambio (`~/buzon_intercambio/`) está marcado como **FUTURE DEPRECATED**. Las comunicaciones entre agentes deben ir por ACB.
- Los deliverables se entregan como artifacts via `POST /tasks/{id}/artifacts` cuando sea posible.
- El buzón solo se mantiene para la transición.