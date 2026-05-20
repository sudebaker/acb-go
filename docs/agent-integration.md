# Agent Integration Guide

This guide explains how an autonomous agent interacts with the Agent Communication Bus (ACB) to claim, execute, and complete tasks.

---

## Overview

Agents connect to the ACB via REST API and optionally subscribe to Redis events for real-time notifications. Each agent must be registered in the ACB's `agents` table with a unique name and Bearer token.

Communication flow:
```
Agent ──REST──→ ACB (state changes, heartbeats)
Agent ←─Redis── ACB (event notifications)
```

---

## 1. Registration

Before an agent can interact with the ACB, it must be registered. This is typically done by the orchestrator at deployment time.

**SQL (direct setup):**
```sql
INSERT INTO agents (name, port, token, skills) VALUES ('agent-alpha', 8081, 'your-secret-token', '["python","linux","docker"]');
```

**Via repository (Go code):**
```go
agentRepo.UpsertAgent(&models.Agent{
    Name:   "agent-alpha",
    Port:   8081,
    Token:  "your-secret-token",
    Skills: []string{"python", "linux", "docker"},
})
```

Each agent must have a unique token. The token is used for Bearer authentication on every request. The `skills` field is configured at deployment time by the operator — the agent does not declare its own skills. The ACB validates these skills when the agent attempts to claim a task.

**Skills catalog:** The ACB enforces a fixed catalog of allowed skills via the `ACB_ALLOWED_SKILLS` env var (comma-separated). At registration, skills not in the catalog are rejected with `400`:

```json
{"error": "invalid_skills", "message": "invalid agent skills: [\"hacking\"]", "allowed": ["coding","review","testing",...]}
```

If `ACB_ALLOWED_SKILLS` is not configured, all skills are accepted.

**Pending task timeout:** Tasks that remain in `pending` state without being claimed automatically expire after `ACB_PENDING_TIMEOUT_MIN` minutes (default: 15). The task transitions to `failed` with reason `"expired: pending timeout"`. Agents should handle this gracefully — if a dispatch/claim arrives after expiration, it will return `409`.

---

## 2. Authentication

Every API call (except `/health`) must include the Bearer token:

```
Authorization: Bearer your-secret-token
```

The ACB validates the token against the `agents` table and sets the `X-Agent-Name` header, which is used for heartbeats and event routing.

---

## 3. Task Lifecycle

The standard task lifecycle for an agent:

```
                     ┌──────────────────────┐
                     │  Agent polls for      │
                     │  pending tasks        │
                     └──────────┬───────────┘
                                │
                     ┌──────────▼───────────┐
                     │  POST /tasks/:id/claim│
                     │  (status: claimed)    │
                     └──────────┬───────────┘
                                │
                     ┌──────────▼───────────┐
                     │  POST /tasks/:id/start│
                     │  (status: in_progress)│
                     └──────────┬───────────┘
                                │
                    ┌───────────┴───────────┐
                    │                      │
            ┌───────▼───────┐     ┌────────▼────────┐
            │ Need human    │     │ Work completes   │
            │ input?        │     │ successfully     │
            └───────┬───────┘     └────────┬────────┘
                    │                      │
         ┌──────────▼──────────┐  ┌────────▼────────┐
         │ POST /tasks/:id/block│  │ POST /tasks/:id/ │
         │ (status: blocked)   │  │ complete         │
         └──────────┬──────────┘  │ (status: comp.)  │
                    │             └─────────────────┘
         (wait for orchestrator   ┌─────────────────┐
          to unblock)       ───→  │ POST /tasks/:id/ │
                    │             │ fail             │
         ┌──────────▼──────────┐  │ (status: failed) │
         │ POST /tasks/:id/    │  └─────────────────┘
         │ unblock (orchestr.) │
         │ (status: in_progress)│
         └────────────────────┘
```

### Step-by-step for agents:

#### 3.1 Poll for Tasks
```http
GET /tasks?status=pending&required_skills=python,linux
Authorization: Bearer your-secret-token
```

Returns pending tasks. Agents should filter by their own capabilities to find relevant work.

#### 3.2 Claim
```http
POST /tasks/:id/claim
Authorization: Bearer your-secret-token
Content-Type: application/json

{"assignee": "agent-alpha"}
```

Only tasks in `pending` state can be claimed. The ACB validates that the authenticated agent has **all** the skills listed in `required_skills`. Returns `403` if the agent lacks the necessary skills, or `409` if already claimed by another agent.

#### 3.3 Start
```http
POST /tasks/:id/start
Authorization: Bearer your-secret-token
```

Marks the beginning of actual work. Task must be in `claimed` state.

#### 3.4 Complete or Fail

**Successful completion:**
```http
POST /tasks/:id/complete
Authorization: Bearer your-secret-token
Content-Type: application/json

{"summary": "Analysis complete. Found 3 anomalies."}
```

**Failure:**
```http
POST /tasks/:id/fail
Authorization: Bearer your-secret-token
Content-Type: application/json

{"reason": "Timeout connecting to database"}
```

Both require the task to be in `in_progress` state.

---

## 4. Heartbeat Protocol

Agents must send regular heartbeats to signal they are alive. This is used by the orchestrator to detect stale agents.

```http
POST /agents/heartbeat
Authorization: Bearer your-secret-token
Content-Type: application/json

{"name": "agent-alpha"}
```

**Rate limit:** 10 requests per minute per agent. Exceeding this returns `429 Too Many Requests`.

**Recommended interval:** Every 10 seconds (6 requests per minute), well within the limit.

**If name is omitted:**
```json
{}
```
The server falls back to `X-Agent-Name` header (set automatically by the auth middleware based on the token). This is the recommended approach — just send an empty body.

**Stale detection:** The orchestrator can query `ListStale(duration)` to find agents that haven't sent a heartbeat within the specified window.

---

## 5. Working with Gates (Human Intervention)

When a task requires human input, the agent blocks it on a gate:

```http
POST /tasks/:id/block
Authorization: Bearer your-secret-token
Content-Type: application/json

{
  "gate_id": "g_001",
  "question": "Should we proceed with the deployment?"
}
```

This transitions the task to `blocked` status and creates a gate record. The orchestrator (or human operator) then:

1. Reviews the gate question
2. Provides an answer via `AnswerGate` (programmatic or via a dashboard)
3. Calls `POST /tasks/:id/unblock` to resolve the gate and return the task to `in_progress`

The agent should listen for Redis events (see section 6) or poll `GET /tasks/:id` to detect when the task is unblocked and resume work.

---

## 6. Subscribing to Redis Events

Agents can subscribe to Redis channels to receive real-time notifications instead of polling.

**Channel formats:**

| Channel | Purpose |
|---------|---------|
| `agent:<agent_name>` | Direct notifications for a specific agent |
| `tasks:pending` | Broadcast of all new tasks |

**Recommended subscription:** Subscribe to `agent:<agent_name>` for direct notifications, plus `tasks:pending` to discover new work.

Example subscription (Redis CLI):
```bash
SUBSCRIBE "agent:agent-alpha" "tasks:pending"
```

**Events relevant to agents:**

| Event | When | Action |
|-------|------|--------|
| `new_task` | Task created | Poll or claim the task |
| `task_unblocked` | Gate resolved | Resume work on the task |

**Event payload example:**
```json
{"event":"new_task","task_id":"t_123","required_skills":["python","linux"],"agent":"agent-alpha"}
```

Events are fire-and-forget — the ACB does not retry on Redis failures. Agents should handle missed events gracefully (e.g., periodic polling as fallback).

**Recommended subscription setup (Go):**
```go
sub := rdb.Subscribe(ctx, "agent:agent-alpha")
ch := sub.Channel()
for msg := range ch {
    var event TaskEvent
    json.Unmarshal([]byte(msg.Payload), &event)
    // handle event
}
```

---

## 7. Error Handling

| HTTP Status | Meaning | Recovery |
|-------------|---------|----------|
| `400` | Malformed request | Fix request body |
| `401` | Invalid/missing token | Check Bearer token |
| `403` | Insufficient skills | Agent lacks required skills for this task |
| `404` | Resource not found | Check task/agent ID |
| `409` | State conflict | Check current task state and retry with valid transition |
| `429` | Rate limited | Wait and retry with exponential backoff |
| `500` | Server error | Retry with backoff |

**Retry strategy for 409 conflicts:**
1. Fetch current task state: `GET /tasks/:id`
2. Determine the correct next action based on the state machine
3. Retry the appropriate action

**Rate limit handling:**
```python
import time
import requests

def send_heartbeat(name, token):
    while True:
        resp = requests.post(
            "http://acb:8080/agents/heartbeat",
            headers={"Authorization": f"Bearer {token}"},
            json={"name": name}
        )
        if resp.status_code == 429:
            time.sleep(10)  # wait before retry
            continue
        resp.raise_for_status()
        time.sleep(10)  # normal interval
```

---

## 8. Uploading Artifacts

Agents can upload files as artifacts attached to tasks. Artifacts are stored in RustFS (S3-compatible object storage).

### Upload

```http
POST /tasks/:id/artifacts
Authorization: Bearer your-secret-token
Content-Type: multipart/form-data

(file: report.pdf)
```

The key is auto-generated as `{task_id}/{uuid}_{filename}`. Content-Type is auto-detected. Response includes the key, bucket, size, and content type.

### List

```http
GET /tasks/:id/artifacts
Authorization: Bearer your-secret-token
```

Returns the list of all artifacts for a task.

### Download

```http
GET /tasks/:id/artifacts?key={url-encoded-key}
Authorization: Bearer your-secret-token
```

Downloads the artifact binary stream with correct Content-Type.

### Delete

```http
DELETE /tasks/:id/artifacts?key={url-encoded-key}
Authorization: Bearer your-secret-token
```

Deletes the artifact from storage and removes its metadata.

### Python Example

```python
# Upload artifact
with open("report.pdf", "rb") as f:
    resp = requests.post(
        f"{BASE}/tasks/t001/artifacts",
        headers={"Authorization": f"Bearer {TOKEN}"},
        files={"file": ("report.pdf", f, "application/pdf")}
    )
    artifact = resp.json()
    print(f"Uploaded: {artifact['key']} ({artifact['size']} bytes)")

# Download artifact
resp = requests.get(
    f"{BASE}/tasks/t001/artifacts",
    headers={"Authorization": f"Bearer {TOKEN}"},
    params={"key": artifact["key"]}
)
with open("downloaded_report.pdf", "wb") as f:
    f.write(resp.content)
```

---

## 9. Quick Reference

```python
# Python agent example
import requests

BASE = "http://acb:8080"
TOKEN = "your-secret-token"
HEADERS = {"Authorization": f"Bearer {TOKEN}", "Content-Type": "application/json"}

# Heartbeat
requests.post(f"{BASE}/agents/heartbeat", headers=HEADERS, json={"name": "agent-alpha"})

# Claim
requests.post(f"{BASE}/tasks/t001/claim", headers=HEADERS, json={"assignee": "agent-alpha"})

# Start
requests.post(f"{BASE}/tasks/t001/start", headers=HEADERS)

# Block
requests.post(f"{BASE}/tasks/t001/block", headers=HEADERS,
    json={"gate_id": "g001", "question": "Proceed?"})

# Complete
requests.post(f"{BASE}/tasks/t001/complete", headers=HEADERS,
    json={"summary": "Done"})

# Fail
requests.post(f"{BASE}/tasks/t001/fail", headers=HEADERS,
    json={"reason": "Error"})
```
