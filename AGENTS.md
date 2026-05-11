# AGENTS — acb-go

## What this is
Agent Communication Bus (ACB): a Go REST service that orchestrates tasks between autonomous agents and a central orchestrator. Not yet implemented — this repo holds the spec and plan.

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

## Directory structure (planned)
```
main.go
internal/
  config/   — env loader
  db/       — SQLite connection, schema, repos (tasks, gates, agents)
  api/      — chi router, handlers, middleware, auth
  redis/    — pub/sub client, event types
  models/   — Go structs for Task, Gate, Agent
cmd/        — future CLI entrypoints
```

## Implementation plan
See `IMPLEMENTATION_PLAN.md` for step-by-step tasks. Use `subagent-driven-development` skill to execute task-by-task with review.

## Prerequisites
- Go 1.22+
- Redis (Docker: `docker run -d --name redis -p 6379:6379 redis:7-alpine`)
- RustFS instance (or mock for dev)
