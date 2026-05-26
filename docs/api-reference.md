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

Returns the status of all system dependencies.

**Response `200` (all healthy):**
```json
{
  "status": "ok",
  "checks": {
    "database": {"status": "ok"},
    "redis": {"status": "ok"},
    "storage": {"status": "ok"}
  }
}
```

**Response `200` (degraded — some dependencies unavailable):**
```json
{
  "status": "degraded",
  "checks": {
    "database": {"status": "ok"},
    "redis": {"status": "error", "error": "dial tcp: ..."},
    "storage": {"status": "disabled"}
  }
}
```

The endpoint always returns HTTP 200 so that load balancers and orchestration systems do not kill the process for transient failures. Dependencies not configured (e.g., Redis or RustFS) are reported as `"disabled"` rather than `"error"`.

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

### Gate Lifecycle

```
gate: pending ──→ asked (agent answers — POST .../gates/:id/answer)
       asked ──→ answered (orchestrator approves — POST .../gates/:id/approve)
       answered ──→ resolved (orchestrator unblocks — POST .../unblock)
```

When a task is blocked, a gate is created in `pending` status. The assigned agent receives a notification and can submit their answer via `POST /tasks/:id/gates/:gate_id/answer`, transitioning the gate to `asked`. The orchestrator reviews the answer and either approves it via `POST /tasks/:id/gates/:gate_id/approve` (transitioning to `answered`) or rejects it (agent must answer again). Once `answered`, the orchestrator calls `POST /tasks/:id/unblock` to resolve the gate and return the task to `in_progress`.

### Pending Task Timeout

Unclaimed tasks in `pending` state are automatically expired after a configurable timeout. When a task exceeds the timeout, its status transitions to `failed` with reason `"expired: pending timeout"`.

**Configuration:**
- `ACB_PENDING_TIMEOUT_MIN` — timeout in minutes (default: `15`)
- `ACB_PENDING_TIMEOUT_CHECK_SEC` — check interval in seconds (default: `60`)

A background goroutine runs at the configured interval, scanning for `pending` tasks older than the timeout and transitioning them to `failed`.

---

### `POST /tasks`

Create a new task.

**Auth:** Bearer token required

**Skills validation:** If `ACB_ALLOWED_SKILLS` is configured, each skill in `required_skills` must be in the allowed list. Tasks with invalid skills receive `400`:

```json
{"error": "invalid_required_skills", "message": "one or more required skills are not in the allowed catalog"}
```

If `ACB_ALLOWED_SKILLS` is not set (empty), all skills are accepted.

**Tags validation:** If `ACB_ALLOWED_TAGS` is configured, each tag must be in the allowed list. Tasks with invalid tags receive `400`:

```json
{"error": "invalid_tags", "message": "one or more tags are not in the allowed catalog"}
```

If `ACB_ALLOWED_TAGS` is not set (empty), all tags are accepted.

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

### `GET /tasks/dispatch`

Get the next best-matching pending task for an agent. Smart polling endpoint for agents without webhook capability.

**Auth:** Bearer token required

**Query parameters:** `?agent=<name>`

Returns the highest-priority pending task whose `required_skills` are a subset of the agent's skills. Marks the task as `dispatched` to prevent duplicate assignment.

**Response `200`:** Full task object (same as GET /tasks/:id)

**Response `204`:** No matching tasks available

**Response `400`:**
```json
{"error": "missing_agent", "message": "agent query parameter is required"}
```

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

Unblock a task by resolving its gate (used by orchestrator).

**Auth:** Bearer token required

**Request body:**
```json
{"gate_id": "g_001"}
```

The gate must be in `answered` status. To reach `answered`: the agent calls `POST /tasks/:id/gates/:gate_id/answer` (pending→asked), then the orchestrator calls `POST /tasks/:id/gates/:gate_id/approve` (asked→answered).

**Response `200`:**
```json
{"id": "t_a1b2c3d4", "status": "in_progress"}
```

**Response `409`:**
- If gate cannot be resolved

---

### `POST /tasks/:id/gates/:gate_id/answer`

Submit an agent's answer to a gate. The agent transitions the gate from `pending` to `asked` with their response. The orchestrator later reviews the answer and decides whether to unblock.

**Auth:** Bearer token required

**Request body:**
```json
{"answer": "I recommend proceeding with the deployment. All checks passed."}
```

**Response `200`:**
```json
{"gate_id": "g_001", "status": "asked"}
```

**Response `400`:**
- `missing_answer` — answer field is required
- `gate_mismatch` — gate does not belong to the task

**Response `404`:**
- `gate_not_found` — gate does not exist

**Response `409`:**
- `invalid_gate_status` — gate is not in `pending` status

---

### `POST /tasks/:id/gates/:gate_id/approve`

Approve an agent's gate answer. The orchestrator transitions the gate from `asked` to `answered`, signaling that the gate answer is accepted and the task can be unblocked.

**Auth:** Bearer token required

**Request body:**
```json
{"answer": "Approved — proceed with deployment."}
```

**Response `200`:**
```json
{"gate_id": "g_001", "status": "answered"}
```

**Response `400`:**
- `missing_answer` — answer field is required
- `gate_mismatch` — gate does not belong to the task

**Response `404`:**
- `gate_not_found` — gate does not exist

**Response `409`:**
- `invalid_gate_status` — gate is not in `asked` status
- `approve_failed` — gate could not be transitioned to answered

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

### `POST /agents`

Register or update an agent with webhook URL for task dispatch.

**Auth:** Bearer token required. Agents can only register themselves (X-Agent-Name must match).

**Skills validation:** If `ACB_ALLOWED_SKILLS` is configured, each skill in the agent's `skills` array must be in the allowed list. Registration with invalid skills receives `400`:

```json
{"error": "invalid_skills", "message": "one or more skills are not in the allowed catalog"}
```

If `ACB_ALLOWED_SKILLS` is not set (empty), all skills are accepted.

```json
{"error": "invalid_skills", "message": "invalid agent skills: [\"hacking\"]", "allowed": ["coding","review","testing",...]}
```

**Request body:**
```json
{
  "name": "agent-2",
  "port": 8645,
  "token": "<AGENT_TOKEN>",
  "skills": ["go", "testing", "devops"],
  "webhook_url": "http://localhost:8645/webhooks/orchestrator",
  "webhook_secret": "<WEBHOOK_SECRET>"
}
```

**Security:** Webhook URLs are validated at registration:
- Must use `http://` or `https://` scheme
- Resolved IPs are checked against private ranges (RFC 1918, loopback, link-local)
- Prevents SSRF attacks via internal network access

**Response `200`:** Agent object (token redacted)

**Response `403`:**
```json
{"error": "forbidden", "message": "agent can only register itself"}
```

**Response `400`:**
```json
{"error": "invalid_webhook_url", "message": "webhook URL rejected: resolves to private IP"}
```

---

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
| `missing_answer` | 400 | Answer is required for gate answer |
| `gate_mismatch` | 400 | Gate does not belong to the specified task |
| `gate_not_found` | 404 | Gate does not exist |
| `invalid_gate_status` | 409 | Gate is not in pending status |
| `ask_gate_failed` | 409 | Gate could not be transitioned to asked |
| `approve_failed` | 409 | Gate could not be transitioned to answered |
| `missing_name` | 400 | Agent name required for heartbeat |
| `missing_file` | 400 | File field missing in upload |
| `empty_file` | 400 | Uploaded file is empty |
| `missing_key` | 400 | Key query parameter required |
| `invalid_form` | 400 | Malformed multipart form |
| `invalid_webhook_url` | 400 | Webhook URL validation failed (SSRF, invalid scheme) |
| `ssrf_blocked` | 400 | Webhook URL resolves to private/blocked IP |
| `forbidden` | 403 | Agent attempted to register with a different name |
| `dispatch_failed` | 500 | Webhook dispatch to agent failed (logged, retried) |
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

## Webhook Dispatch

When a task is created, ACB automatically dispatches it to matching agents via their `webhook_url`.

### Flow

```
Task Created → Match agents by required_skills → POST to each agent's webhook_url
              ↓ (if webhook fails)                ↓ (success)
         Queue in Redis for retry          Agent receives task notification
              ↓ (goroutine, exponential backoff)
         Retry up to 5 times (5s, 25s, 125s)
              ↓ (all retries exhausted)
         Mark as dispatch_failed (agent can still poll)
```

### Webhook Payload

```json
{
  "action": "new_task",
  "task": {
    "id": "...",
    "title": "...",
    "required_skills": ["go"],
    "body_goal": "..."
  },
  "timestamp": "2026-05-16T21:00:00Z"
}
```

### Signature Verification

Each webhook POST includes two headers:
- `X-Webhook-Signature`: HMAC-SHA256(webhook_secret, timestamp + "." + body) as hex
- `X-Webhook-Timestamp`: Unix timestamp of the dispatch

Receivers should:
1. Reject if `|current_time - timestamp| > 5 minutes` (replay protection)
2. Recompute HMAC: `SHA256(secret, timestamp + "." + body)` and compare with `X-Webhook-Signature`

### Agent Registration for Dispatch

Agents must register with a `webhook_url` and `webhook_secret` via `POST /agents`. See the Agents section above.

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
| `gate_answered` | Gate answer submitted | `{"event":"gate_answered","task_id":"t_123","gate_id":"g_001"}` |
| `gate_approved` | Gate answer approved | `{"event":"gate_approved","task_id":"t_123","gate_id":"g_001"}` |
| `task_completed` | Task completed | `{"event":"task_completed","task_id":"t_123"}` |
| `task_failed` | Task failed | `{"event":"task_failed","task_id":"t_123"}` |

Events are fire-and-forget via goroutines. Redis publish errors are logged but never block the request.

---

## Environment Variables

| Variable | Description | Default |
|---|---|---|
| `ACB_PORT` | HTTP server port | `8090` |
| `ACB_PG_HOST` | PostgreSQL host | `localhost` |
| `ACB_PG_PORT` | PostgreSQL port | `5433` |
| `ACB_PG_USER` | PostgreSQL user | `acb` |
| `ACB_PG_PASSWORD` | PostgreSQL password | `acb-secure-pg-pass-2026` |
| `ACB_PG_DATABASE` | PostgreSQL database name | `acb` |
| `ACB_REDIS_ADDR` | Redis address | `redis:6379` |
| `ACB_REDIS_PASS` | Redis password | (empty) |
| `ACB_RUSTFS_ENDPOINT` | RustFS S3-compatible endpoint | `rustfs:9000` |
| `ACB_RUSTFS_BUCKET` | RustFS bucket name | `acb-artifacts` |
| `ACB_RUSTFS_REGION` | RustFS region (preferred). Falls back to `RUSTFS_REGION`. | `us-east-1` |
| `ACB_WEBHOOK_SECRET_KEY` | Base64-encoded 32-byte key for webhook secret encryption. Falls back to `WEBHOOK_SECRET_KEY`. | (required for webhook secrets) |
| `ACB_ADMIN_TOKEN` | Bootstrap admin token. If unset, a random token is generated and printed to stderr on every restart. | (auto-generated) |
| `ACB_ALLOWED_SKILLS` | Comma-separated allowed skills catalog. | (all skills allowed) |
| `ACB_ALLOWED_TAGS` | Comma-separated allowed tags catalog. | (all tags allowed) |
| `ACB_PENDING_TIMEOUT_MIN` | Minutes before unclaimed pending tasks auto-expire (0 = disabled). | `15` |
| `ACB_PENDING_TIMEOUT_CHECK_SEC` | Seconds between timeout checks. | `60` |
| `ACB_ALLOW_HTTP_WEBHOOKS` | Set to `1` to allow `http://` webhook URLs (internal networks). | `0` (https only) |
| `ACB_LOG_LEVEL` | Log level (`debug`, `info`, `warn`, `error`). | `info` |
| `ACB_ARTIFACT_TTL_DAYS` | Artifact TTL in days. | `30` |
| `ACB_MAX_UPLOAD_SIZE_MB` | Max upload size in MB. | `32` |

## cURL Examples

```bash
# Create a task with skill requirements
curl -s -X POST http://localhost:8090/tasks \
  -H "Authorization: Bearer test-token" \
  -H "Content-Type: application/json" \
  -d '{"id":"t001","title":"test","required_skills":["python","linux"],"priority":3}'

# Claim a task
curl -s -X POST http://localhost:8090/tasks/t001/claim \
  -H "Authorization: Bearer test-token" \
  -H "Content-Type: application/json" \
  -d '{"assignee":"agent-alpha"}'

# Send heartbeat
curl -s -X POST http://localhost:8090/agents/heartbeat \
  -H "Authorization: Bearer test-token" \
  -H "Content-Type: application/json" \
  -d '{"name":"agent-alpha"}'

# List pending tasks matching agent skills
curl -s "http://localhost:8090/tasks?status=pending" \
  -H "Authorization: Bearer test-token"

# Register an agent with webhook URL
curl -s -X POST http://localhost:8090/agents \
  -H "Authorization: Bearer <AGENT_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{"name":"agent-2","port":8645,"token":"<AGENT_TOKEN>","skills":["go","testing"],"webhook_url":"http://localhost:8645/webhooks/orchestrator","webhook_secret":"<WEBHOOK_SECRET>"}'

# Get next matching task for polling
curl -s "http://localhost:8090/tasks/dispatch?agent=agent-2" \
  -H "Authorization: Bearer <AGENT_TOKEN>"
```
