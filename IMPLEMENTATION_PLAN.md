# ACB (Agent Communication Bus) — Implementation Plan

> **For implementation:** Use subagent-driven-development skill to execute this plan task-by-task with two-stage review.

**Goal:** Build the Agent Communication Bus — a Go service that orchestrates tasks across autonomous agents with SQLite persistence, Redis signaling, and RustFS artifact storage.

**Architecture:** Three pillars: SQLite for durable state (tasks, gates, agents), Redis Pub/Sub for real-time event notification between agents, and RustFS for binary artifact storage. REST API exposed on a configurable port with Bearer token auth per agent.

**Tech Stack:** Go 1.22+, `github.com/mattn/go-sqlite3`, `github.com/go-chi/chi/v5`, `github.com/redis/go-redis/v9`, `github.com/google/uuid`

---

## Phase 0: Project Scaffolding

### Task 0.1: Initialize Go module and install dependencies

**Objective:** Create the Go project structure with all dependencies declared.

**Files:**
- Create: `/home/amphora/src/acb/go.mod`
- Create: `/home/amphora/src/acb/main.go`
- Create: `/home/amphora/src/acb/internal/config/config.go`

**Step 1: Create project directory and initialize module**

```bash
mkdir -p /home/amphora/src/acb/{internal/{config,db,api,auth,redis,models},cmd}
cd /home/amphora/src/acb
go mod init github.com/amphora/acb
```

**Step 2: Install dependencies**

```bash
go get github.com/go-chi/chi/v5
go get github.com/mattn/go-sqlite3
go get github.com/redis/go-redis/v9
go get github.com/google/uuid
go get github.com/joho/godotenv
```

**Step 3: Create config loader**

File: `internal/config/config.go`
```go
package config

import (
	"os"
	"strconv"
)

type Config struct {
	Port        int
	DBPath      string
	RedisAddr   string
	RedisPass   string
	RustFSEndpoint string
	RustFSBucket   string
	LogLevel    string
}

func Load() *Config {
	return &Config{
		Port:        getEnvInt("ACB_PORT", 8080),
		DBPath:      getEnv("ACB_DB_PATH", "/var/lib/acb/acb.db"),
		RedisAddr:   getEnv("ACB_REDIS_ADDR", "localhost:6379"),
		RedisPass:   getEnv("ACB_REDIS_PASS", ""),
		RustFSEndpoint: getEnv("ACB_RUSTFS_ENDPOINT", "http://localhost:8085"),
		RustFSBucket:   getEnv("ACB_RUSTFS_BUCKET", "acb-artifacts"),
		LogLevel:    getEnv("ACB_LOG_LEVEL", "info"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}
```

**Step 4: Create entrypoint**

File: `main.go`
```go
package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/amphora/acb/internal/config"
)

func main() {
	cfg := config.Load()
	log.Printf("ACB starting on port %d", cfg.Port)

	// TODO: wire up handlers, db, redis

	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Fatal(http.ListenAndServe(addr, nil))
}
```

**Step 5: Verify it compiles**

```bash
cd /home/amphora/src/acb && go build ./...
```

Expected: no errors, binary produced.

**Step 6: Commit**

```bash
cd /home/amphora/src/acb && git init && git add -A && git commit -m "feat: initial project scaffold with deps and config"
```

---

## Phase 1: Data Layer — SQLite Schema + Repositories

### Task 1.1: Create SQLite schema and migration runner

**Objective:** Define the three core tables (tasks, gates, agents) and a migration function that creates them on startup.

**Files:**
- Create: `/home/amphora/src/acb/internal/db/schema.go`
- Create: `/home/amphora/src/acb/internal/db/db.go`
- Test: `/home/amphora/src/acb/internal/db/schema_test.go`

**Step 1: Write failing test for schema creation**

File: `internal/db/schema_test.go`
```go
package db

import (
	"database/sql"
	"os"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestRunMigrations_CreatesTables(t *testing.T) {
	tmp := t.TempDir() + "/test.db"
	db, err := sql.Open("sqlite3", tmp)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := RunMigrations(db); err != nil {
		t.Fatal(err)
	}

	// Verify all three tables exist
	tables := []string{"tasks", "gates", "agents"}
	for _, name := range tables {
		var count int
		row := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", name)
		if err := row.Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("table %s not found after migration", name)
		}
	}
}
```

**Step 2: Run test to verify failure**

```bash
cd /home/amphora/src/acb && go test ./internal/db/ -run TestRunMigrations_CreatesTables -v
```

Expected: FAIL — `undefined: RunMigrations`

**Step 3: Write migration runner**

File: `internal/db/schema.go`
```go
package db

import "database/sql"

func RunMigrations(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS tasks (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		assignee TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'pending'
			CHECK(status IN ('pending','claimed','in_progress','blocked','completed','failed')),
		priority INTEGER NOT NULL DEFAULT 3,
		parents TEXT NOT NULL DEFAULT '[]',
		body_goal TEXT NOT NULL DEFAULT '',
		body_context TEXT NOT NULL DEFAULT '',
		body_deliverable_format TEXT NOT NULL DEFAULT 'markdown',
		body_deliverable_path TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT (datetime('now')),
		summary TEXT NOT NULL DEFAULT '',
		artifacts_json TEXT NOT NULL DEFAULT '[]'
	);

	CREATE TABLE IF NOT EXISTS gates (
		gate_id TEXT PRIMARY KEY,
		task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
		question TEXT NOT NULL,
		ask TEXT NOT NULL DEFAULT 'Jesus',
		status TEXT NOT NULL DEFAULT 'pending'
			CHECK(status IN ('pending','asked','answered','resolved')),
		answer TEXT NOT NULL DEFAULT ''
	);

	CREATE TABLE IF NOT EXISTS agents (
		name TEXT PRIMARY KEY,
		port INTEGER NOT NULL DEFAULT 0,
		token TEXT NOT NULL DEFAULT '',
		last_heartbeat TEXT
	);
	`

	_, err := db.Exec(schema)
	return err
}
```

File: `internal/db/db.go`
```go
package db

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}
	db.SetMaxOpenConns(1) // SQLite single-writer
	return db, nil
}
```

**Step 4: Run test to verify pass**

```bash
cd /home/amphora/src/acb && go test ./internal/db/ -run TestRunMigrations_CreatesTables -v
```

Expected: PASS

**Step 5: Commit**

```bash
cd /home/amphora/src/acb && git add -A && git commit -m "feat: sqlite schema with migrations and db connection"
```

### Task 1.2: Create task repository

**Objective:** Implement CRUD + state transitions for the tasks table via a repository pattern.

**Files:**
- Create: `/home/amphora/src/acb/internal/db/task_repo.go`
- Create: `/home/amphora/src/acb/internal/models/task.go`
- Test: `/home/amphora/src/acb/internal/db/task_repo_test.go`

**Step 1: Define task model**

File: `internal/models/task.go`
```go
package models

import "time"

type Task struct {
	ID                  string    `json:"id"`
	Title               string    `json:"title"`
	Assignee            string    `json:"assignee"`
	Status              string    `json:"status"`
	Priority            int       `json:"priority"`
	Parents             []string  `json:"parents"`
	BodyGoal            string    `json:"body_goal"`
	BodyContext         string    `json:"body_context"`
	BodyDeliverableFmt  string    `json:"body_deliverable_format"`
	BodyDeliverablePath string    `json:"body_deliverable_path"`
	CreatedAt           time.Time `json:"created_at"`
	Summary             string    `json:"summary"`
	Artifacts           []Artifact `json:"artifacts"`
}

type Artifact struct {
	Key    string `json:"key"`
	Bucket string `json:"bucket"`
	Size   int64  `json:"size"`
}
```

**Step 2: Write failing test for CreateTask**

```go
func TestCreateAndGetTask(t *testing.T) {
	db := setupTestDB(t)
	repo := NewTaskRepo(db)

	task := &models.Task{
		ID:       "t_test_001",
		Title:    "Test task",
		Assignee: "agent-alpha",
		Priority: 3,
		BodyGoal: "Run the test suite",
	}

	if err := repo.Create(task); err != nil {
		t.Fatal(err)
	}

	got, err := repo.GetByID("t_test_001")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Test task" {
		t.Errorf("expected 'Test task', got %q", got.Title)
	}
	if got.Status != "pending" {
		t.Errorf("expected status 'pending', got %q", got.Status)
	}
}
```

**Step 3: Implement TaskRepo**

File: `internal/db/task_repo.go` (full implementation with Create, GetByID, List, UpdateStatus)

**Step 4: Run tests → pass**

**Step 5: Repeat TDD cycle for:**
- `ClaimTask(id, assignee string) error` — sets status to `claimed` and assignee, error if not `pending`
- `StartTask(id string) error` — status → `in_progress`, error if not `claimed`
- `BlockTask(id, gateID string) error` — status → `blocked`
- `CompleteTask(id, summary string) error` — status → `completed`
- `FailTask(id, reason string) error` — status → `failed`
- `GetPendingByAgent(agent string) ([]Task, error)` — for agent work queue

### Task 1.3: Create gates repository

**Objective:** CRUD for gates — the human intervention mechanism.

**Files:**
- Create: `/home/amphora/src/acb/internal/db/gate_repo.go`
- Create: `/home/amphora/src/acb/internal/models/gate.go`
- Test: `/home/amphora/src/acb/internal/db/gate_repo_test.go`

Same TDD cycle:
- `CreateGate(gate *Gate) error`
- `GetByTaskID(taskID string) ([]Gate, error)`
- `AnswerGate(gateID, answer string) error` — sets `status=answered`, stores `answer`
- `ResolveGate(gateID string) error` — sets `status=resolved`

### Task 1.4: Create agents repository

**Objective:** Track agent registration and heartbeats.

**Files:**
- Create: `/home/amphora/src/acb/internal/db/agent_repo.go`
- Create: `/home/amphora/src/acb/internal/models/agent.go`
- Test: `/home/amphora/src/acb/internal/db/agent_repo_test.go`

Same TDD cycle:
- `UpsertAgent(agent *Agent) error` — insert or update
- `GetByName(name string) (*Agent, error)`
- `UpdateHeartbeat(name string) error` — sets `last_heartbeat` to now
- `ListStale(dur time.Duration) ([]Agent, error)` — agents without heartbeat in N minutes

---

## Phase 2: REST API Layer

### Task 2.1: Create HTTP router and middleware

**Objective:** Set up chi router with JSON content-type middleware, CORS, logging, and panic recovery.

**Files:**
- Create: `/home/amphora/src/acb/internal/api/router.go`
- Create: `/home/amphora/src/acb/internal/api/middleware.go`
- Create: `/home/amphora/src/acb/internal/api/response.go`
- Test: `/home/amphora/src/acb/internal/api/router_test.go`

**Step 1-4 (TDD):**
- Test that router returns 404 for unknown routes
- Test that `/health` returns 200 with `{"status":"ok"}`
- Implement JSON helpers (WriteJSON, WriteError)

File: `internal/api/response.go`
```go
package api

import (
	"encoding/json"
	"net/http"
)

func WriteJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, ErrorResponse{Error: code, Message: message})
}
```

File: `internal/api/middleware.go` — JSON content-type, request logging, panic recovery.

### Task 2.2: Implement task handlers

**Objective:** Wire up the repository with HTTP handlers for all task endpoints.

**Files:**
- Create: `/home/amphora/src/acb/internal/api/task_handler.go`
- Create: `/home/amphora/src/acb/internal/api/task_handler_test.go`

**Endpoints to implement (TDD each):**
- `POST /tasks` — Create task. Input: JSON body. Output: 201 + task.
- `GET /tasks` — List tasks. Query params: `?status=pending&assignee=agent-alpha`. Output: 200 + [].
- `GET /tasks/:id` — Get single task. Output: 200 or 404.
- `POST /tasks/:id/claim` — Claim task. Headers: `X-Agent-Name`. Output: 200 or 409 (already claimed).
- `POST /tasks/:id/start` — Start execution. Output: 200 or 409 (wrong state).
- `POST /tasks/:id/block` — Block with gate. Body: `{"gate_id":"g1","question":"..."}`. Output: 200.
- `POST /tasks/:id/unblock` — Unblock gate. Body: `{"gate_id":"g1","answer":"..."}`. Output: 200.
- `POST /tasks/:id/complete` — Complete. Body: `{"summary":"..."}`. Output: 200.
- `POST /tasks/:id/fail` — Fail. Body: `{"reason":"..."}`. Output: 200.

### Task 2.3: Implement agent handlers

**Files:**
- Create: `/home/amphora/src/acb/internal/api/agent_handler.go`

**Endpoints:**
- `POST /agents/heartbeat` — Update agent heartbeat. Header: `X-Agent-Name`.
- `GET /agents/:name` — Get agent status.

---

## Phase 3: Redis Integration

### Task 3.1: Create Redis Pub/Sub client

**Objective:** Publish events when tasks change state; allow agents to subscribe.

**Files:**
- Create: `/home/amphora/src/acb/internal/redis/events.go`
- Test: `/home/amphora/src/acb/internal/redis/events_test.go`

**Step 1: Define event types**

```go
const (
	ChannelAgentPrefix = "agent:"

	EventNewTask     = "new_task"
	EventTaskClaimed = "task_claimed"
	EventTaskStarted = "task_started"
	EventTaskBlocked = "task_blocked"
	EventTaskUnblock = "task_unblocked"
	EventTaskDone    = "task_completed"
	EventTaskFailed  = "task_failed"
)

type TaskEvent struct {
	Event   string `json:"event"`
	TaskID  string `json:"task_id"`
	Agent   string `json:"agent,omitempty"`
	GateID  string `json:"gate_id,omitempty"`
}
```

**Step 2-4 (TDD):**
- Test: `PublishTaskEvent(rdb, event, taskID, agent)` publishes JSON to `agent:<name>` channel
- Implementation: use `redis.Client.Publish()`

### Task 3.2: Wire Redis into task lifecycle

**Objective:** Every state transition in the task handler publishes a Redis event.

**Files:**
- Modify: `internal/api/task_handler.go`

After each successful state transition, call `redis.PublishTaskEvent()`.

Example:
```go
// After successful claim
go func() {
    pub.PublishTaskEvent(rdb, EventTaskClaimed, taskID, agentName)
}()
```

Events are fire-and-forget (goroutine). Non-blocking.

---

## Phase 4: Auth Middleware

### Task 4.1: Bearer token authentication

**Objective:** Every API call except `/health` must validate a Bearer token against the agents table.

**Files:**
- Create: `/home/amphora/src/acb/internal/api/auth.go`
- Modify: `internal/api/router.go`

**Step 1-4 (TDD):**
```go
func TestAuthMiddleware_ValidToken(t *testing.T) {
    // Setup: insert agent "worker-a" with token "abc123"
    // Request: POST /tasks with header "Authorization: Bearer abc123"
    // Expected: 200 (passes through to handler)
}

func TestAuthMiddleware_InvalidToken(t *testing.T) {
    // Request: POST /tasks with header "Authorization: Bearer invalid"
    // Expected: 401
}

func TestAuthMiddleware_MissingToken(t *testing.T) {
    // Request: POST /tasks without Authorization header
    // Expected: 401
}

func TestAuthMiddleware_HealthBypass(t *testing.T) {
    // Request: GET /health without token
    // Expected: 200 (public endpoint)
}
```

**Middleware implementation:**
```go
func AuthMiddleware(repo *db.AgentRepo) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Extract Bearer token
            // Validate against agents table
            // Set X-Agent-Name header from matched agent
            next.ServeHTTP(w, r)
        })
    }
}
```

---

## Phase 5: Wiring and Boot

### Task 5.1: Wire everything in main.go

**Objective:** Connect all components — DB, repos, Redis, router, auth.

**Files:**
- Modify: `/home/amphora/src/acb/main.go`

```go
func main() {
    cfg := config.Load()

    // DB
    database, err := db.Open(cfg.DBPath)
    if err != nil { log.Fatal(err) }
    defer database.Close()
    db.RunMigrations(database)

    // Redis
    rdb := redis.NewClient(&redis.Options{
        Addr: cfg.RedisAddr, Password: cfg.RedisPass, DB: 0,
    })

    // Repos
    taskRepo := db.NewTaskRepo(database)
    gateRepo := db.NewGateRepo(database)
    agentRepo := db.NewAgentRepo(database)

    // Pub
    pub := redis.NewPublisher(rdb)

    // Router
    r := api.NewRouter(taskRepo, gateRepo, agentRepo, pub)

    addr := fmt.Sprintf(":%d", cfg.Port)
    log.Printf("ACB listening on %s", addr)
    log.Fatal(http.ListenAndServe(addr, r))
}
```

### Task 5.2: Create env template and Dockerfile

**Files:**
- Create: `/home/amphora/src/acb/.env.example`
- Create: `/home/amphora/src/acb/Dockerfile`
- Create: `/home/amphora/src/acb/docker-compose.yml`

**`.env.example`:**
```
ACB_PORT=8080
ACB_DB_PATH=/var/lib/acb/acb.db
ACB_REDIS_ADDR=localhost:6379
ACB_REDIS_PASS=
ACB_RUSTFS_ENDPOINT=http://rustfs:8085
ACB_RUSTFS_BUCKET=acb-artifacts
ACB_LOG_LEVEL=info
```

**`Dockerfile`:** Multi-stage Go build, scratch runtime, copy acb binary.

**`docker-compose.yml`:**
```yaml
version: '3'
services:
  acb:
    build: .
    ports:
      - "8080:8080"
    env_file: .env
    volumes:
      - acb-data:/var/lib/acb
    depends_on:
      - redis
      - rustfs

  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"

  rustfs:
    image: rustfs:latest  # configurable
    ports:
      - "8085:8085"
    volumes:
      - rustfs-data:/data

volumes:
  acb-data:
  rustfs-data:
```

### Task 5.3: Integration test — full cycle

**Objective:** End-to-end test: create task → agent claims → starts → blocks → unblock → completes.

**Files:**
- Create: `/home/amphora/src/acb/tests/e2e_test.go`

```go
func TestFullTaskLifecycle(t *testing.T) {
    // Start ACB with test DB + test Redis
    // Register agent "worker-a" with token
    // POST /tasks → 201, status=pending
    // POST /tasks/:id/claim → 200, status=claimed, assignee=worker-a
    // POST /tasks/:id/start → 200, status=in_progress
    // POST /tasks/:id/block → 200, status=blocked, gate created
    // POST /tasks/:id/unblock → 200, status=in_progress
    // POST /tasks/:id/complete → 200, status=completed
}
```

---

## Phase 6: Documentation & Deployment

### Task 6.1: API documentation

**Files:**
- Create: `/home/amphora/src/acb/README.md`

Document:
- Architecture overview
- Setup instructions (Docker, native)
- API reference (all endpoints)
- Agent configuration example
- How gates work

### Task 6.2: Agent integration guide

**Files:**
- Create: `/home/amphora/src/acb/docs/agent-integration.md`

Document for each agent type (e.g. worker-a, worker-b):
- Required env vars
- How to claim tasks from the ACB queue
- How to report progress
- How to create/answer gates
- How to send heartbeats

---

## Summary

| Phase | Tasks | Est. time |
|-------|-------|-----------|
| 0. Scaffolding | 1 task | ~10 min |
| 1. Data Layer | 4 tasks | ~60 min |
| 2. REST API | 3 tasks | ~90 min |
| 3. Redis | 2 tasks | ~30 min |
| 4. Auth | 1 task | ~20 min |
| 5. Wiring | 3 tasks | ~30 min |
| 6. Docs | 2 tasks | ~20 min |

**Total: ~16 tasks, ~4 hours** with TDD per task.

---

## Prerequisites (to install on this server)

```bash
# Go 1.22+
wget https://go.dev/dl/go1.22.3.linux-amd64.tar.gz
sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf go1.22.3.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc

# Redis (Docker preferred)
docker run -d --name redis -p 6379:6379 redis:7-alpine
```