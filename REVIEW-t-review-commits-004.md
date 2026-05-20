# Code Review: acb-go Commits 68fa8c5, 63eb8c4, 4daee5c

**Reviewer:** Braulio  
**Date:** 2026-05-20  
**Scope:** Last 3 commits — timeout service, skill catalog validation, docs updates

---

## Commit 68fa8c5 — Pending Task Timeout Service

**Files:** `internal/timeout/timeout.go`, `internal/timeout/timeout_test.go`, `internal/db/task_repo.go` (ExpirePendingTasks), `internal/db/task_repo_timeout_test.go`, `main.go`

### Code Quality

- **timeout.go**: Clean structure. `PendingTimeoutService` is well-named. The `Start`/`Stop` lifecycle is idiomatic Go (channel-based shutdown). The immediate first check on start (line 48) is a good design choice.
- **Stop() does not wait**: `close(s.stopCh)` signals the goroutine but does not block until it exits. Under graceful shutdown, this means the goroutine may still be running when `main()` returns and `database.Close()` is called in the deferred cleanup, causing a **potential panic** if `check()` hits the DB after close. Should use a `sync.WaitGroup` or `done` channel to wait for goroutine exit.
- **Logging**: Uses `log.Printf` instead of structured logging (slog). Consistent with the rest of the codebase but worth noting as tech debt.

### ExpirePendingTasks — CRITICAL SQL Bug

- **Line 324**: `'-' || ? || ' minutes'` — SQLite's `||` operator converts the integer parameter to its string representation, but with `?` as a parameterized value, SQLite will interpret `'-' || 15 || ' minutes'` as the string `"-15 minutes"` which is a **valid SQLite modifier**, so this *does work*. However, the `||` concatenation with a parameterized `?` is fragile and confusing. The integer parameter should be cast: `cast(? as text)`. More critically, if `timeoutMinutes` is negative or zero, the SQL will produce nonsense (e.g., `'-0 minutes'` = `datetime('now')` which would expire *everything*). The `Start()` method guards against `timeoutMin <= 0`, but `ExpirePendingTasks` itself has **no guard** — it's a public method that could be called directly.

### Race Conditions

- **Timeout goroutine + HTTP handlers share TaskRepo**: SQLite is configured with `SetMaxOpenConns(1)` (single-writer). The timeout goroutine's `ExpirePendingTasks` does SELECT then individual UPDATEs in a loop (lines 321–356). Meanwhile, an HTTP `ClaimTask` handler does its own UPDATE (`status = 'claimed' WHERE status = 'pending'`). Because SQLite serializes writes, this is safe at the DB level — the UPDATE in `ExpirePendingTasks` includes `AND status = 'pending'` (line 348), so if a task was claimed between the SELECT and UPDATE, the UPDATE becomes a no-op (0 rows affected). **This is correct** but not documented, and the error handling silently continues on failure (line 352–354). The partial-update scenario (some tasks expired, some not) is acceptable but should be logged more explicitly.
- **No mutex/protection on the repo struct itself**: `TaskRepo` holds a `*sql.DB` which is safe for concurrent use. No issues here.

### Test Coverage

- **task_repo_timeout_test.go**: 4 test cases — no expired, single expired, claimed-not-expired, multiple expired. Good coverage of the repo method.
- **timeout_test.go**: 2 test cases — disabled mode, and runs-check (integration with repo). The `RunsCheck` test uses `time.Sleep(1s)` which is flaky on slow CI. Should use a synchronization mechanism (e.g., poll with timeout).
- **Missing tests**: No test for `Stop()` behavior (does it actually stop?). No test for concurrent `ExpirePendingTasks` + `ClaimTask` (the key race scenario). No test for edge case: what if `timeoutMinutes` is passed as 0 or negative directly to `ExpirePendingTasks`.

### Findings

| # | Severity | Finding |
|---|----------|---------|
| T1 | **HIGH** | `Stop()` does not wait for goroutine exit — potential panic on shutdown if goroutine accesses DB after `database.Close()` |
| T2 | **HIGH** | `ExpirePendingTasks` has no guard against `timeoutMinutes <= 0` — would expire all pending tasks or produce invalid SQL |
| T3 | **MEDIUM** | SQL `||` concatenation with `?` parameter is fragile/confusing — use `printf`-style modifier or `cast(? as text)` |
| T4 | **MEDIUM** | Test `RunsCheck` uses `time.Sleep` — flaky on slow CI, use poll-with-timeout pattern |
| T5 | **LOW** | Partial expiration (some tasks fail to expire) is silently continued — consider returning partial results or aggregate error |
| T6 | **LOW** | `title` column is selected in ExpirePendingTasks query (line 323) but never used — dead column |

---

## Commit 63eb8c4 — Skill Catalog Validation

**Files:** `internal/config/config.go`, `internal/api/task_handler.go`, `internal/api/agent_handler.go`, `internal/api/router.go`, `main.go`

### Code Quality

- **config.go**: Clean `getEnvList()` helper with proper trimming. `IsValidSkill` and `ValidateSkills` are straightforward. The "empty catalog = allow all" behavior (line 87) is a reasonable default.
- **task_handler.go**: Skill validation at lines 56–68 is clear and well-placed (before DB write). Two separate validation blocks for `RequiredSkills` and `Skills` — slightly repetitive but readable.
- **agent_handler.go**: Skill validation at lines 105–110 mirrors the task handler pattern. Consistent.

### Security — Skill Validation Bypass

- **CRITICAL**: `if h.cfg != nil && len(input.RequiredSkills) > 0` — the `h.cfg != nil` check means **if cfg is nil, validation is entirely skipped**. Looking at `router.go` line 34, `cfg` is always passed from `NewRouter`, so in practice it's non-nil. But the nil check creates a **bypass path** — if someone refactors and accidentally passes nil, all skills would be accepted. The guard should be on the *catalog* being empty (`len(c.AllowedSkills) == 0`), which is already handled by `IsValidSkill`. Remove the `h.cfg != nil` guard and just call `h.cfg.ValidateSkills()` — it safely returns empty when catalog is empty.
- **Same issue in agent_handler.go** line 105: `if h.cfg != nil && len(input.Skills) > 0` — identical bypass risk.

### Tags Validation Missing

- `config.go` defines `AllowedTags []string` (line 25) loaded from `ACB_ALLOWED_TAGS` env var (line 45), but **no `ValidateTags()` method** exists, and **no handler validates tags** at task creation or agent registration. If the intent is to constrain tags the same way as skills, this is incomplete. The env var and field are dead code.

### Architecture

- Passing `*config.Config` directly to handlers is fine for now, but couples the API layer to the full config struct. A `SkillValidator` interface would be cleaner for testability — you could inject a mock validator in tests. Current approach requires constructing a full `Config` for handler tests.
- The error messages expose the full allowed list (e.g., `"allowed: %v"` with `h.cfg.AllowedSkills`). This is helpful for debugging but leaks the full catalog to unauthorized callers who send bad skills. Consider returning only the invalid skills without the full list.

### Findings

| # | Severity | Finding |
|---|----------|---------|
| S1 | **HIGH** | `h.cfg != nil` guard in task_handler.go:56 and agent_handler.go:105 creates a validation bypass path — remove nil check, rely on Config's "empty catalog = allow all" logic |
| S2 | **MEDIUM** | `AllowedTags` field and `ACB_ALLOWED_TAGS` env var are loaded but never validated — dead code or incomplete feature |
| S3 | **MEDIUM** | Error responses expose full `AllowedSkills` list — information disclosure to unauthenticated/low-privilege callers |
| S4 | **LOW** | Duplicate validation pattern (RequiredSkills + Skills) — could extract to a helper `validateAllSkills(skills, requiredSkills)` |
| S5 | **LOW** | Handlers depend on `*config.Config` concrete type — consider `SkillValidator` interface for testability |

---

## Commit 4daee5c — Docs Updates

**Files:** `AGENTS.md`, `docs/api-reference.md`, `docs/agent-integration.md`

### Quality

- **AGENTS.md**: Updated with skills catalog validation, pending timeout, and env vars. Accurate and complete. Directory structure updated to include `timeout/` package.
- **api-reference.md**: Added "Pending Task Timeout" section (lines 44–52) and skills validation documentation (lines 62–68, 319–323). The timeout section is clear. Skills validation docs accurately describe the 400 response.
- **agent-integration.md**: Added skills catalog section (lines 40–46) and pending timeout note (line 48). Both are well-placed and accurate.

### Findings

| # | Severity | Finding |
|---|----------|---------|
| D1 | **MEDIUM** | Docs say `ACB_ALLOWED_TAGS` in AGENTS.md env var list, but there is no API validation for tags — misleading documentation |
| D2 | **LOW** | api-reference.md line 46 says reason is `"expired: pending timeout"` but actual code sets `summary = 'Task expired: no agent claimed within timeout period'` — doc/code mismatch |
| D3 | **LOW** | agent-integration.md line 48 says `"expired: pending timeout"` — same mismatch as D2 |

---

## Overall Verdict

**3 commits, 12 findings total: 2 HIGH, 4 MEDIUM, 6 LOW**

### Must Fix Before Merge (HIGH)

1. **T1** — `Stop()` must wait for goroutine exit to prevent shutdown panic
2. **T2** — `ExpirePendingTasks` must guard against `timeoutMinutes <= 0`
3. **S1** — Remove `h.cfg != nil` guards in handlers — rely on Config's empty-catalog logic instead

### Should Fix (MEDIUM)

4. **T3** — SQL concatenation fragility in `ExpirePendingTasks`
5. **T4** — Flaky `time.Sleep`-based test
6. **S2** — `AllowedTags` is dead code
7. **S3** — Full skill catalog exposure in error responses
8. **D1** — Docs reference tag validation that doesn't exist
9. **D2/D3** — Doc/code mismatch on expiry message

### Assessment

The timeout service is well-structured but has a critical shutdown-safety gap and a missing input guard. The skill catalog validation is a solid feature but the nil-check bypass and incomplete tag validation undermine the security intent. Code is idiomatic Go, consistent with the existing codebase style. Test coverage is reasonable but missing the most important edge case (concurrent access) and the shutdown path.

**Overall: CONDITIONAL APPROVE** — merge after fixing HIGH items.