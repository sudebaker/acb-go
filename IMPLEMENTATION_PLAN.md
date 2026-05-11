# ACB (Agent Communication Bus) — Implementation Plan

> **Status:** All phases complete.
> **Repo:** github.com/sudebaker/acb-go

**Goal:** Build the Agent Communication Bus — a Go service that orchestrates tasks across autonomous agents with SQLite persistence, Redis signaling, and RustFS artifact storage.

**Architecture:** Three pillars: SQLite for durable state (tasks, gates, agents), Redis Pub/Sub for real-time event notification between agents, and RustFS for binary artifact storage. REST API exposed on a configurable port with Bearer token auth per agent.

**Tech Stack:** Go 1.22+, `github.com/mattn/go-sqlite3`, `github.com/go-chi/chi/v5`, `github.com/redis/go-redis/v9`, `golang.org/x/time/rate`

---

## Phase 0: Project Scaffolding ✅

### Task 0.1: Initialize Go module and install dependencies

**Status:** Complete

**Files:**
- `go.mod` — module `github.com/sudebaker/acb-go`, Go 1.24
- `main.go` — entry point wiring DB → repos → Redis → router → HTTP
- `internal/config/config.go` — env loader (`config.Load()`)

### Commands
```bash
go build ./...               # verify compilation
go test ./...                # all tests (48+)
```

---

## Phase 1: Data Layer — SQLite Schema + Repositories ✅

### Task 1.1: SQLite schema and migration runner

**Status:** Complete

**Files:**
- `internal/db/schema.go` — `RunMigrations()`: 3 tables (tasks, gates, agents) with CHECK constraints, plus index `idx_agents_last_heartbeat` on `agents(last_heartbeat)`
- `internal/db/db.go` — `Open()` with `SetMaxOpenConns(1)`, `PRAGMA journal_mode=WAL`, `PRAGMA busy_timeout=5000`
- `internal/db/schema_test.go`

### Task 1.2: Task repository

**Status:** Complete

**Files:**
- `internal/models/task.go` — `Task` struct with Artifact slice
- `internal/db/task_repo.go` — CRUD + 5 state transitions returning `(*models.Task, error)`
- `internal/db/task_repo_test.go` — 10 test cases

Transition methods return minimal `Task` object (id + status + assignee/summary where applicable) built from in-memory data — no extra SELECT after UPDATE (N+1 fix).

### Task 1.3: Gates repository

**Status:** Complete

**Files:**
- `internal/models/gate.go` — `Gate` struct
- `internal/db/gate_repo.go` — `CreateGate`, `GetByTaskID`, `AnswerGate`, `ResolveGate`
- `internal/db/gate_repo_test.go`

### Task 1.4: Agents repository

**Status:** Complete

**Files:**
- `internal/models/agent.go` — `Agent` struct
- `internal/db/agent_repo.go` — `UpsertAgent`, `GetByName` (clears token), `GetByToken`, `UpdateHeartbeat`, `ListStale`
- `internal/db/agent_repo_test.go`

---

## Phase 2: REST API Layer ✅

### Task 2.1: HTTP router and middleware

**Status:** Complete

**Files:**
- `internal/api/router.go` — chi router with middleware stack
- `internal/api/middleware.go` — JSON content-type, request logging, panic recovery
- `internal/api/response.go` — `WriteJSON`, `WriteError`, `ErrorResponse`
- `internal/api/router_test.go`

### Task 2.2: Task handlers

**Status:** Complete

**Files:**
- `internal/api/task_handler.go` — 9 handlers: Create, List, Get, Claim, Start, Block, Unblock, Complete, Fail
- `internal/api/task_handler_test.go` — 14 test cases

### Task 2.3: Agent handlers

**Status:** Complete

**Files:**
- `internal/api/agent_handler.go` — `Heartbeat` (with rate limiter), `GetAgent` (clears token)
- `internal/api/limiter.go` — per-agent `rate.Limiter` (10/min), `golang.org/x/time/rate`
- `internal/api/limiter_test.go`

---

## Phase 3: Redis Integration ✅

### Task 3.1: Redis Pub/Sub client

**Status:** Complete

**Files:**
- `internal/redis/events.go` — `Publisher` with 7 event types, nil-safe, fire-and-forget via goroutine
- `internal/redis/events_test.go` — nil-safe (no Redis needed for CI)

### Task 3.2: Wire Redis into task lifecycle

**Status:** Complete

Redis events published after every state transition in `internal/api/task_handler.go`. Publish errors are logged via `log.Printf` (non-blocking).

---

## Phase 4: Auth Middleware ✅

### Task 4.1: Bearer token authentication

**Status:** Complete

**Files:**
- `internal/api/auth.go` — `AuthMiddleware(agentRepo)`, bypasses `/health` and `/health/`
- `internal/api/auth_test.go` — validates token, rejects invalid/missing, health bypass

---

## Phase 5: Wiring and Boot ✅

### Task 5.1: Wire everything in main.go

**Status:** Complete

**File:** `main.go` — connects: config → DB (migrate) → Redis → repos → publisher → router → HTTP server

### Task 5.2: Docker and env template

**Status:** Complete

**Files:**
- `.env.example` — all 7 env vars with defaults
- `Dockerfile` — multi-stage, `alpine:3.19` runtime (CGO-compatible)
- `docker-compose.yml` — ACB + Redis + RustFS stack

### Task 5.3: Integration test

**Status:** Complete

**File:** `tests/e2e_test.go` — full lifecycle: create → claim → start → block → unblock → complete

---

## Post-Implementation

### Security Audit

A security audit of all 14 commits identified and fixed 9 findings:

| Severity | Finding | Fix |
|----------|---------|-----|
| CRITICAL | CompleteTask silently succeeded on tasks not in_progress | Added RowsAffected check |
| CRITICAL | FailTask allowed failing from ANY state | Added `AND status = 'in_progress'` |
| MEDIUM | ClaimTask ignored JSON decode errors | Added validation, returns 400 |
| MEDIUM | StartTask ignored JSON decode errors | Added validation, returns 400 |
| MEDIUM | Other 3 handlers ignored JSON decode errors (CompleteTask, FailTask, BlockTask) | Added validation, returns 400 |
| LOW | Auth bypass with non-normalized `/health/` path | Added `/health/` to bypass list |
| LOW | GetByName exposed token in response | Token cleared before return |
| HIGH | Dockerfile `scratch` incompatible with CGO sqlite3 | Changed to `alpine:3.19` |

### Performance: N+1 Query Fix

All 5 transition methods (`ClaimTask`, `StartTask`, `BlockTask`, `CompleteTask`, `FailTask`) refactored to return `(*models.Task, error)`. Handlers use the returned task directly instead of calling `GetByID` after each transition, eliminating the extra SELECT.

### Go Module Hygiene

- All dependencies correctly classified as direct/indirect via `go mod tidy`
- `google/uuid` and `joho/godotenv` removed (not imported directly)
- `go 1.22` upgraded to `go 1.25` based on current Go SDK

---

## Phase 6: Documentation ✅

### Task 6.1: API documentation

**Files:**
- `README.md` — ✅ Project overview, setup, API reference summary, testing
- `docs/api-reference.md` — ✅ Complete API docs with all 15 endpoints (11 original + 4 artifact), request/response schemas, error codes, Redis events, cURL examples

### Task 6.2: Agent integration guide

**Files:**
- `docs/agent-integration.md` — ✅ Agent lifecycle, heartbeat protocol, gate workflow, artifact upload, Redis subscription, error handling, Python examples

### Task 6.3: Specification updates

**Files:**
- `ACB_SPECIFICATION.md` — ✅ Updated with WAL mode, rate limiting, N+1 fix, security audit, index

---

## Phase 7: RustFS Artifact Storage ✅

### Task 7.1: Config — RustFS credentials

**Status:** Complete

**Files:**
- `internal/config/config.go` — ✅ Added `RustFSRegion`, `RustFSAccessKey`, `RustFSSecretKey`
- `.env.example` — ✅ Added `RUSTFS_REGION`, `RUSTFS_ACCESS_KEY_ID`, `RUSTFS_SECRET_ACCESS_KEY`

### Task 7.2: RustFS Client

**Status:** Complete

**Files:**
- `internal/rustfs/client.go` — ✅ `Client` struct wrapping `ObjectStore` interface, nil-safe for disabled client
- `internal/rustfs/client_test.go` — ✅ In-memory store tests, nil-client tests, full lifecycle

`ObjectStore` interface: `Upload`, `Download`, `Delete`, `Exists`, `BucketExists`, `MakeBucket`

Implementations:
- `minioStore` — production, uses `minio-go/v7` S3-compatible client
- `memoryStore` — test, uses in-memory map

Key naming: `{task_id}/{uuid}_{filename}`

Content-Type: auto-detected via `http.DetectContentType()` on first 512 bytes.

### Task 7.3: TaskRepo — Artifact tracking

**Status:** Complete

**Files:**
- `internal/models/task.go` — ✅ Added `ContentType` field to `Artifact` struct
- `internal/db/task_repo.go` — ✅ Added `AddArtifact`, `RemoveArtifact`, `GetArtifacts`
- `internal/db/task_repo_test.go` — ✅ 5 test cases

### Task 7.4: Artifact HTTP Handler

**Status:** Complete

**Files:**
- `internal/api/artifact_handler.go` — ✅ 4 handlers (upload via multipart, list, download via query param `?key=`, delete via query param)
- `internal/api/artifact_handler_test.go` — ✅ 9 test cases

Endpoints:
| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/tasks/{id}/artifacts` | Upload artifact (multipart) |
| `GET` | `/tasks/{id}/artifacts` | List all artifacts |
| `GET` | `/tasks/{id}/artifacts?key=...` | Download specific artifact |
| `DELETE` | `/tasks/{id}/artifacts?key=...` | Delete specific artifact |

### Task 7.5: Wire into main.go + router.go

**Status:** Complete

**Files:**
- `main.go` — ✅ Creates RustFS client from config, calls `EnsureBucket()` on startup, passes to router
- `internal/api/router.go` — ✅ Updated `NewRouter` signature, registers artifact routes

### Task 7.6: Docker compose + gen-env.sh

**Status:** Complete

**Files:**
- `scripts/gen-env.sh` — ✅ Generates random RustFS credentials
- `docker-compose.yml` — ✅ Updated `rustfs` service with env vars for access keys

### Task 7.7: Documentation updates

**Status:** Complete

**Files:**
- `docs/api-reference.md` — ✅ Artifact endpoints section with request/response examples
- `docs/agent-integration.md` — ✅ "Uploading Artifacts" section with Python examples
- `docs/api-reference.md` — ✅ Error codes updated

---

## Summary

| Phase | Tasks | Status |
|-------|-------|--------|
| 0. Scaffolding | 1 task | ✅ Complete |
| 1. Data Layer | 4 tasks | ✅ Complete |
| 2. REST API | 3 tasks | ✅ Complete |
| 3. Redis | 2 tasks | ✅ Complete |
| 4. Auth | 1 task | ✅ Complete |
| 5. Wiring | 3 tasks | ✅ Complete |
| 6. Docs | 3 tasks | ✅ Complete |
| 7. RustFS | 7 tasks | ✅ Complete |

**Total:** ~24 tasks, ~57+ tests, RustFS integration via minio-go, S3-compatible storage.

