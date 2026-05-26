#!/usr/bin/env python3
"""
ACB Agent Poller — merged task checker + state watcher.

Usage: ACB_TOKEN=<token> acb-agent-poller.py <agent_name>

Checks for new dispatchable tasks and monitors state changes.
Silent when nothing changes. Replaces acb-task-checker.py + acb-state-watcher.py.

Environment variables:
  ACB_URL        - ACB base URL (default: http://localhost:8090)
  ACB_TOKEN      - Agent Bearer token (required)
  ACB_STATE_FILE - State file path (default: /tmp/acb-agent-state.json)
"""

import json
import os
import sys
import urllib.error
import urllib.request

ACB_URL = os.environ.get("ACB_URL", "http://localhost:8090")
STATE_FILE = os.environ.get("ACB_STATE_FILE", "/tmp/acb-agent-state.json")


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


def check_dispatch(agent_name, token):
    """Check for new tasks available via dispatch endpoint."""
    status, data = acb_request("GET", f"/tasks/dispatch?agent={agent_name}", token)
    if status == 200 and isinstance(data, dict) and "id" in data:
        title = data.get("title", data.get("id", "unknown"))
        return f"[DISPATCH] New task available: {title}"
    return None


def build_task_state(tasks, agent_name):
    """Build a map of task_id -> {status, assignee, title} for this agent."""
    state = {}
    for t in tasks:
        if not isinstance(t, dict):
            continue
        if t.get("assignee") != agent_name:
            continue
        state[t["id"]] = {
            "status": t.get("status", ""),
            "assignee": t.get("assignee") or "",
            "title": t.get("title", ""),
        }
    return state


def load_state():
    """Load previous state from file."""
    if os.path.exists(STATE_FILE):
        try:
            with open(STATE_FILE) as f:
                return json.load(f)
        except (json.JSONDecodeError, OSError):
            pass
    return {}


def save_state(state):
    """Save current state to file."""
    try:
        os.makedirs(os.path.dirname(STATE_FILE) or ".", exist_ok=True)
        with open(STATE_FILE, "w") as f:
            json.dump(state, f)
    except OSError:
        pass


def detect_changes(current, previous):
    """Compare current and previous state, return list of change messages."""
    notifications = []

    for tid, info in current.items():
        if tid not in previous:
            assignee_str = f" -> {info['assignee']}" if info["assignee"] else " (unassigned)"
            notifications.append(f"[NEW] Task \"{info['title']}\" ({info['status']}){assignee_str}")
        else:
            prev = previous[tid]
            if prev["status"] != info["status"]:
                notifications.append(
                    f"[CHANGED] Task \"{info['title']}\": {prev['status']} -> {info['status']}"
                )
            elif prev["assignee"] != info["assignee"] and info["assignee"]:
                notifications.append(
                    f"[ASSIGNED] Task \"{info['title']}\" -> {info['assignee']}"
                )

    removed = set(previous.keys()) - set(current.keys())
    for tid in removed:
        prev = previous[tid]
        notifications.append(f"[REMOVED] Task \"{prev['title']}\" ({prev['status']})")

    return notifications


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

    dispatch_msg = check_dispatch(agent_name, token)
    if dispatch_msg:
        lines.append(dispatch_msg)

    status, tasks = acb_request("GET", "/tasks", token)
    if isinstance(tasks, dict) and "error" in tasks:
        print(f"[ERROR] ACB: {tasks['error']}", file=sys.stderr)
        sys.exit(1)

    if not isinstance(tasks, list):
        print("[ERROR] Unexpected response from /tasks", file=sys.stderr)
        sys.exit(1)

    current = build_task_state(tasks, agent_name)
    previous = load_state()
    changes = detect_changes(current, previous)
    lines.extend(changes)

    save_state(current)

    if lines:
        print("\n".join(lines))


if __name__ == "__main__":
    main()
