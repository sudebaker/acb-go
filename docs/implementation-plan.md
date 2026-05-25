# ACB v2 — Implementation Plan

## Overview
Six independent feature areas to extend ACB's task orchestration capabilities.
Each phase is self-contained, can be tested independently, and does not change
existing routes or break backward compatibility.

---

## Phase 1: Task Events API

**Goal:** Expose the existing `task_events` table via a read-only REST endpoint.

### Files to modify
| File | Change |
|------|--------|
| `internal/api/task_handler.go` | Add `ListTaskEvents` handler |
| `internal/api/router.go` | Add `GET /tasks/{id}/events` route |
| `internal/db/task_repo.go` | Log `CreateTask` event in `Create()` |

### Testing
- `TestListTaskEvents_200` — create a task, verify event exists
- `TestListTaskEvents_NotFound` — non-existent task returns 200 empty array

---

## Phase 2: Task Heartbeat

**Goal:** Track liveness of in-progress tasks; auto-fail stale ones.

### Files to modify
| File | Change |
|------|--------|
| `internal/db/schema.go` | Add `last_heartbeat TIMESTAMPTZ` column (migration v2) |
| `internal/models/task.go` | Add `LastHeartbeat *time.Time` field |
| `internal/db/task_repo.go` | Add `UpdateTaskHeartbeat(taskID string) error` and `ExpireStaleInProgressTasks(timeoutMinutes int)` |
| `internal/api/task_handler.go` | Add `TaskHeartbeat` handler |
| `internal/api/router.go` | Add `POST /tasks/{id}/heartbeat` route |
| `internal/timeout/timeout.go` | Rename to generic `TimeoutService`, add stale in-progress check |
| `internal/config/config.go` | Add `TaskTimeoutMin`, `TaskTimeoutCheckSec` |
| `main.go` | Wire updated `TimeoutService` |

### State machine
- `in_progress` tasks with `last_heartbeat < NOW() - N minutes` → `failed`

---

## Phase 3: Auto-Retries

**Goal:** Automatically requeue failed tasks when `retry_count < max_retries`.

### Files to modify
| File | Change |
|------|--------|
| `internal/db/schema.go` | Add `max_retries INT DEFAULT 0`, `retry_count INT DEFAULT 0` (migration v2) |
| `internal/models/task.go` | Add `MaxRetries int`, `RetryCount int` |
| `internal/db/task_repo.go` | Modify `FailTask` to auto-requeue if retryable; add `IncrementRetry` |
| `internal/api/task_handler.go` | `CreateTask` input accepts `max_retries` |

### State machine extension
```
fail → if retry_count < max_retries → reset to pending, retry_count++
                                                         → webhook dispatch
```

---

## Phase 4: Dependencies (Parents Enforcement)

**Goal:** Prevent claiming tasks whose parents aren't completed; auto-promote
children when a parent completes.

### Files to modify
| File | Change |
|------|--------|
| `internal/db/task_repo.go` | Add `CheckParentsCompleted(taskID string) (bool, error)`, `PromoteChildren(taskID string)` |
| `internal/api/task_handler.go` | `ClaimTask`: return 403 if parents not completed |
| `internal/dispatcher/dispatcher.go` | `FindNextForAgent`: filter out tasks with uncompleted parents |

### State machine extension
```
pending (parents incomplete) → no agent can claim → becomes claimable only
when all parents are completed
```

---

## Phase 5: Dashboard + Dependency Graph

**Goal:** Enriched JSON endpoints for monitoring and visualization.

### Files to add/modify
| File | Change |
|------|--------|
| `internal/api/dashboard_handler.go` | New file with `Dashboard` + `TaskGraph` handlers |
| `internal/api/router.go` | Add `GET /dashboard`, `GET /tasks/{id}/graph` routes |
| `internal/db/task_repo.go` | Add `GetTaskCounts()`, `GetDependencyGraph(taskID string)` |

### Dashboard response
```json
{
  "tasks_by_status": {
    "pending": [...],
    "claimed": [...],
    "in_progress": [...],
    "blocked": [...],
    "completed": [...],
    "failed": [...]
  },
  "total": 42,
  "stale_agents": 1
}
```

### Graph response
```json
{
  "task": {...},
  "parents": [{...}],
  "children": [{...}]
}
```

---

## Phase 6: Stale Agent Detection

**Goal:** Detect agents that stopped heartbeating and release their tasks.

### Files to modify
| File | Change |
|------|--------|
| `internal/timeout/timeout.go` | Add stale-agent check using `agentRepo.ListStale()` |
| `internal/config/config.go` | Add `AgentStaleMin`, `AgentStaleCheckSec` |
| `main.go` | Wire stale-agent config |

### Behavior
- Agents with `last_heartbeat < NOW() - N minutes` are stale
- Their claimed/in-progress tasks are released back to pending
- A `StaleAgentRelease` event is logged per task

---

## Implementation Order
```
Phase 1 (low risk, quick win)
    ↓
Phase 2 (medium, needs DB migration)
    ↓
Phase 3 (medium, needs DB migration)
    ↓
Phase 4 (high, changes claim + dispatch logic)
    ↓
Phase 5 (low, purely additive endpoints)
    ↓
Phase 6 (medium, background goroutine)
```

Each phase has its own test file additions and does not break existing tests.
Run `go test ./...` after each phase before moving to the next.
