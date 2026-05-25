# BUG: Skills column missing from agents and tasks tables

Labels: bug, priority:critical

## Description
The spec (ACB_SPECIFICATION.md v3) defines `skills TEXT (JSON)` in the `agents` table and `required_skills TEXT (JSON)` in the `tasks` table. Neither column exists in the Go implementation.

### Impact
- Skill validation on claim does not work. The spec says 403 if agent lacks required skills, but there are no skills to validate.
- `AgentRepo` has no method to query agent skills.
- `CreateTask` handler does not parse `required_skills`.
- `ListTasks` cannot filter by skill.

### Files Affected
- `internal/db/schema.go` — missing columns
- `internal/db/agent_repo.go` — no skills field
- `internal/db/task_repo.go` — no required_skills field
- `internal/models/agent.go` — missing Skills field
- `internal/models/task.go` — missing RequiredSkills field
- `internal/api/task_handlers.go` — no skill validation on claim

### Fix
1. Add `skills TEXT NOT NULL DEFAULT '[]'` to `agents` table migration
2. Add `required_skills TEXT NOT NULL DEFAULT '[]'` and `tags TEXT NOT NULL DEFAULT '[]'` to `tasks` table migration
3. Update `Agent` and `Task` structs with JSON-tagged fields
4. Implement skill validation in `ClaimTask`: decode agent skills + task required_skills, return 403 if intersection is empty
5. Add `required_skills` filter to `ListTasks`

**Severity: Critical** — skill-based routing is completely non-functional.
**Source:** Code review by Braulio, Armando, and Quique agents.
