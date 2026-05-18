# ACB Security Audit Report

**Author:** Agent-3 Security Audit  
**Date:** 2026-05-15  
**Repo:** `/home/amphora/src/acb-go`  
**Priority:** 6  
**Cross-reference:** Docker review (`agent-2-acb-docker-review.md`)

---

## Executive Summary

9 findings: **3 CRITICAL**, **2 HIGH**, **2 MEDIUM**, **2 LOW**. Three main attack surfaces: plaintext authentication, unrestricted SSRF via webhooks, and unauthenticated Redis. The most severe vectors allow full takeover of the agent bus.

---

## Findings

### S01 — CRITICAL: Tokens stored in plaintext

**File:** `internal/db/schema.go:45`, `internal/db/agent_repo.go:14-35`

Agent Bearer tokens are stored unhashed in the `agents.token` column. Authentication (`auth.go:21`) does `WHERE token = ?` comparing the request token directly against the stored value.

**Risk:** Any access to the SQLite database (file read, backup leak, another app's vulnerability) exposes functional tokens. An attacker with DB access impersonates all agents.

**Fix:**
- Store Argon2id/bcrypt hash of the token, never plaintext.
- Compare with hash in auth: `hash(sent_token) == stored_hash`.
- Generate read-only tokens for audit (no header injection capability).

**Status:** 🔴 Open

---

### S02 — CRITICAL: SSRF via unrestricted webhook dispatch

**File:** `internal/dispatcher/dispatcher.go:107-132`

The dispatcher sends HTTP POST to `agent.WebhookURL` — a URL provided by the agent itself during registration. No validation:
- No private IP denylist (RFC 1918, link-local, localhost)
- No domain allowlist
- No scheme validation (accepts `file://`, `gopher://`)
- No differentiated timeout

**Risk:** A malicious agent registers with `webhook_url: "http://169.254.169.254/latest/meta-data/"` and obtains cloud credentials. Or scans internal ports. The ACB becomes a SSRF proxy by design.

**Fix:**
- Validate that `webhook_url` starts with `https://` in production.
- Resolve DNS and reject private IPs (10.x, 172.16-31.x, 192.168.x, 127.x, 169.254.x, ::1).
- Add configurable domain allowlist.
- Short connect timeout (5s) + total timeout (15s).

**Status:** ✅ Fixed — `internal/dispatcher/validator.go` implements SSRF validation.

---

### S03 — CRITICAL: Redis without authentication by default

**File:** `internal/config/config.go:15`, `main.go:28-31`

`ACB_REDIS_PASS` defaults to empty string. Redis listens on `localhost:6379` without a password. Any local process can `PUBLISH` to `tasks:*`, `agent:*`, `tasks:gates` channels and inject fake events (claim, complete, fail) without API authentication.

**Risk:** An attacker with local access (or remote if Redis binds 0.0.0.0) can:
- Inject a fake `task_completed` event.
- Delete tasks from the pipeline by publishing fraudulent states.
- DoS: publish thousands of events to saturate subscribers.

**Fix:**
- `ACB_REDIS_PASS` should be mandatory in production (fail if empty).
- Bind Redis to `127.0.0.1` explicitly.
- Enable `rename-command` for FLUSHALL/FLUSHDB/CONFIG.

**Status:** 🔴 Open

---

### S04 — HIGH: HMAC webhook signature without replay protection

**File:** `internal/dispatcher/dispatcher.go:123-128`

HMAC-SHA256 is computed only over the body. No timestamp or nonce in the signature:

```go
mac := hmac.New(sha256.New, []byte(agent.WebhookSecret))
mac.Write(body)
sig := hex.EncodeToString(mac.Sum(nil))
```

**Risk:** An attacker who intercepts a legitimate webhook can replay it indefinitely. The receiver has no way to distinguish a new payload from an old one.

**Fix:**
- Sign `timestamp + "." + body`.
- Include timestamp in header: `X-Webhook-Timestamp: <unix>`.
- Receiver rejects if `|current_time - timestamp| > 5 min`.
- Or use event ID as nonce with TTL in Redis.

**Status:** ✅ Fixed — timestamp included in signature and header.

---

### S05 — HIGH: Agent registration allows token overwrite (Upsert)

**File:** `internal/api/agent_handler.go:75-87`, `internal/db/agent_repo.go:14-35`

`POST /agents` calls `UpsertAgent` — any authenticated agent can register with the same name as another agent and **overwrite their token**. The `ON CONFLICT(name) DO UPDATE SET token = excluded.token` replaces the original agent's token.

**Risk:** A malicious agent registers as `orchestrator` with their own token and takes control of all orchestrator's tasks. The original token stops working.

**Fix:**
- Verify that the registering agent is the owner of the name (compare with `X-Agent-Name` from auth middleware).
- Or separate registration (admin-only) from heartbeat (agent can update heartbeat but not token).
- Add `registered_by_admin` flag to agents table.

**Status:** ✅ Fixed — `RegisterAgent` now validates X-Agent-Name matches the authenticated agent.

---

### S06 — MEDIUM: LIKE injection in skill filtering

**File:** `internal/db/task_repo.go:138-139`

```go
query += " AND required_skills LIKE ?"
args = append(args, fmt.Sprintf("%%%s%%", skill))
```

Although it uses prepared statements (no classic SQLi), user input is interpolated into a LIKE pattern without escaping wildcard characters (`%`, `_`). A skill value of `_%` would produce `LIKE '%_%'` returning all tasks.

**Risk:** Information disclosure — an attacker can enumerate all tasks without knowing real skills.

**Fix:**
- Escape LIKE wildcards: `strings.NewReplacer("%", "\\%", "_", "\\_").Replace(skill)`.
- Or better: parse `required_skills` as JSON in Go instead of LIKE (already available in `List()` via `FindMatchingAgents`).

**Status:** 🔴 Open

---

### S07 — MEDIUM: No HTTP security headers

**Files:** `internal/api/router.go`, `internal/api/middleware.go`

No security header middleware:
- No `Strict-Transport-Security` (HSTS)
- No `X-Content-Type-Options: nosniff`
- No `X-Frame-Options: DENY`
- No `Content-Security-Policy`
- No restrictive CORS (API is fully open if exposed to Internet)

**Risk:** Clickjacking, MIME sniffing, content injection in browsers accessing the API.

**Fix:** Add `SecurityHeaders` middleware:
```go
w.Header().Set("X-Content-Type-Options", "nosniff")
w.Header().Set("X-Frame-Options", "DENY")
w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
```

**Status:** 🔴 Open

---

### S08 — LOW: SQL query logged in production

**File:** `internal/db/task_repo.go:125`

```go
log.Printf("[ACB] List query: %s args: %v", query, args)
```

In production, this exposes query structure and values (including `assignee`, `status`) in logs.

**Risk:** Information leakage — an attacker with log access can infer usage patterns and task data.

**Fix:** Move to conditional `log.Printf` with DEBUG level, or remove in production.

**Status:** 🔴 Open (note: agent-1's revert removed this, but it came back with agent-2's WIP)

---

### S09 — LOW: No TLS on HTTP server

**File:** `main.go:52`

```go
log.Fatal(http.ListenAndServe(addr, r))
```

The server runs plain HTTP. Without a TLS reverse proxy, all communications (including Bearer tokens) travel in cleartext.

**Risk:** Token sniffing on unencrypted network.

**Fix:** Add `ListenAndServeTLS` as an option, or document that a TLS reverse proxy (nginx, Caddy, Traefik) MUST be used in production.

**Status:** 🔴 Open (use reverse proxy)

---

## Attack Surface Matrix

| Surface | Findings | Max Severity |
|---------|----------|-------------|
| **Authentication** | S01 (plaintext tokens), S05 (token overwrite) | CRITICAL |
| **SSRF/Webhooks** | S02 (unrestricted SSRF), S04 (HMAC replay) | CRITICAL |
| **Infrastructure** | S03 (Redis without auth), S09 (no TLS) | CRITICAL |
| **Injection/Logic** | S06 (LIKE injection) | MEDIUM |
| **HTTP Hardening** | S07 (no security headers), S08 (SQL in logs) | MEDIUM |

---

## Corrective Actions (priority)

| # | Finding | Action | Effort | Status |
|---|---------|--------|--------|--------|
| 1 | S02 | Validate webhook_url: reject private IPs, force HTTPS, add timeouts | M | ✅ Done |
| 2 | S01 | Hash tokens with Argon2id, compare hash in auth | M | 🔴 Open |
| 3 | S03 | Make ACB_REDIS_PASS mandatory, bind 127.0.0.1 | S | 🔴 Open |
| 4 | S05 | Protect UpsertAgent against token overwrite | S | ✅ Done |
| 5 | S04 | Add timestamp to HMAC, reject replays >5min | S | ✅ Done |
| 6 | S06 | Escape LIKE wildcards or refactor to JSON filter | S | 🔴 Open |
| 7 | S07 | Add security headers middleware | S | 🔴 Open |
| 8 | S08 | Guard log with debug conditional | XS | 🔴 Open |
| 9 | S09 | Add TLS support or document reverse proxy requirement | S | 🔴 Open |

---

*End of report. Agent-3 Security Audit — 72h uptime, powered by Cheetos and paranoia.*