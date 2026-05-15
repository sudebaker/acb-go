#!/bin/bash
# Script para crear issues en GitHub para el repo sudebaker/acb-go
# Ejecutar con: GITHUB_TOKEN=tu_token_aqui ./create_github_issues.sh

set -e

REPO="sudebaker/acb-go"
API_URL="https://api.github.com/repos/$REPO/issues"

if [ -z "$GITHUB_TOKEN" ]; then
    echo "ERROR: GITHUB_TOKEN not set"
    echo "Por favor, ve a https://github.com/settings/tokens y crea un personal access token con scopes repo, write:org"
    echo "Luego ejecuta:"
    echo "  export GITHUB_TOKEN='tu_token_aqui'"
    echo "  bash create_github_issues.sh"
    exit 1
fi

echo "Creating issues for $REPO..."

# Issue 1: Skills column missing (CRITICAL)
echo "Creating issue 1: Skills column missing..."
curl -s -X POST "$API_URL" \
  -H "Authorization: token $GITHUB_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "title": "BUG: Skills column missing from agents and tasks tables",
    "labels": ["bug", "priority:critical"],
    "body": "## Description\nThe spec (ACB_SPECIFICATION.md v3) defines `skills TEXT (JSON)` in the `agents` table and `required_skills TEXT (JSON)` in the `tasks` table. Neither column exists in the Go implementation.\n\n### Impact\n- Skill validation on claim does not work. The spec says 403 if agent lacks required skills, but there are no skills to validate.\n- `AgentRepo` has no method to query agent skills.\n- `CreateTask` handler does not parse `required_skills`.\n- `ListTasks` cannot filter by skill.\n\n### Files Affected\n- `internal/db/schema.go` — missing columns\n- `internal/db/agent_repo.go` — no skills field\n- `internal/db/task_repo.go` — no required_skills field\n- `internal/models/agent.go` — missing Skills field\n- `internal/models/task.go` — missing RequiredSkills field\n- `internal/api/task_handlers.go` — no skill validation on claim\n\n### Fix\n1. Add `skills TEXT NOT NULL DEFAULT '\''[]'\''` to `agents` table migration\n2. Add `required_skills TEXT NOT NULL DEFAULT '\''[]'\''` and `tags TEXT NOT NULL DEFAULT '\''[]'\''` to `tasks` table migration\n3. Update `Agent` and `Task` structs with JSON-tagged fields\n4. Implement skill validation in `ClaimTask`: decode agent skills + task required_skills, return 403 if intersection is empty\n5. Add `required_skills` filter to `ListTasks`\n\n**Severity: Critical** — skill-based routing is completely non-functional.\n**Source:** Code review by Braulio, Armando, and Quique agents.\n'

# Issue 2: Redis Pub/Sub not broadcasting (CRITICAL)
echo "Creating issue 2: Redis Pub/Sub broadcasting..."
curl -s -X POST "$API_URL" \
  -H "Authorization: token $GITHUB_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "title": "BUG: Redis Pub/Sub only sends to agent channel, never broadcasts to tasks:pending",
    "labels": ["bug", "priority:critical"],
    "body": "## Description\n`PublishTaskEvent` in `internal/redis/events.go` always publishes to `ChannelPrefix + agent` (line ~58). This means:\n- `new_task` events go to `agent:` (empty agent = malformed channel)\n- `task_blocked` never reaches `tasks:gates`\n- Agents without a known assignee never get notified\n\n### Impact\n- Agents relying on Redis subscription for real-time task notification will never receive `new_task` broadcasts\n- The only workaround is constant HTTP polling of `GET /tasks?status=pending`\n- Gate notifications are lost\n\n### Fix\n1. Map event types to channels:\n   - `new_task` -> publish to `tasks:pending` AND `agent:<assignee>` (if set)\n   - `task_blocked`/`task_unblocked` -> publish to `tasks:gates`\n   - Agent-specific events -> `agent:<name>`\n2. Update `TaskEvent` struct to include `RequiredSkills []string`\n3. Add channel routing logic in `PublishTaskEvent`\n\n**Severity: Critical** — real-time notification system is non-functional for broadcasts.\n**Source:** All three reviewers identified this independently.\n'
# Note: Issue is already fixed in code

# Issue 3: Audit table (MEDIUM)
echo "Creating issue 3: Audit table for task events..."
curl -s -X POST "$API_URL" \
  -H "Authorization: token $GITHUB_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "title": "FEAT: Add task_events audit table for state transitions",
    "labels": ["enhancement", "priority:medium"],
    "body": "## Description\nTask state transitions are only reflected in the final `status` field. No record of when each transition occurred or who triggered it.\n\n### Proposed Fix\nAdd a `task_events` table:\n```sql\nCREATE TABLE IF NOT EXISTS task_events (\n    id INTEGER PRIMARY KEY AUTOINCREMENT,\n    task_id TEXT NOT NULL REFERENCES tasks(id),\n    event TEXT NOT NULL,\n    agent TEXT NOT NULL,\n    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,\n    detail TEXT\n);\nCREATE INDEX idx_task_events_task ON task_events(task_id);\nCREATE INDEX idx_task_events_timestamp ON task_events(timestamp);\n```\n\n**Severity: Medium** — important for observability but not blocking.\n**Source:** Armando'\''s review."
}'

# Issue 4: Assignee NULL (LOW)
echo "Creating issue 4: Assignee column NULL value..."
curl -s -X POST "$API_URL" \
  -H "Authorization: token $GITHUB_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "title": "BUG: assignee column uses DEFAULT empty string instead of NULL",
    "labels": ["bug", "priority:low"],
    "body": "## Description\nThe spec says `assignee` should be NULL until claimed. But `schema.go` defines:\n```go\n`assignee TEXT NOT NULL DEFAULT '\'''\''',`\n```\nEmpty string `'\''' != `NULL`. Queries using `IS NULL` will never match.\n\n### Fix\n- Change to `assignee TEXT DEFAULT NULL`\n- Remove `NOT NULL` constraint\n- Update repository queries to use `IS NULL` checks\n\n**Severity: Low** — works but diverges from spec.\n**Source:** Armando'\''s and Braulio'\''s reviews.\n"
}'

# Issue 5: Artifact TTL (MEDIUM)
echo "Creating issue 5: Artifact TTL and cleanup..."
curl -s -X POST "$API_URL" \
  -H "Authorization: token $GITHUB_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "title": "FEAT: Add TTL and cleanup mechanism for artifacts + fix silent upload failure",
    "labels": ["enhancement", "priority:medium"],
    "body": "## Description\n1. Completed/failed tasks retain artifacts in RustFS indefinitely — no TTL or cleanup.\n2. When RustFS is disabled, uploads silently succeed (returning 201) but data is lost.\n3. 32MB hardcoded upload limit should be configurable.\n\n### Proposed Fix\n1. Add `artifact_ttl_days` config option (default: 30)\n2. Add cleanup goroutine or `POST /tasks/:id/artifacts/cleanup` endpoint\n3. Return 503 when RustFS is disabled instead of silently accepting\n4. Make max upload size configurable via env var\n\n**Severity: Medium** — important for production.\n**Source:** All three reviewers identified this independently."
}'

echo ""
echo "=== All issues created! ==="
echo "Open https://github.com/$REPO/issues to verify"
