# MVP Swarm — Design Specification

**Date:** 2026-05-26  
**Status:** Approved  
**Scope:** Backend-only MVP (UI dashboard is post-MVP)

---

## 1. Overview

MVP Swarm extends `acb-go` from a task-level orchestrator into a **mission-level orchestrator**. A user (or external system) defines a high-level **Project** with a `goal`. The built-in **Orchestrator Agent** decomposes the goal into individual `Task`s with dependencies. Autonomous AI agents (Hermes, OpenClaw, Nanobot, etc.) claim tasks, execute them, and may invoke tools from `mcp-go` directly. All events are broadcast via SSE for real-time dashboards.

**Key principle:** `acb-go` owns the mission (project + tasks + gates + agents). `mcp-go` owns the tool catalog. Agents talk to both.

---

## 2. Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│              Future Web Dashboard (post-MVP)                    │
│                    (SSE consumer)                               │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                        acb-go (Go)                               │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────────┐   │
│  │   Project    │  │ Event Stream │  │  Orchestrator Agent │   │
│  │   (new)      │  │   (new SSE)  │  │      (new)           │   │
│  └──────────────┘  └──────────────┘  └──────────────────────┘   │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────────┐   │
│  │ Task (exists)│  │ Gate (exists)│  │ Agent Registry       │   │
│  │ +project_id  │  │              │  │ (exists)             │   │
│  └──────────────┘  └──────────────┘  └──────────────────────┘   │
│  ┌──────────────┐  ┌──────────────┐                             │
│  │ Dispatcher   │  │ Worktree Mgr │                             │
│  │ (exists)     │  │   (new)      │                             │
│  └──────────────┘  └──────────────┘                             │
└─────────────────────────────────────────────────────────────────┘
                              │ REST API (tasks, claims, gates)
                              │
┌─────────────────────────────────────────────────────────────────┐
│              Agente IA (Hermes / OpenClaw / etc.)                │
│  ┌────────────────────────┐  ┌──────────────────────────────┐  │
│  │ Task loop (claim/start)│  │ MCP Client → mcp-go:8080    │  │
│  │ complete/fail/heartbeat│  │ POST /mcp (tools/list, call)│  │
│  └────────────────────────┘  └──────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
                              │ MCP Streamable HTTP
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                        mcp-go (Go)                               │
│  ┌────────────────────────┐                                     │
│  │ Tool Orchestrator      │  tools: data_analysis, kb_search,   │
│  │ (existe)               │  pdf_reports, vision_ocr, etc.      │
│  └────────────────────────┘                                     │
└─────────────────────────────────────────────────────────────────┘
```

**Two services, one mission:**
1. `acb-go` — mission control (projects, tasks, gates, agents, events, worktrees)
2. `mcp-go` — tool shed (MCP server with Python tool catalog)

---

## 3. Components

### 3.1 Project Entity (new)

```go
type Project struct {
    ID          string     `json:"id"`
    Title       string     `json:"title"`
    Goal        string     `json:"goal"`              // high-level objective
    Status      string     `json:"status"`            // planning | active | completed | failed
    CreatedAt   time.Time  `json:"created_at"`
    UpdatedAt   time.Time  `json:"updated_at"`
    CompletedAt *time.Time `json:"completed_at,omitempty"`
}
```

**Lifecycle:**
- `planning` — project created, no tasks yet
- `active` — tasks published, agents can claim
- `completed` — all tasks done
- `failed` — one or more tasks exhausted retries

### 3.2 Task Extension (existing)

Add field to `models.Task`:
```go
// Inside Task struct, add:
ProjectID string `json:"project_id,omitempty"`
```

When a task belongs to a project, the agent UI can show context: *"Task #3 of 'Refactor Auth'"*.

### 3.3 Orchestrator Agent (new)

A built-in goroutine in `acb-go` (not an external agent) that:

1. Listens for `/projects/{id}/activate` calls
2. Reads the project `goal`
3. Decomposes it into tasks using **templates** or an optional LLM
4. Inserts tasks with `project_id` set
5. Transitions project to `active`

**Template-based decomposition (MVP default):**

A YAML config file `configs/project_templates.yaml` defines reusable task breakdowns:

```yaml
# configs/project_templates.yaml
templates:
  - name: "refactor_auth"
    pattern: "refactor.*auth|migrar.*autenticación"
    tasks:
      - title: "Auditar autenticación actual"
        skills: ["code-review", "security"]
        body_goal: "Auditar el código actual de autenticación..."
      - title: "Diseñar nuevo flujo de sesiones"
        skills: ["architecture", "redis"]
        body_goal: "Diseñar el nuevo flujo..."
        parents: [0]
      - title: "Implementar migración de datos"
        skills: ["backend", "database"]
        body_goal: "Migrar usuarios existentes..."
        parents: [1]
```

Matching is done via simple regex on the goal string. If no template matches, the orchestrator uses an **optional LLM client** (configurable).

### 3.4 Event Stream / SSE (new)

**Endpoint:** `GET /events/stream`

**Protocol:** Server-Sent Events (text/event-stream)

**Events emitted:**

| Event Type          | Payload Fields |
|---------------------|----------------|
| `project_created`   | `project` (Project) |
| `project_activated` | `project_id` |
| `project_completed` | `project_id` |
| `task_created`      | `task` (Task) |
| `task_claimed`      | `task_id`, `agent` |
| `task_started`      | `task_id`, `agent` |
| `task_blocked`      | `task_id`, `gate_id`, `question` |
| `task_unblocked`    | `task_id`, `gate_id` |
| `task_completed`    | `task_id`, `agent`, `summary` |
| `task_failed`       | `task_id`, `agent`, `reason` |
| `agent_heartbeat`   | `agent_name`, `timestamp` |
| `agent_stale`       | `agent_name` |

**Implementation plan:**
- Uses the existing `internal/redis/events.go` `Publisher` as a bus
- Maintains an in-memory `sync.Map` of SSE client connections
- When any handler triggers an event, it is: (1) published to Redis, (2) broadcast to all SSE clients
- Each SSE connection runs as a goroutine flushing events

### 3.5 Worktree Manager (new)

Creates and manages git worktrees so each task has an isolated working directory.

**API:**
```
POST /projects/{id}/worktrees  → creates worktree + branch per task
GET /worktrees/{task_id}       → returns {path, branch}
DELETE /worktrees/{task_id}    → removes worktree
```

**Storage path:** `/var/lib/acb/worktrees/{project_id}/{task_id}/`

**Branch naming:** `feat/{project_id}/{sanitized-task-title}`

**Implementation:** uses `os/exec` to run `git worktree add` and `git branch` commands with timeout.

---

## 4. Database Schema Additions

```sql
CREATE TABLE IF NOT EXISTS projects (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    goal TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'planning',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    completed_at DATETIME
);

ALTER TABLE tasks ADD COLUMN project_id TEXT REFERENCES projects(id);

CREATE INDEX IF NOT EXISTS idx_tasks_project_id ON tasks(project_id);
CREATE INDEX IF NOT EXISTS idx_projects_status ON projects(status);

CREATE TABLE IF NOT EXISTS worktrees (
    task_id TEXT PRIMARY KEY REFERENCES tasks(id),
    project_id TEXT NOT NULL REFERENCES projects(id),
    path TEXT NOT NULL,
    branch TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

---

## 5. API Endpoints

### 5.1 Projects

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/projects` | Create project |
| `GET`  | `/projects` | List projects |
| `GET`  | `/projects/{id}` | Get project + task summary |
| `POST` | `/projects/{id}/activate` | Trigger orchestrator to decompose and activate |
| `GET`  | `/projects/{id}/progress` | Get completion stats |
| `POST` | `/projects/{id}/worktrees` | Create worktrees for all tasks |

### 5.2 Tasks (modified)

`POST /tasks` now accepts optional `project_id`.

### 5.3 SSE

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/events/stream` | SSE stream |

### 5.4 Worktrees

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/worktrees/{task_id}` | Get worktree info |
| `DELETE` | `/worktrees/{task_id}` | Remove worktree |

---

## 6. Orchestrator Logic

```go
type Orchestrator struct {
    projectRepo *db.ProjectRepo
    taskRepo    *db.TaskRepo
    templates   []ProjectTemplate
    llmClient   *llmClient // optional
}

func (o *Orchestrator) PlanProject(ctx context.Context, projectID string) error {
    project, err := o.projectRepo.GetByID(ctx, projectID)
    if err != nil { return err }

    template := o.matchTemplate(project.Goal)
    if template != nil {
        return o.executeTemplate(ctx, project, template)
    }

    if o.llmClient != nil {
        return o.planWithLLM(ctx, project)
    }

    return fmt.Errorf("no template matched and LLM not configured")
}
```

---

## 7. Security Considerations

- Worktree paths validated to prevent traversal
- Git commands run with timeout
- SSE endpoint is open (no auth) — only status/ID events, no task body
- Orchestrator LLM calls use `internal/executor/llm_client.go` from mcp-go

---

## 8. Post-MVP (out of scope)

- Web Dashboard
- Full LLM-based task decomposition
- Cost tracking
- Agent conversation history / branching
- Skills library sync
- Diff viewer
- Batch gate approvals
