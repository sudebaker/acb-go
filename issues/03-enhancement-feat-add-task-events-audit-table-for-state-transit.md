# FEAT: Add task_events audit table for state transitions

Labels: enhancement, priority:medium

## Description
Task state transitions are only reflected in the final `status` field. No record of when each transition occurred or who triggered it.

### Proposed Fix
Add a `task_events` table:
```sql
CREATE TABLE IF NOT EXISTS task_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id TEXT NOT NULL REFERENCES tasks(id),
    event TEXT NOT NULL,
    agent TEXT NOT NULL,
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
    detail TEXT
);
CREATE INDEX idx_task_events_task ON task_events(task_id);
```

**Severity: Medium** — important for observability but not blocking.
**Source:** Armando's review.
