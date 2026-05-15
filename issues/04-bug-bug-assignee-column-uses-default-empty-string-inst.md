# BUG: assignee column uses DEFAULT empty string instead of NULL

Labels: bug, priority:low

## Description
The spec says `assignee` should be NULL until claimed. But `schema.go` defines:
```go
`assignee TEXT NOT NULL DEFAULT '',`
```
Empty string != NULL. Queries using `IS NULL` will never match.

### Fix
- Change to `assignee TEXT DEFAULT NULL`
- Remove NOT NULL constraint
- Update repository queries to use `IS NULL` checks

**Severity: Low** — works but diverges from spec.
**Source:** Armando's and Braulio's reviews.
