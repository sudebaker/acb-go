# acb-go

**Agent Communication Bus (ACB)** — a Go REST service that orchestrates tasks between autonomous agents and a central orchestrator. It manages task lifecycle, state transitions, human-in-the-loop gates, and real-time event signaling.

## Architecture (Three Pillars)

| Pillar | Technology | Role |
|--------|-----------|------|
| **Persistence** | SQLite (`/var/lib/acb/acb.db`) | Durable state: tasks, gates, agents. WAL mode with busy timeout for concurrent reads. |
| **Signaling** | Redis Pub/Sub | Real-time event notifications (fire-and-forget, non-blocking). No state stored. |
| **Storage** | RustFS (S3-like) | Binary artifact storage via Bucket/Key model. |

## Quick Start

### Prerequisites
- Go 1.22+ (CGO required for `go-sqlite3`)
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
| `ACB_PORT` | `8080` | HTTP listen port |
| `ACB_DB_PATH` | `/var/lib/acb/acb.db` | SQLite database path |
| `ACB_REDIS_ADDR` | `localhost:6379` | Redis server address |
| `ACB_REDIS_PASS` | `` | Redis password (empty = no auth) |
| `ACB_RUSTFS_ENDPOINT` | `http://localhost:8085` | RustFS endpoint |
| `ACB_RUSTFS_BUCKET` | `acb-artifacts` | RustFS bucket name |
| `ACB_LOG_LEVEL` | `info` | Logging level |

## API Overview (11 endpoints)

### Tasks
| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/tasks` | Create a new task |
| `GET` | `/tasks` | List tasks (query: `?status=`, `?assignee=`) |
| `GET` | `/tasks/:id` | Get task details |
| `POST` | `/tasks/:id/claim` | Claim a task for an agent |
| `POST` | `/tasks/:id/start` | Start execution |
| `POST` | `/tasks/:id/block` | Block on a gate (human intervention) |
| `POST` | `/tasks/:id/unblock` | Unblock via gate answer |
| `POST` | `/tasks/:id/complete` | Complete with summary |
| `POST` | `/tasks/:id/fail` | Mark as failed |

### Agents
| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/agents/heartbeat` | Send heartbeat (rate-limited: 10/min) |
| `GET` | `/agents/:name` | Get agent status |

Full API reference at [`docs/api-reference.md`](docs/api-reference.md).

## Task State Machine

```
pending → claimed → in_progress → completed
                              ↘ blocked → in_progress
                              ↘ failed
```

Transitions enforce valid state changes server-side.

## Authentication

All endpoints except `/health` require a Bearer token:
```
Authorization: Bearer <agent-token>
```

Tokens are validated against the `agents` table. Register agents via the `agents` repository or direct SQL insert.

## Testing

```bash
go test ./...          # all tests (48+ tests)
go test -v ./internal/db/   # repository tests (SQLite, single-writer)
go test -v ./internal/api/  # handler + auth + rate limiter tests
go test -v ./tests/         # e2e lifecycle test
```

Tests require no external services. Redis tests nil-safe. DB tests use `t.TempDir()` for isolation.

## Project Structure

```
main.go              — entry point, wires everything
internal/
  config/config.go   — environment loader
  db/                — SQLite schema, migrations, TaskRepo, GateRepo, AgentRepo
  api/               — chi router, handlers, auth middleware, rate limiter, response helpers
  models/            — Task, Gate, Agent structs
  redis/             — Publisher with 7 event types, fire-and-forget
tests/
  e2e_test.go        — full task lifecycle integration test
docs/
  api-reference.md   — complete API documentation
  agent-integration.md — agent developer guide
```

## Docker

The `Dockerfile` uses multi-stage build with `alpine:3.19` runtime (CGO-compatible). See `docker-compose.yml` for the full stack (ACB + Redis + RustFS).

## License

MIT
