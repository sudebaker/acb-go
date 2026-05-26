# AGENTS — acb-go

## What this is
Agent Communication Bus (ACB): a Go REST service that orchestrates tasks between autonomous agents and a central orchestrator, with automatic webhook dispatch and smart polling fallback.

## Architecture (3 pillars)
- **SQLite** (`/var/lib/acb/acb.db`) — durable state: tasks, gates, agents
- **Redis Pub/Sub** — real-time event signaling (no state storage)
- **RustFS** (S3-like) — artifact storage via Bucket/Key model

## Tech stack
Go 1.22+, `go-sqlite3`, `chi/v5`, `go-redis/v9`, `google/uuid`, `godotenv`

## Key conventions from the plan
- **TDD per task**: write failing test → implement → pass → commit
- **Single DB connection**: `SetMaxOpenConns(1)` (SQLite single-writer)
- **Auth**: Bearer token per agent, validated against `agents` table; `/health` is public
- **Skills**: Agents have skills set at deployment time; tasks declare `required_skills` → ACB validates on claim (403 if insufficient). Skills are constrained to a fixed catalog via `ACB_ALLOWED_SKILLS` env var (comma-separated). Both task creation and agent registration reject skills not in the catalog (400).
- **Pending timeout**: Unclaimed tasks auto-expire after `ACB_PENDING_TIMEOUT_MIN` minutes (default 15). A background goroutine checks every `ACB_PENDING_TIMEOUT_CHECK_SEC` seconds (default 60) and transitions stale `pending` tasks to `failed`.
- **Dispatch**: Hybrid push webhook + pull polling. ACB matches agents by skills and POSTs to `webhook_url` on task creation. Failed webhooks retry via Redis list with exponential backoff. `GET /tasks/dispatch` for agents that prefer polling.
- **SSRF protection**: Webhook URLs validated at registration — private IPs rejected, scheme enforced, DNS resolution checked.
- **Task states**: `pending → claimed → in_progress → blocked → completed/failed`
- **Env vars**: `ACB_PORT`, `ACB_DB_PATH`, `ACB_REDIS_ADDR`, `ACB_REDIS_PASS`, `ACB_RUSTFS_ENDPOINT`, `ACB_RUSTFS_BUCKET`, `ACB_MAX_UPLOAD_SIZE_MB`, `ACB_ARTIFACT_TTL_DAYS`, `ACB_LOG_LEVEL`, `ACB_PENDING_TIMEOUT_MIN`, `ACB_PENDING_TIMEOUT_CHECK_SEC`, `ACB_ALLOWED_SKILLS`, `ACB_ALLOWED_TAGS`

## Directory structure
```
main.go                  — entry point, wires DB → repos → Redis → dispatcher → router → HTTP
internal/
  config/                — env loader (config.Load()), skill validation helpers
  db/                    — SQLite Open/ path, RunMigrations, TaskRepo, GateRepo, AgentRepo
  api/                   — NewRouter, chi handlers, response helpers, AuthMiddleware
  dispatcher/             — webhook push dispatcher with SSRF validation + retry queue
    dispatcher.go        — Dispatcher struct, DispatchToAgents(), retry goroutine
    validator.go         — SSRF validation (private IP denylist, scheme check, DNS resolution)
    dispatcher_test.go   — unit tests for dispatch and validation
  timeout/               — PendingTimeoutService, auto-expires unclaimed tasks
    timeout.go           — goroutine with configurable interval and timeout
    timeout_test.go      — unit tests for timeout service
  models/                — Task, Gate, Agent structs
  redis/                 — NewPublisher, PublishTaskEvent (fire-and-forget)
tests/
  e2e_test.go            — full task lifecycle integration test
docs/
  api-reference.md       — complete API documentation
  agent-integration.md   — agent developer guide
  dispatcher-architecture.md — dispatch design decision record
```

## Commands
```bash
go test ./...               # all tests (db + api + redis + dispatcher + e2e)
go test ./internal/db/ -v   # repository tests (single-writer SQLite)
go test ./internal/api/ -v  # handler + auth + rate limiter tests
go test ./tests/ -v          # e2e lifecycle test
go build ./...               # verify compilation
```

## Testing quirks
- All db tests use PostgreSQL via `pgx`; set `ACB_PG_HOST`/`ACB_PG_PORT` env vars (default `localhost:5433`)
- API tests register a test agent (`test-agent` / `test-token`) for auth; use `authRequest()` helper
- Redis tests are nil-safe (no Redis server needed for CI)
- e2e test does NOT require Redis or RustFS (publisher handles nil client)

## Security notes
- Webhook URL validation prevents SSRF (rejects private IPs, enforces http/https scheme)
- Agent registration checks X-Agent-Name to prevent token overwrite
- HMAC webhook signatures include timestamp for replay protection
- Tokens hashed with Argon2id before storage (S01 — resolved)
- Redis password warning logged at startup if ACB_REDIS_PASS is empty (S03 — mitigated)

## Prerequisites
- Go 1.22+ (CGO required for `go-sqlite3`)
- Redis (Docker: `docker run -d --name redis -p 6379:6379 redis:7-alpine`)
- RustFS instance (or mock for dev)