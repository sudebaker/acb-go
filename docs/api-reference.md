# API Reference

Base URL: `http://<host>:<port>`

## Authentication

All endpoints except `GET /health` require a Bearer token:

```
Authorization: Bearer <agent-token>
```

Tokens are validated against the `agents` table. Requests with invalid or missing tokens receive `401 Unauthorized`.

---

## Health Check

### `GET /health`

**Auth:** None (public)

**Response `200`:**
```json
{"status": "ok"}
```

---

## Tasks

### Task State Machine

```
pending ──→ claimed ──→ in_progress ──→ completed
                            │
                            ├──→ blocked ──→ in_progress (via unblock)
                            │
                            └──→ failed
```

Valid transitions are enforced server-side. Invalid transitions return `409 Conflict`.

---

### `POST /tasks`

Create a new task.

**Auth:** Bearer token required

**Request body:**
```json
{
  "id": "t_a1b2c3d4",
  "title": "Analyze log files",
  "required_skills": ["python", "linux"],
  "priority": 3,
  "tags": ["security", "urgent"],
  "parents": ["t_prev_001"],
  "body_goal": "Find anomalies in the access log",
  "body_context": "Logs are in /var/log/access.log",
  "body_deliverable_format": "markdown"
}
```

All fields except `id` and `title` are optional. `assignee` is omitted at creation — agents self-assign via `claim`. Default status: `pending`.

**Response `201`:**
```json
{
  "id": "t_a1b2c3d4",
  "title": "Analyze log files",
  "assignee": null,
  "required_skills": ["python", "linux"],
  "status": "pending",
  "priority": 3,
  "tags": ["security", "urgent"],
  "parents": ["t_prev_001"],
  "body_goal": "Find anomalies in the access log",
  "body_context": "Logs are in /var/log/access.log",
  "body_deliverable_format": "markdown",
  "created_at": "2025-01-15T10:30:00Z",
  "summary": "",
  "artifacts": []
}
```

**Response `400`:**
```json
{"error": "missing_title", "message": "title is required"}
```

---

### `GET /tasks`

List tasks with optional filters.

**Auth:** Bearer token required

**Query parameters:** `?status=pending` or `?status=pending&required_skills=python,linux`

**Response `200`:**
```json
[
  {
    "id": "t_a1b2c3d4",
    "title": "Analyze log files",
    "assignee": "agent-alpha",
    "status": "pending"
  }
]
```

Returns empty array `[]` when no tasks match.

---

### `GET /tasks/:id`

Get a single task by ID.

**Auth:** Bearer token required

**Response `200`:** Full task object

**Response `404`:**
```json
{"error": "not_found", "message": "task not found"}
```

---

### `POST /tasks/:id/claim`

Claim a pending task for an agent. The ACB validates that the authenticated agent has **all** the skills listed in `required_skills`.

**Auth:** Bearer token required

**Request body:**
```json
{"assignee": "agent-alpha"}
```

**Response `200`:**
```json
{"id": "t_a1b2c3d4", "status": "claimed", "assignee": "agent-alpha"}
```

**Response `403`:**
```json
{"error": "insufficient_skills", "message": "agent lacks required skills for task t_a1b2c3d4"}
```

**Response `409`:**
```json
{"error": "claim_failed", "message": "task t_a1b2c3d4 is claimed, expected pending"}
```

---

### `POST /tasks/:id/start`

Start execution of a claimed task.

**Auth:** Bearer token required

**Request body:** (none)

**Response `200`:**
```json
{"id": "t_a1b2c3d4", "status": "in_progress"}
```

**Response `409`:**
```json
{"error": "start_failed", "message": "task t_a1b2c3d4 is pending, expected claimed"}
```

---

### `POST /tasks/:id/block`

Block a task on a gate (human intervention needed).

**Auth:** Bearer token required

**Request body:**
```json
{
  "gate_id": "g_001",
  "question": "Should we proceed with the deployment?"
}
```

**Response `200`:**
```json
{"status": "blocked", "gate_id": "g_001"}
```

**Response `409`:**
- If task is not `in_progress` or `claimed`

---

### `POST /tasks/:id/unblock`

Unblock a task by answering its gate (used by orchestrator).

**Auth:** Bearer token required

**Request body:**
```json
{"gate_id": "g_001"}
```

The gate must be in `answered` status (answer must have been provided via `AnswerGate`).

**Response `200`:**
```json
{"id": "t_a1b2c3d4", "status": "in_progress"}
```

**Response `409`:**
- If gate cannot be resolved

---

### `POST /tasks/:id/complete`

Complete a task with a summary. Only allowed from `in_progress`.

**Auth:** Bearer token required

**Request body:**
```json
{"summary": "Analysis complete. Found 3 anomalies."}
```

**Response `200`:**
```json
{"id": "t_a1b2c3d4", "status": "completed", "summary": "Analysis complete. Found 3 anomalies."}
```

**Response `409`:**
- If task is not `in_progress`

---

### `POST /tasks/:id/fail`

Mark a task as failed with a reason. Only allowed from `in_progress`.

**Auth:** Bearer token required

**Request body:**
```json
{"reason": "Timeout connecting to database"}
```

**Response `200`:**
```json
{"id": "t_a1b2c3d4", "status": "failed", "summary": "Timeout connecting to database"}
```

**Response `409`:**
- If task is not `in_progress`

---

## Agents

### `POST /agents/heartbeat`

Send a heartbeat signal for an agent. Rate-limited to 10 requests per minute per agent.

**Auth:** Bearer token required

**Request body:**
```json
{"name": "agent-alpha"}
```

If `name` is empty in the body, it falls back to the `X-Agent-Name` header (set by auth middleware).

**Response `200`:**
```json
{"status": "ok"}
```

**Response `404`:**
```json
{"error": "agent_not_found", "message": "agent agent-alpha not found"}
```

**Response `429`:**
```json
{"error": "rate_limited", "message": "too many heartbeats"}
```

---

### `GET /agents/:name`

Get agent status and port. The agent's token is **never returned** (cleared server-side).

**Auth:** Bearer token required

**Response `200`:**
```json
{
  "name": "agent-alpha",
  "port": 8081,
  "last_heartbeat": "2025-01-15T10:30:00Z"
}
```

**Response `404`:**
```json
{"error": "not_found", "message": "agent not found"}
```

---

---

## Artifacts

### `POST /tasks/:id/artifacts`

Upload a file artifact for a task. Uses multipart form upload.

**Auth:** Bearer token required

**Request:** Multipart form with `file` field. Key is auto-generated as `{task_id}/{uuid}_{filename}`. Content-Type is auto-detected from file header bytes.

**Response `201`:**
```json
{
  "key": "t_a1b2c3d4/f47ac10e-59a0-report.pdf",
  "bucket": "acb-artifacts",
  "size": 12345,
  "content_type": "application/pdf"
}
```

**Response `400`:**
```json
{"error": "empty_file", "message": "file is empty"}
```

---

### `GET /tasks/:id/artifacts`

List all artifacts for a task.

**Auth:** Bearer token required

**Response `200`:**
```json
[
  {
    "key": "t_a1b2c3d4/uuid-report.pdf",
    "bucket": "acb-artifacts",
    "size": 12345,
    "content_type": "application/pdf"
  }
]
```

Returns `[]` when no artifacts exist.

---

### `GET /tasks/:id/artifacts?key=<key>`

Download a specific artifact by its key. The key must be URL-encoded.

**Auth:** Bearer token required

**Response `200`:** Binary stream with correct `Content-Type` and `Content-Length` headers.

**Response `404`:**
```json
{"error": "not_found", "message": "artifact not found"}
```

---

### `DELETE /tasks/:id/artifacts?key=<key>`

Delete a specific artifact by its key. Removes both the RustFS object and the task's metadata.

**Auth:** Bearer token required

**Response `204`:** No Content

**Response `404`:**
```json
{"error": "not_found", "message": "artifact not found"}
```

---

## Error Codes

| Code | HTTP Status | Description |
|------|-------------|-------------|
| `invalid_json` | 400 | Malformed request body |
| `missing_title` | 400 | Task title is required |
| `missing_assignee` | 400 | Assignee is required for claim |
| `missing_fields` | 400 | Gate fields required for block |
| `missing_gate_id` | 400 | Gate ID required for unblock |
| `missing_name` | 400 | Agent name required for heartbeat |
| `missing_file` | 400 | File field missing in upload |
| `empty_file` | 400 | Uploaded file is empty |
| `missing_key` | 400 | Key query parameter required |
| `invalid_form` | 400 | Malformed multipart form |
| `unauthorized` | 401 | Missing or invalid Bearer token |
| `insufficient_skills` | 403 | Agent lacks required skills for task |
| `not_found` | 404 | Resource not found |
| `claim_failed` | 409 | Task cannot be claimed |
| `start_failed` | 409 | Task cannot be started |
| `block_failed` | 409 | Task cannot be blocked |
| `resolve_failed` | 409 | Gate cannot be resolved |
| `complete_failed` | 409 | Task cannot be completed |
| `fail_failed` | 409 | Task cannot be failed |
| `rate_limited` | 429 | Too many heartbeats |
| `upload_failed` | 500 | RustFS upload error |
| `download_failed` | 500 | RustFS download error |
| `delete_failed` | 500 | RustFS delete error |
| `add_artifact_failed` | 500 | Failed to save artifact metadata |
| `remove_artifact_failed` | 500 | Failed to remove artifact metadata |
| `create_failed` | 500 | Internal error creating resource |
| `get_failed` | 500 | Internal error fetching resource |
| `list_failed` | 500 | Internal error listing resources |
| `gate_create_failed` | 500 | Internal error creating gate |
| `update_failed` | 500 | Internal error updating resource |
| `check_failed` | 500 | Internal error checking artifact |
| `agent_not_found` | 404 | Agent not registered |

---

## Redis Events

Published on the following channels depending on the event type:

- `agent:<agent_name>` — targeted to the involved agent
- `tasks:pending` — broadcast of new pending tasks
- `tasks:gates` — broadcast of blocked tasks requiring human intervention

| Event | Trigger | Payload |
|-------|---------|---------|
| `new_task` | Task created | `{"event":"new_task","task_id":"t_123","required_skills":["python","linux"],"agent":"agent-alpha"}` |
| `task_claimed` | Task claimed | `{"event":"task_claimed","task_id":"t_123","agent":"agent-alpha"}` |
| `task_started` | Task started | `{"event":"task_started","task_id":"t_123"}` |
| `task_blocked` | Task blocked | `{"event":"task_blocked","task_id":"t_123"}` |
| `task_unblocked` | Task unblocked | `{"event":"task_unblocked","task_id":"t_123"}` |
| `task_completed` | Task completed | `{"event":"task_completed","task_id":"t_123"}` |
| `task_failed` | Task failed | `{"event":"task_failed","task_id":"t_123"}` |

Events are fire-and-forget via goroutines. Redis publish errors are logged but never block the request.

---

## cURL Examples

```bash
# Create a task with skill requirements
curl -s -X POST http://localhost:8080/tasks \
  -H "Authorization: Bearer test-token" \
  -H "Content-Type: application/json" \
  -d '{"id":"t001","title":"test","required_skills":["python","linux"],"priority":3}'

# Claim a task
curl -s -X POST http://localhost:8080/tasks/t001/claim \
  -H "Authorization: Bearer test-token" \
  -H "Content-Type: application/json" \
  -d '{"assignee":"agent-alpha"}'

# Send heartbeat
curl -s -X POST http://localhost:8080/agents/heartbeat \
  -H "Authorization: Bearer test-token" \
  -H "Content-Type: application/json" \
  -d '{"name":"agent-alpha"}'

# List pending tasks matching agent skills
curl -s "http://localhost:8080/tasks?status=pending" \
  -H "Authorization: Bearer test-token"
```
