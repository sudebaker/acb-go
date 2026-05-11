# AGENTS — acb-go

## What this is
Agent Communication Bus (ACB): a Go REST service that orchestrates tasks between autonomous agents and a central orchestrator.

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
- **Redis events**: fire-and-forget via goroutine, non-blocking
- **Task states**: `pending → claimed → in_progress → blocked → completed/failed`
- **Env vars**: `ACB_PORT`, `ACB_DB_PATH`, `ACB_REDIS_ADDR`, `ACB_REDIS_PASS`, `ACB_RUSTFS_ENDPOINT`, `ACB_RUSTFS_BUCKET`, `ACB_LOG_LEVEL`

## Directory structure
```
main.go                  — entry point, wires DB → repos → Redis → router → HTTP
internal/
  config/                — env loader (config.Load())
  db/                    — SQLite Open/ path, RunMigrations, TaskRepo, GateRepo, AgentRepo
  api/                   — NewRouter, chi handlers, response helpers, AuthMiddleware
  redis/                 — NewPublisher, PublishTaskEvent (fire-and-forget)
  models/                — Task, Gate, Agent structs
tests/
  e2e_test.go            — full task lifecycle integration test
```

## Commands
```bash
go test ./...               # all tests (24 db + 20 api + 3 redis + 1 e2e)
go test ./internal/db/ -v   # repository tests (single-writer SQLite)
go test ./internal/api/ -v  # handler + auth tests
go test ./tests/ -v          # e2e lifecycle test
go build ./...               # verify compilation
```

## Testing quirks
- All db tests use `t.TempDir()` for isolated SQLite files
- API tests register a test agent (`test-agent` / `test-token`) for auth; use `authRequest()` helper
- Redis tests are nil-safe (no Redis server needed for CI)
- e2e test does NOT require Redis or RustFS (publisher handles nil client)

## Prerequisites
- Go 1.22+ (CGO required for `go-sqlite3`)
- Redis (Docker: `docker run -d --name redis -p 6379:6379 redis:7-alpine`)
- RustFS instance (or mock for dev)
