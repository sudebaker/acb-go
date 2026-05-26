# acb-go

**Agent Communication Bus (ACB)** — a Go REST service that orchestrates tasks between autonomous agents and a central orchestrator. It manages task lifecycle, state transitions, human-in-the-loop gates, real-time event signaling, and automatic task dispatch via webhooks.

## Architecture (Three Pillars)

| Pillar | Technology | Role |
|--------|-----------|------|
| **Persistence** | PostgreSQL | Durable state: tasks, gates, agents. |
| **Signaling** | Redis Pub/Sub | Real-time event notifications (fire-and-forget, non-blocking). No state stored. |
| **Storage** | RustFS (S3-like) | Binary artifact storage via Bucket/Key model. |

## Quick Start

### Prerequisites
- Go 1.22+
- PostgreSQL 16+
- Redis 7+ (`docker run -d --name redis -p 6379:6379 redis:7-alpine`)
- RustFS instance (optional for dev)

### Native
```bash
cp .env.example .env        # edit as needed
go build -o acb ./main.go
./acb
```

### Docker
```bash
docker compose up --build
```

## Configuration

All environment variables are optional (defaults shown):

| Variable | Default | Description |
|----------|---------|-------------|
| `ACB_PORT` | `8090` | HTTP listen port |
| `ACB_PG_HOST` | `localhost` | PostgreSQL host |
| `ACB_PG_PORT` | `5433` | PostgreSQL port |
| `ACB_PG_USER` | `acb` | PostgreSQL user |
| `ACB_PG_PASSWORD` | `acb-secure-pg-pass-2026` | PostgreSQL password |
| `ACB_PG_DATABASE` | `acb` | PostgreSQL database name |
| `ACB_REDIS_ADDR` | `localhost:6379` | Redis server address |
| `ACB_REDIS_PASS` | `` | Redis password (empty = no auth) |
| `ACB_RUSTFS_ENDPOINT` | `http://localhost:8085` | RustFS endpoint |
| `ACB_RUSTFS_BUCKET` | `acb-artifacts` | RustFS bucket name |
| `ACB_MAX_UPLOAD_SIZE_MB` | `32` | Max artifact upload size in MB |
| `ACB_ARTIFACT_TTL_DAYS` | `30` | Days before artifacts are auto-cleaned |
| `ACB_LOG_LEVEL` | `info` | Logging level |

## API Overview (17 endpoints)

### Tasks
| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/tasks` | Create a new task |
| `GET` | `/tasks` | List tasks (query: `?status=`, `?assignee=`) |
| `GET` | `/tasks/dispatch` | Get next matching task for an agent (smart polling) |
| `GET` | `/tasks/:id` | Get task details |
| `GET` | `/tasks/:id/gates` | List gates for a task |
| `POST` | `/tasks/:id/claim` | Claim a task for an agent |
| `POST` | `/tasks/:id/start` | Start execution |
| `POST` | `/tasks/:id/block` | Block on a gate (human intervention) |
| `POST` | `/tasks/:id/unblock` | Unblock via gate resolution |
| `POST` | `/tasks/:id/gates/:gate_id/answer` | Submit gate answer (agent) |
| `POST` | `/tasks/:id/gates/:gate_id/approve` | Approve gate answer (orchestrator) |
| `POST` | `/tasks/:id/complete` | Complete with summary |
| `POST` | `/tasks/:id/fail` | Mark as failed |

### Agents
| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/agents` | Register or update an agent (with webhook_url) |
| `POST` | `/agents/heartbeat` | Send heartbeat (rate-limited: 10/min) |
| `GET` | `/agents/:name` | Get agent status |

### Artifacts
| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/tasks/:id/artifacts` | Upload artifact |
| `GET` | `/tasks/:id/artifacts` | List or download artifacts |

Full API reference at [`docs/api-reference.md`](docs/api-reference.md).

## Task State Machine

```
pending → claimed → in_progress → completed
                             ↘ blocked → in_progress (via unblock)
                             ↘ failed

Gate lifecycle (when blocked):
pending → asked (agent answers) → answered (orchestrator approves) → resolved (unblock)
```

Transitions enforce valid state changes server-side.

## Task Dispatch

ACB uses a **hybrid webhook push + smart polling** model:

### Push (automatic)
When a task is created, ACB looks up agents whose `skills` match the task's `required_skills` and POSTs the task to their `webhook_url`. The POST includes an `X-Webhook-Signature` header (HMAC-SHA256 of the body with the agent's `webhook_secret`).

```json
POST <agent_webhook_url>
X-Webhook-Signature: <hex_hmac>
{
  "action": "new_task",
  "task": { "id": "...", "title": "...", "required_skills": [...] },
  "timestamp": "2026-05-16T21:00:00Z"
}
```

Failed webhooks are queued in Redis (`acb:retry:<agent_name>`) with exponential backoff (5s, 25s, 125s). Max 5 retries.

### Pull (fallback)
Agents without a `webhook_url` can poll for tasks:

```
GET /tasks/dispatch?agent=<name>
```

Returns the best-matching pending task for the agent based on skills. Marks the task as `dispatched` to prevent duplicates.

### SSRF Protection
Webhook URLs are validated at registration time:
- Must use `http://` or `https://` scheme
- Resolved IPs are checked against private ranges (RFC 1918, loopback, link-local)
- Timestamp included in HMAC signature for replay protection (5-minute window)

## Authentication

All endpoints except `/health` require a Bearer token:
```
Authorization: Bearer <agent-token>
```

Tokens are validated against the `agents` table. Register agents via `POST /agents` or direct SQL insert.

**Security:** Agents can only register themselves (X-Agent-Name must match the authenticated agent name). Token overwrite is prevented.

## Agent Registration

```json
POST /agents
{
  "name": "braulio",
  "port": 8645,
  "token": "braulio-token",
  "skills": ["go", "testing", "devops"],
  "webhook_url": "http://localhost:8645/webhooks/amanda",
  "webhook_secret": "<WEBHOOK_SECRET>"
}
```

## Testing

```bash
go test ./...          # all tests (48+ tests)
go test -v ./internal/db/   # repository tests (PostgreSQL, set ACB_PG_* vars)
go test -v ./internal/api/  # handler + auth + rate limiter tests
go test -v ./tests/         # e2e lifecycle test
```

Tests require a running PostgreSQL (set `ACB_PG_HOST`, `ACB_PG_PORT`, `ACB_PG_USER`, `ACB_PG_PASSWORD`, `ACB_PG_DATABASE`). Redis tests are nil-safe.

## Project Structure

```
main.go              — entry point, wires DB → repos → Redis → dispatcher → router → HTTP
internal/
  config/config.go   — environment loader
   db/                — PostgreSQL migrations, TaskRepo, GateRepo, AgentRepo
  api/               — chi router, handlers, auth middleware, rate limiter, response helpers
  dispatcher/        — webhook push dispatcher with SSRF validation + retry queue
  models/            — Task, Gate, Agent structs
   redis/             — Publisher with 9 event types, fire-and-forget
tests/
```
docs/
  api-reference.md       — complete API documentation
  agent-integration.md   — agent developer guide (API flows)
  deploy-agents.md       — full agent deployment & ACB cron setup guide
  dispatcher-architecture.md — dispatch design decision record
scripts/
  acb-agent-poller.py         — agent task poller + state tracker (silent if no changes)
  acb-orchestrator-poller.py  — orchestrator task monitor + auto-approve gates
  provision-agent.sh          — generic agent provisioning (platform-agnostic)
  provision-hermes-cron.sh    — Hermes-specific cron job setup hook
  simulate-orchestration.py   — e2e orchestration simulation
```

## Docker

The `Dockerfile` uses multi-stage build with `alpine:3.19` runtime (CGO-compatible). See `docker-compose.yml` for the full stack.

**Security:** Container runs as non-root user `acb:1000`. Healthcheck uses BusyBox-compatible `wget`. CA certificates included for TLS support.

## Known Issues & Security Audit

See [Security Audit Report](docs/security-audit.md) for the full findings.

| # | Severity | Issue | Status |
|---|----------|-------|--------|
| S01 | CRITICAL | Tokens stored in plaintext | 🔴 Open |
| S02 | CRITICAL | SSRF in webhook dispatch | ✅ Fixed (validator.go) |
| S03 | CRITICAL | Redis without auth by default | 🔴 Open |
| S04 | HIGH | HMAC without replay protection | ✅ Fixed (timestamp in signature) |
| S05 | HIGH | Agent token overwrite via upsert | ✅ Fixed (X-Agent-Name check) |
| S06 | MEDIUM | LIKE injection in skill filtering | 🔴 Open |
| S07 | MEDIUM | No security HTTP headers | 🔴 Open |
| S08 | LOW | SQL query logged in production | 🔴 Open |
| S09 | LOW | No TLS on HTTP server | 🔴 Open (use reverse proxy) |

## License

MIT