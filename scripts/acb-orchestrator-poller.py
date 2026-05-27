#!/usr/bin/env python3
"""
ACB Orchestrator Poller — event-based monitor with cursor persistence.

Usage: ACB_ORCHESTRATOR_TOKEN=<token> acb-orchestrator-poller.py

Monitors all tasks via events API and auto-handles gates.
Silent when nothing changes. Persists cursor via ACB API (no local state files).

Environment:
  ACB_URL                    - ACB base URL (default: http://localhost:8090)
  ACB_ORCHESTRATOR_TOKEN     - Orchestrator Bearer token (required)
  ACB_AUTO_APPROVE_GATES     - Auto-approve + unblock gates (default: true)
"""

import json
import os
import sys
import urllib.error
import urllib.request

ACB_URL = os.environ.get("ACB_URL", "http://localhost:8090")
AUTO_APPROVE = os.environ.get("ACB_AUTO_APPROVE_GATES", "true").lower() in ("true", "1", "yes")

EVENT_LABELS = {
    "CreateTask": "[NEW]",
    "ClaimTask": "[CLAIMED]",
    "StartTask": "[STARTED]",
    "CompleteTask": "[COMPLETED]",
    "FailTask": "[FAILED]",
    "BlockTask": "[BLOCKED]",
    "TaskRetry": "[RETRY]",
    "PendingTimeout": "[TIMEOUT]",
    "StaleAgentRelease": "[RELEASED]",
    "TaskHeartbeatTimeout": "[HEARTBEAT]",
    "ParentsCompleted": "[PARENTS_DONE]",
    "UpdateStatus": "[STATUS]",
}


def acb_request(method, path, token, data=None):
    """Make an authenticated request to the ACB API."""
    url = f"{ACB_URL}{path}"
    headers = {
        "Authorization": f"Bearer {token}",
        "Content-Type": "application/json",
    }
    body = json.dumps(data).encode() if data else None
    req = urllib.request.Request(url, data=body, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            if resp.status == 204:
                return resp.status, None
            return resp.status, json.loads(resp.read().decode())
    except urllib.error.HTTPError as e:
        body = e.read().decode() if e.fp else "{}"
        try:
            payload = json.loads(body)
        except json.JSONDecodeError:
            payload = {"error": body}
        return e.code, payload
    except Exception as e:
        return 0, {"error": str(e)}


def get_cursor(token):
    """Get the last processed event ID from ACB. Returns int or 0."""
    status, data = acb_request("GET", "/agents/me/cursor", token)
    if status == 200 and isinstance(data, dict):
        c = data.get("cursor")
        if c is not None:
            return int(c)
    return 0


def save_cursor(token, cursor):
    """Save the last processed event ID to ACB."""
    status, _ = acb_request(
        "POST", "/agents/me/cursor",
        token, {"cursor": cursor}
    )
    return status in (200, 201)


def format_event(event):
    """Convert a task event dict into a human-readable notification line."""
    event_type = event.get("event", "")
    task_id = event.get("task_id", "")
    agent = event.get("agent", "") or ""
    title = event.get("title", task_id)

    label = EVENT_LABELS.get(event_type, f"[{event_type}]")

    if event_type == "CreateTask":
        return f'{label} Task "{title}" created'
    elif event_type == "ClaimTask":
        by = agent or "unknown"
        return f'{label} Task "{title}" claimed by {by}'
    elif event_type == "StartTask":
        return f'{label} Task "{title}" started'
    elif event_type in ("CompleteTask", "FailTask"):
        return f'{label} Task "{title}"'
    elif event_type == "BlockTask":
        return f'{label} Task "{title}" blocked'
    else:
        return f'{label} Task "{title}": {event_type}'


def get_gates_for_task(task_id, token):
    """Get gates for a task. Returns list of gate dicts."""
    status, data = acb_request("GET", f"/tasks/{task_id}/gates", token)
    if status == 200 and isinstance(data, list):
        return data
    return []


def handle_blocked_tasks(tasks, token, lines):
    """Auto-approve and unblock tasks with gates in 'asked' status."""
    if not AUTO_APPROVE:
        return

    for t in tasks:
        if not isinstance(t, dict):
            continue
        if t.get("status") != "blocked":
            continue
        tid = t["id"]
        title = t.get("title", tid)

        gates = get_gates_for_task(tid, token)
        for gate in gates:
            if gate.get("status") == "asked":
                gate_id = gate["gate_id"]
                lines.append(f'[GATE] Task "{title}" — gate {gate_id} (asked), auto-approving...')

                sc1, _ = acb_request(
                    "POST", f"/tasks/{tid}/gates/{gate_id}/approve",
                    token, {"answer": "Approved by orchestrator"}
                )
                if sc1 in (200, 201):
                    sc2, _ = acb_request(
                        "POST", f"/tasks/{tid}/unblock",
                        token, {"gate_id": gate_id}
                    )
                    if sc2 in (200, 201):
                        lines.append("  -> approved + unblocked")
                    else:
                        lines.append(f"  -> approved but unblock failed ({sc2})")
                else:
                    lines.append(f"  -> approve failed ({sc1})")


def main():
    token = os.environ.get("ACB_ORCHESTRATOR_TOKEN")
    if not token:
        print("ACB_ORCHESTRATOR_TOKEN not set", file=sys.stderr)
        sys.exit(1)

    lines = []

    # 1. Auto-approve gates (requires /tasks snapshot)
    status, tasks = acb_request("GET", "/tasks", token)
    if isinstance(tasks, dict) and "error" in tasks:
        print(f"[ERROR] ACB: {tasks['error']}", file=sys.stderr)
        sys.exit(1)

    if not isinstance(tasks, list):
        print("[ERROR] Unexpected response from /tasks", file=sys.stderr)
        sys.exit(1)

    handle_blocked_tasks(tasks, token, lines)

    # 2. Get cursor (last processed event ID)
    after_id = get_cursor(token)

    # 3. Fetch global events since after_id (no agent filter)
    path = f"/events?after_id={after_id}"
    status, events = acb_request("GET", path, token)
    if status != 200:
        print(f"[ERROR] Failed to fetch events: {events}", file=sys.stderr)
        sys.exit(1)

    if not isinstance(events, list):
        print("[ERROR] Unexpected response from /events", file=sys.stderr)
        sys.exit(1)

    # 4. Format events into notification lines
    for event in events:
        msg = format_event(event)
        if msg:
            lines.append(msg)

    # 5. Update cursor with the highest event ID seen (API returns DESC)
    if events:
        max_id = max(e.get("id", 0) for e in events)
        if max_id > 0:
            save_cursor(token, max_id)

    # 6. Output only if there are any notifications
    if lines:
        print("\n".join(lines))


if __name__ == "__main__":
    main()
