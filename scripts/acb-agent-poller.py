#!/usr/bin/env python3
"""
ACB Agent Poller — event-based task monitor with cursor persistence.

Usage: ACB_TOKEN=<token> acb-agent-poller.py <agent_name>

Checks for dispatchable tasks and reports new events since last cursor.
Silent when nothing changes. Persists cursor via ACB API (no local state files).

Environment:
  ACB_URL   - ACB base URL (default: http://localhost:8090)
  ACB_TOKEN - Agent Bearer token (required)
"""

import json
import os
import sys
import urllib.error
import urllib.request

ACB_URL = os.environ.get("ACB_URL", "http://localhost:8090")

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


def check_dispatch(agent_name, token):
    """Check for new tasks available via dispatch endpoint."""
    status, data = acb_request("GET", f"/tasks/dispatch?agent={agent_name}", token)
    if status == 200 and isinstance(data, dict) and "id" in data:
        title = data.get("title", data.get("id", "unknown"))
        return f"[DISPATCH] New task available: {title}"
    return None


def main():
    if len(sys.argv) < 2 or sys.argv[1] in ("-h", "--help"):
        print(f"Usage: ACB_TOKEN=<token> {sys.argv[0]} <agent_name>", file=sys.stderr)
        sys.exit(1)

    agent_name = sys.argv[1]
    token = os.environ.get("ACB_TOKEN")
    if not token:
        print("ACB_TOKEN not set", file=sys.stderr)
        sys.exit(1)

    lines = []

    # 1. Check for dispatchable tasks
    dispatch_msg = check_dispatch(agent_name, token)
    if dispatch_msg:
        lines.append(dispatch_msg)

    # 2. Get cursor (last processed event ID)
    after_id = get_cursor(token)

    # 3. Fetch new events since after_id
    path = f"/events?after_id={after_id}&agent={agent_name}"
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
