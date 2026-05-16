# ACB Dispatcher Architecture Analysis

**Date:** 2025-05-16  
**Context:** El dispatcher Python (`scripts/acb_dispatcher.py`) ejecuta como systemd service con polling cada 30s. Solo funciona para agents Hermes (webhook `/webhooks/amanda`). El objetivo es que **CUALQUIER tipo de agente** pueda recibir tareas del ACB.

---

## Current State

```
ACB (Go) ──Redis Pub/Sub──→ tasks:pending channel
                              ↓ (nobody subscribes)
ACB (Go) ──HTTP API──→ GET /tasks?status=pending
                              ↓
acb_dispatcher.py (systemd) ──poll every 30s──→ match skills → webhook → Hermes /webhooks/amanda
```

**Problems:**
- `AGENTS` dict hardcoded with ports, secrets, tokens
- Only knows about `/webhooks/amanda` (Hermes-specific endpoint)
- Skill matching logic lives in Python, disconnected from ACB's Go skill validation
- No persistence of dispatch state (in-memory `_dispatched` set lost on restart)
- Linux-only (systemd), no Windows/macOS alternative
- Single point of failure: dispatcher down = no task delivery
- 30s latency window minimum

---

## Option 1: Systemd Service (Current Approach)

**How it works:** Python script runs as systemd unit, polls ACB, dispatches to agents.

### Assessment

| Criterion | Score | Notes |
|-----------|-------|-------|
| Portability | ⚠️ 2/5 | Linux-only, no Docker native support |
| Simplicity | ✅ 4/5 | 200 lines of Python, easy to understand |
| Coupling | ❌ 1/5 | Hardcoded agents, Hermes-specific webhook format |
| Resilience | ⚠️ 2/5 | In-memory state, no retry, single process |
| Multi-agent | ❌ 1/5 | Only works with Hermes agents via `/webhooks/amanda` |

**Verdict:** The current approach is **ad-hoc**. Systemd is fine for process management, but the dispatcher logic itself is the problem — it's a hardcoded bridge between ACB and one specific agent type.

---

## Option 2: ACB Push via Webhooks

**How it works:** ACB stores `webhook_url` per agent. On task creation, ACB POSTs the task to each relevant agent's webhook URL.

```
Agent registers: POST /agents {name: "braulio", webhook_url: "http://localhost:8645/webhooks/amanda", skills: [...]}
ACB creates task → looks up matching agents → POSTs task to their webhook_url
```

### Assessment

| Criterion | Score | Notes |
|-----------|-------|-------|
| Portability | ✅ 5/5 | HTTP is universal — any agent in any language |
| Simplicity | ✅ 4/5 | Simple POST, standard HTTP semantics |
| Coupling | ✅ 4/5 | ACB only needs to know URL, not implementation |
| Resilience | ⚠️ 3/5 | Needs retry logic, timeout handling, webhook queue |
| Multi-agent | ✅ 5/5 | Any agent with an HTTP endpoint works |

**Changes needed:**
- Add `webhook_url` column to `agents` table
- Add dispatch logic in ACB's `CreateTask` handler (after Redis publish)
- Webhook payload: task JSON + action ("new_task")
- Signature: HMAC-SHA256 for verification
- Retry queue for failed webhooks (Redis list or DB table)

**Biggest advantage:** Agent framework-agnostic. A shell script with `nc`, a Python bot, a Node.js service — all can receive webhooks.

**Biggest risk:** What if the agent is down? Need fallback (→ Option 4 polling).

---

## Option 3: Redis Pub/Sub

**How it works:** Agents subscribe directly to Redis channels (`tasks:pending`, `agent:<name>`). ACB already publishes these events.

```
ACB creates task → publishes to Redis "tasks:pending" and "agent:<name>"
Agent (any language with Redis client) subscribes to relevant channels
```

### Assessment

| Criterion | Score | Notes |
|-----------|-------|-------|
| Portability | ⚠️ 3/5 | Requires Redis client in every agent |
| Simplicity | ✅ 4/5 | ACB already publishes! Just use it |
| Coupling | ⚠️ 3/5 | Binds agents to Redis infrastructure |
| Resilience | ⚠️ 2/5 | Fire-and-forget — missed messages if agent offline |
| Multi-agent | ⚠️ 3/5 | Works, but every agent needs Redis client lib |

**Advantages:**
- ACB already publishes to `tasks:pending` and `agent:<name>` — zero ACB code changes
- Near-zero latency
- Real-time

**Disadvantages:**
- Redis Pub/Sub has **no delivery guarantee** — if agent is offline, message is lost
- Requires all agents to have Redis connectivity
- Redis credentials must be shared (or use ACLs)
- Not all agent frameworks have Redis clients (especially simple scripts)
- Pollutes agent infrastructure — agents must now understand Redis

**Best for:** Agents that are always-online services (already in the Docker network with Redis).

---

## Option 4: Agent Polling

**How it works:** Each agent periodically calls `GET /tasks?status=pending&assignee=me` or a new `GET /tasks/next` endpoint. Like current approach but ACB-centric instead of dispatcher-centric.

```
Agent heartbeat: POST /agents/heartbeat {name: "braulio"}
Agent polls: GET /tasks?status=pending → claims matching ones
```

### Assessment

| Criterion | Score | Notes |
|-----------|-------|-------|
| Portability | ✅ 5/5 | Any HTTP client can poll |
| Simplicity | ✅ 5/5 | Agent just needs curl-level HTTP |
| Coupling | ✅ 5/5 | Agent only knows ACB API, nothing else |
| Resilience | ✅ 4/5 | Naturally resilient — retry on next poll |
| Multi-agent | ✅ 5/5 | Any agent can poll |

**Advantages:**
- Zero infrastructure dependencies beyond ACB itself
- Idempotent by nature
- Agent controls its own cadence
- Works with any agent type: shell scripts, cron jobs, daemons

**Disadvantages:**
- Latency = polling interval (15-60s typical)
- Wasteful if no tasks (empty polls)
- Agent must implement skill-matching OR claim task and return if wrong skills

**Enhancement:** Add a dedicated endpoint:
```
GET /tasks/dispatch?agent=<name>  → returns best-matching pending task for this agent
```
This moves the skill-matching logic from the Python dispatcher into ACB where it belongs.

---

## Option 5: WebSocket / SSE Stream

**How it works:** ACB maintains persistent connections with agents. Pushes tasks immediately when created.

```
Agent connects: WS /agents/stream or GET /agents/events (SSE)
ACB pushes: task assignment events in real-time
```

### Assessment

| Criterion | Score | Notes |
|-----------|-------|-------|
| Portability | ⚠️ 2/5 | WS/SSE not trivial in all environments |
| Simplicity | ❌ 2/5 | Connection management, reconnection, state |
| Coupling | ✅ 3/5 | ACB becomes stateful for connections |
| Resilience | ⚠️ 3/5 | Needs reconnection logic, message buffering |
| Multi-agent | ⚠️ 3/5 | Agent must implement WS/SSE client |

**Advantages:**
- Instant delivery
- No polling overhead
- Bidirectional communication (WS)

**Disadvantages:**
- Significant complexity in ACB (connection pool, heartbeat, cleanup)
- ACB becomes stateful — cannot restart without disrupting agents
- Harder to load-balance / scale
- Not all agent environments support persistent connections (e.g., serverless)
- Overkill for current scale (3 agents)

---

## Comparison Matrix

```
                    Portability  Simplicity  Decoupling  Resilience  Multi-agent
─────────────────────────────────────────────────────────────────────────────────
1. Systemd (current)    ⚠️2        ✅4         ❌1         ⚠️2         ❌1
2. Webhook push         ✅5        ✅4         ✅4         ⚠️3         ✅5
3. Redis Pub/Sub        ⚠️3        ✅4         ⚠️3         ⚠️2         ⚠️3
4. Agent polling        ✅5        ✅5         ✅5         ✅4         ✅5
5. WS/SSE stream        ⚠️2        ❌2         ✅3         ⚠️3         ⚠️3
```

---

## ⭐ Recommended Architecture: Hybrid Webhook + Polling

The dispatcher should not exist as a separate component. ACB itself should handle dispatch.

### Design

```
                    ┌─────────────────────────────────┐
                    │           ACB (Go)                │
                    │                                  │
                    │  1. Task created                  │
                    │  2. Look up agents with matching  │
                    │     skills (or all if no skills) │
                    │  3. POST task to agent webhook_url│
                    │  4. If webhook fails → queue     │
                    │     for retry (Redis list)        │
                    │                                  │
                    │  ALSO: publish Redis event       │
                    │  (for real-time subscribers)      │
                    └──────────┬───────────────────────┘
                               │
              ┌────────────────┼──────────────────┐
              │                │                  │
        ┌─────▼─────┐  ┌──────▼──────┐  ┌───────▼──────┐
        │  Hermes    │  │  Python     │  │  Shell Script│
        │  Agent     │  │  Agent      │  │  (cron poll) │
        │            │  │             │  │               │
        │ Webhook:   │  │ Webhook:    │  │ No webhook:  │
        │ /webhooks/ │  │ /dispatch   │  │ polls every  │
        │  amanda    │  │             │  │ 60s           │
        └────────────┘  └─────────────┘  └──────────────┘
```

### Implementation Plan

**Phase 1: Add webhook_url to agents table + ACB push**

1. Schema migration: add `webhook_url` and `webhook_secret` to `agents`
2. On `CreateTask`, after persisting to DB:
   ```go
   go dispatchToAgents(task, matchingAgents)
   ```
3. `dispatchToAgents` tries POST to each agent's `webhook_url`
4. If POST fails, push to Redis list `dispatch:retry:<agent_name>`
5. Background goroutine retries failed webhooks (exponential backoff)

**Phase 2: Add smart polling endpoint**

```go
// GET /tasks/dispatch?agent=<name>
// Returns the best-matching pending task for this agent
// considering skills, priority, and dependencies
func (h *TaskHandler) DispatchNext(w, r) {
    agent := r.Header.Get("X-Agent-Name")
    task := h.taskRepo.FindNextForAgent(agent)
    // Auto-assigns and returns task
}
```

**Phase 3: Deprecate acb_dispatcher.py**

Once both mechanisms work, the Python dispatcher becomes unnecessary.

### Why Not Just Redis Pub/Sub?

Redis Pub/Sub is great for real-time agents that are **always connected**. But:
- It's fire-and-forget — no delivery guarantee
- Requires every agent to have a Redis client
- Doesn't work for agents behind NAT/firewalls
- The ACB webhook already includes all task context; Redis just signals "something happened"

**Recommendation:** ACB publishes to Redis for internal event streaming (auditing, logging, orchestration). But for **task dispatch**, use webhooks + polling. Redis is a supporting transport, not the primary dispatch mechanism.

### Agent Registration (New Schema)

```sql
ALTER TABLE agents ADD COLUMN webhook_url TEXT DEFAULT '';
ALTER TABLE agents ADD COLUMN webhook_secret TEXT DEFAULT '';
ALTER TABLE agents ADD COLUMN poll_interval INTEGER DEFAULT 30;
```

Agent registration:
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

ACB webhook payload:
```json
POST <webhook_url>
{
  "action": "new_task",
  "task": {
    "id": "...",
    "title": "...",
    "required_skills": ["go"],
    "body_goal": "...",
    "body_context": "...",
    "body_deliverable_format": "markdown",
    "body_deliverable_path": "..."
  },
  "timestamp": "2025-05-16T21:00:00Z"
}
```

Signature: `X-Webhook-Signature: HMAC-SHA256(webhook_secret, body)`

### Retry Mechanism

```
Redis list: dispatch:retry:{agent_name}
  └─ Failed webhooks pushed here
  └─ Goroutine pops every 5s with exponential backoff
  └─ Max 5 retries, then mark task as "dispatch_failed"
  └─ Agent can still claim via polling
```

---

## Summary

| | Short-term | Medium-term | Long-term |
|---|---|---|---|
| **Keep** | Python dispatcher | — | — |
| **Add** | `webhook_url` in agents table | `/tasks/dispatch` smart polling | Webhook retry + dead letter |
| **Decommission** | — | acb_dispatcher.py | — |

**The dispatcher Python script is ad-hoc and should be replaced.** The right architecture is for ACB itself to push webhooks on task creation, with a smart polling endpoint as fallback. This makes the system:

1. **Universal** — any agent type can participate
2. **Resilient** — webhook + poll covers both push and pull
3. **Simple** — no separate process to manage
4. **Decoupled** — agents only need an HTTP endpoint or willingness to poll
5. **Portable** — no OS dependencies, no Redis client requirement

Redis Pub/Sub remains valuable for event streaming and internal coordination, but should not be the primary task dispatch mechanism.