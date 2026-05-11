#!/bin/sh
# End-to-end test script for ACB service.
# Usage: ./scripts/test.sh [host:port]
# Requires: curl, docker

BASE="${1:-http://localhost:8080}"
PASS=0
FAIL=0
AGENT_NAME="test-agent"
AGENT_TOKEN="test-token-123"

log_pass() { echo "  ✓ PASS: $1" >&2; PASS=$((PASS + 1)); }
log_fail() { echo "  ✗ FAIL: $1" >&2; FAIL=$((FAIL + 1)); }

# Single curl call: captures status and body without double-mutation
curl_json() {
	local tmpfile
	tmpfile=$(mktemp /tmp/acb-test-body.XXXXXX)
	local status
	status=$(curl -s -w "%{http_code}" -o "$tmpfile" "$@")
	local body
	body=$(cat "$tmpfile")
	rm -f "$tmpfile"
	echo "$body"
	return "$status"
}

assert_status() {
	local label="$1" expected="$2"
	shift 2
	local tmpfile
	tmpfile=$(mktemp /tmp/acb-test-body.XXXXXX)
	local actual
	actual=$(curl -s -w "%{http_code}" -o "$tmpfile" "$@")
	cat "$tmpfile" > /dev/null  # discard body for status-only checks
	rm -f "$tmpfile"
	if [ "$actual" = "$expected" ]; then
		log_pass "$label"
		return 0
	else
		log_fail "$label (expected $expected, got $actual)"
		return 1
	fi
}

assert_status_body() {
	local label="$1" expected="$2"
	shift 2
	local tmpfile
	tmpfile=$(mktemp /tmp/acb-test-body.XXXXXX)
	local actual
	actual=$(curl -s -w "%{http_code}" -o "$tmpfile" "$@")
	local body
	body=$(cat "$tmpfile")
	rm -f "$tmpfile"
	if [ "$actual" = "$expected" ]; then
		log_pass "$label"
	else
		log_fail "$label (expected $expected, got $actual)"
	fi
	echo "$body"
}

assert_contains() {
	local label="$1" needle="$2" haystack="$3"
	case "$haystack" in
		*"$needle"*) log_pass "$label" ;;
		*) log_fail "$label (expected to contain '$needle')" ;;
	esac
}

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  ACB Integration Test Suite"
echo "  Target: $BASE"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

# ── Setup ──────────────────────────────────────────────────────────────
echo "▶ Setup: registering test agent"
docker exec acb-service sqlite3 /var/lib/acb/acb.db \
	"DELETE FROM gates; DELETE FROM tasks WHERE id LIKE 't%'; DELETE FROM agents WHERE name = '$AGENT_NAME';" \
	2>/dev/null || true
docker exec acb-service sqlite3 /var/lib/acb/acb.db \
	"INSERT INTO agents (name, port, token) VALUES ('$AGENT_NAME', 8080, '$AGENT_TOKEN');" 2>/dev/null \
	&& log_pass "agent registered" \
	|| (log_fail "failed to register agent"; exit 1)

AUTH() { echo "-H Authorization: Bearer $AGENT_TOKEN"; }

# ── 1. Health ───────────────────────────────────────────────────────────
echo ""
echo "▶ 1. Health endpoint"

assert_status "GET /health → 200" "200" "$BASE/health"

BODY=$(curl -s "$BASE/health")
assert_contains "response body has status ok" '"status":"ok"' "$BODY"

# ── 2. Auth failures ────────────────────────────────────────────────────
echo ""
echo "▶ 2. Auth failures"

assert_status "POST /tasks without token → 401" "401" -X POST "$BASE/tasks" \
	-H "Content-Type: application/json" -d '{"title":"x"}'

assert_status "POST /tasks with invalid token → 401" "401" -X POST "$BASE/tasks" \
	-H "Authorization: Bearer invalid-token" \
	-H "Content-Type: application/json" -d '{"title":"x"}'

assert_status "GET /tasks/nonexistent with valid token → 404" "404" \
	-H "Authorization: Bearer $AGENT_TOKEN" "$BASE/tasks/nonexistent"

# ── 3. Create task ──────────────────────────────────────────────────────
echo ""
echo "▶ 3. Create task"

BODY=$(assert_status_body "POST /tasks → 201" "201" -X POST "$BASE/tasks" \
	-H "Authorization: Bearer $AGENT_TOKEN" \
	-H "Content-Type: application/json" \
	-d '{"id":"t001","title":"Integration test task","priority":3}')
assert_contains "response has pending status" '"status":"pending"' "$BODY"

# ── 4. Validation ───────────────────────────────────────────────────────
echo ""
echo "▶ 4. Validation"

assert_status "POST /tasks without title → 400" "400" -X POST "$BASE/tasks" \
	-H "Authorization: Bearer $AGENT_TOKEN" \
	-H "Content-Type: application/json" -d '{"id":"t002"}'

# ── 5. List tasks ───────────────────────────────────────────────────────
echo ""
echo "▶ 5. List tasks"

BODY=$(assert_status_body "GET /tasks → 200" "200" \
	-H "Authorization: Bearer $AGENT_TOKEN" "$BASE/tasks")
assert_contains "list contains t001" '"id":"t001"' "$BODY"

# ── 6. Get task ─────────────────────────────────────────────────────────
echo ""
echo "▶ 6. Get task by ID"

BODY=$(assert_status_body "GET /tasks/t001 → 200" "200" \
	-H "Authorization: Bearer $AGENT_TOKEN" "$BASE/tasks/t001")
assert_contains "response has title" '"title":"Integration test task"' "$BODY"

# ── 7. Claim task ───────────────────────────────────────────────────────
echo ""
echo "▶ 7. Claim task"

BODY=$(assert_status_body "POST /tasks/t001/claim → 200" "200" -X POST "$BASE/tasks/t001/claim" \
	-H "Authorization: Bearer $AGENT_TOKEN" \
	-H "Content-Type: application/json" -d '{"assignee":"worker-a"}')
assert_contains "response has claimed status" '"status":"claimed"' "$BODY"

# ── 8. Claim already claimed (conflict) ─────────────────────────────────
echo ""
echo "▶ 8. Claim conflict"

BODY=$(assert_status_body "re-claim → 409" "409" -X POST "$BASE/tasks/t001/claim" \
	-H "Authorization: Bearer $AGENT_TOKEN" \
	-H "Content-Type: application/json" -d '{"assignee":"worker-b"}')
assert_contains "409 has current_status" '"current_status"' "$BODY"

# ── 9. Start task ───────────────────────────────────────────────────────
echo ""
echo "▶ 9. Start task"

BODY=$(assert_status_body "POST /tasks/t001/start → 200" "200" -X POST "$BASE/tasks/t001/start" \
	-H "Authorization: Bearer $AGENT_TOKEN")
assert_contains "response has in_progress status" '"status":"in_progress"' "$BODY"

# ── 10. Block task ──────────────────────────────────────────────────────
echo ""
echo "▶ 10. Block task"

BODY=$(assert_status_body "POST /tasks/t001/block → 200" "200" -X POST "$BASE/tasks/t001/block" \
	-H "Authorization: Bearer $AGENT_TOKEN" \
	-H "Content-Type: application/json" -d '{"gate_id":"g001","question":"Should we proceed?"}')
assert_contains "response has blocked status" '"status":"blocked"' "$BODY"

# ── 11. Unblock task ────────────────────────────────────────────────────
echo ""
echo "▶ 11. Unblock task"

docker exec acb-service sqlite3 /var/lib/acb/acb.db \
	"UPDATE gates SET status = 'asked' WHERE gate_id = 'g001';" 2>/dev/null || true
docker exec acb-service sqlite3 /var/lib/acb/acb.db \
	"UPDATE gates SET status = 'answered', answer = 'yes' WHERE gate_id = 'g001';" 2>/dev/null || true

BODY=$(assert_status_body "POST /tasks/t001/unblock → 200" "200" -X POST "$BASE/tasks/t001/unblock" \
	-H "Authorization: Bearer $AGENT_TOKEN" \
	-H "Content-Type: application/json" -d '{"gate_id":"g001"}')
assert_contains "response has in_progress status" '"status":"in_progress"' "$BODY"

# ── 12. Complete task ───────────────────────────────────────────────────
echo ""
echo "▶ 12. Complete task"

BODY=$(assert_status_body "POST /tasks/t001/complete → 200" "200" -X POST "$BASE/tasks/t001/complete" \
	-H "Authorization: Bearer $AGENT_TOKEN" \
	-H "Content-Type: application/json" -d '{"summary":"All tests passed"}')
assert_contains "response has completed status" '"status":"completed"' "$BODY"

# ── 13. Artifact upload ─────────────────────────────────────────────────
echo ""
echo "▶ 13. Artifact upload"

echo "hello from acb" > /tmp/acb-test-artifact.txt
BODY=$(assert_status_body "POST /tasks/t001/artifacts → 201" "201" -X POST "$BASE/tasks/t001/artifacts" \
	-H "Authorization: Bearer $AGENT_TOKEN" \
	-F "file=@/tmp/acb-test-artifact.txt")
assert_contains "upload response has key" '"key"' "$BODY"
ARTIFACT_KEY=$(echo "$BODY" | python3 -c "import json,sys; print(json.load(sys.stdin)['key'])" 2>/dev/null || echo "")
echo "  → artifact key: $ARTIFACT_KEY"

# ── 14. List artifacts ──────────────────────────────────────────────────
echo ""
echo "▶ 14. List artifacts"

BODY=$(assert_status_body "GET /tasks/t001/artifacts → 200" "200" \
	-H "Authorization: Bearer $AGENT_TOKEN" "$BASE/tasks/t001/artifacts")
assert_contains "list has key field" '"key"' "$BODY"

# ── 15. Download artifact ───────────────────────────────────────────────
echo ""
echo "▶ 15. Download artifact"

if [ -n "$ARTIFACT_KEY" ]; then
	ENCODED_KEY=$(python3 -c "import sys,urllib.parse; print(urllib.parse.quote(sys.stdin.read().strip()))" <<< "$ARTIFACT_KEY" 2>/dev/null || echo "$ARTIFACT_KEY")
	CONTENT=$(curl -s "$BASE/tasks/t001/artifacts?key=$ENCODED_KEY" \
		-H "Authorization: Bearer $AGENT_TOKEN")
	case "$CONTENT" in
		*"hello from acb"*) log_pass "downloaded artifact content matches" ;;
		*) log_fail "downloaded artifact content mismatch (got: '$CONTENT')" ;;
	esac
fi

# ── 16. Delete artifact ─────────────────────────────────────────────────
echo ""
echo "▶ 16. Delete artifact"

if [ -n "$ARTIFACT_KEY" ]; then
	ENCODED_KEY=$(python3 -c "import sys,urllib.parse; print(urllib.parse.quote(sys.stdin.read().strip()))" <<< "$ARTIFACT_KEY" 2>/dev/null || echo "$ARTIFACT_KEY")
	assert_status "DELETE /tasks/t001/artifacts → 204" "204" -X DELETE \
		-H "Authorization: Bearer $AGENT_TOKEN" \
		"$BASE/tasks/t001/artifacts?key=$ENCODED_KEY"
fi

# ── 17. Agent heartbeat ─────────────────────────────────────────────────
echo ""
echo "▶ 17. Agent heartbeat"

assert_status "POST /agents/heartbeat → 200" "200" -X POST "$BASE/agents/heartbeat" \
	-H "Authorization: Bearer $AGENT_TOKEN" \
	-H "Content-Type: application/json" -d "{\"name\":\"$AGENT_NAME\"}"

# ── 18. Get agent ───────────────────────────────────────────────────────
echo ""
echo "▶ 18. Get agent"

BODY=$(assert_status_body "GET /agents/$AGENT_NAME → 200" "200" \
	-H "Authorization: Bearer $AGENT_TOKEN" "$BASE/agents/$AGENT_NAME")
assert_contains "response has agent name" '"name":"test-agent"' "$BODY"

# ── 19. Fail task lifecycle ─────────────────────────────────────────────
echo ""
echo "▶ 19. Fail task lifecycle"

BODY=$(assert_status_body "POST /tasks t002 → 201" "201" -X POST "$BASE/tasks" \
	-H "Authorization: Bearer $AGENT_TOKEN" \
	-H "Content-Type: application/json" -d '{"id":"t002","title":"Fail test"}')
assert_contains "t002 has pending status" '"status":"pending"' "$BODY"

assert_status "Claim t002 → 200" "200" -X POST "$BASE/tasks/t002/claim" \
	-H "Authorization: Bearer $AGENT_TOKEN" \
	-H "Content-Type: application/json" -d '{"assignee":"worker-a"}'

assert_status "Start t002 → 200" "200" -X POST "$BASE/tasks/t002/start" \
	-H "Authorization: Bearer $AGENT_TOKEN"

BODY=$(assert_status_body "POST /tasks/t002/fail → 200" "200" -X POST "$BASE/tasks/t002/fail" \
	-H "Authorization: Bearer $AGENT_TOKEN" \
	-H "Content-Type: application/json" -d '{"reason":"Something went wrong"}')
assert_contains "t002 has failed status" '"status":"failed"' "$BODY"

# ── Summary ─────────────────────────────────────────────────────────────
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
TOTAL=$((PASS + FAIL))
echo "  Results: $PASS passed, $FAIL failed ($TOTAL total)"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

# Cleanup
rm -f /tmp/acb-test-artifact.txt

if [ "$FAIL" -gt 0 ]; then
	exit 1
fi
