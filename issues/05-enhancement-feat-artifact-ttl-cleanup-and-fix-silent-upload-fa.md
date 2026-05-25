# FEAT: Artifact TTL, cleanup, and fix silent upload failure

Labels: enhancement, priority:medium

## Description
1. Completed/failed tasks retain artifacts indefinitely — no TTL or cleanup.
2. When RustFS is disabled, uploads silently succeed (201) but data is lost.
3. 32MB hardcoded upload limit should be configurable.

### Proposed Fix
1. Add `artifact_ttl_days` config option (default: 30)
2. Add cleanup goroutine or endpoint
3. Return 503 when RustFS is disabled
4. Make max upload size configurable

**Severity: Medium** — important for production.
**Source:** All three reviewers.
