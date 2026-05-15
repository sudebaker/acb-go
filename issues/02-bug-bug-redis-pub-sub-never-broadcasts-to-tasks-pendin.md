# BUG: Redis Pub/Sub never broadcasts to tasks:pending

Labels: bug, priority:critical

## Description
`PublishTaskEvent` always publishes to `ChannelPrefix + agent` (events.go line ~58). This means:
- `new_task` events go to `agent:` (empty agent = malformed channel)
- `task_blocked` never reaches `tasks:gates`
- Agents without a known assignee never get notified

### Impact
- Agents relying on Redis subscription for real-time task notification will never receive `new_task` broadcasts
- The only workaround is constant HTTP polling of `GET /tasks?status=pending`
- Gate notifications are lost

### Fix
1. Map event types to channels: `new_task` -> `tasks:pending`, `task_blocked`/`task_unblocked` -> `tasks:gates`
2. Update `TaskEvent` struct to include `RequiredSkills []string`
3. Add channel routing logic in `PublishTaskEvent`

**Severity: Critical** — real-time notification system is non-functional for broadcasts.
**Source:** All three reviewers identified this independently.
