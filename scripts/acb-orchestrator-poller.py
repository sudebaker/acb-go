#!/usr/bin/env python3
"""
ACB Orchestrator Poller — monitors all tasks and auto-handles gates.

Usage: ACB_ORCHESTRATOR_TOKEN=<token> acb-orchestrator-poller.py

Checks for task state changes across all tasks and optionally auto-approves
gates. Silent when nothing changes.

Environment:
  ACB_URL                    - ACB base URL (default: http://localhost:8090)
  ACB_ORCHESTRATOR_TOKEN     - Orchestrator Bearer token (required)
  ACB_STATE_FILE             - State file path (default: /tmp/acb-orch-state.json)
  ACB_AUTO_APPROVE_GATES     - Auto-approve + unblock gates (default: true)
"""

import json
import os
import sys
import urllib.error
import urllib.request

ACB_URL = os.environ.get("ACB_URL", "http://localhost:8090")
STATE_FILE = os.environ.get("ACB_STATE_FILE", "/tmp/acb-orch-state.json")
AUTO_APPROVE = os.environ.get("ACB_AUTO_APPROVE_GATES", "true").lower() in ("true", "1", "yes")


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


def build_task_state(tasks):
    """Build a map of task_id -> {status, assignee, title, gate_id, gate_status}."""
    state = {}
    for t in tasks:
        if not isinstance(t, dict):
            continue
        tid = t.get("id", "")
        if not tid:
            continue
        entry = {
            "status": t.get("status", ""),
            "assignee": t.get("assignee") or "",
            "title": t.get("title", ""),
        }
        state[tid] = entry
    return state


def get_gates_for_task(task_id, token):
    """Get gates for a task. Returns list of gate dicts."""
    status, data = acb_request("GET", f"/tasks/{task_id}/gates", token)
    if status == 200 and isinstance(data, list):
        return data
    return []


def detect_changes(current, previous):
    """Compare current and previous state, return list of change messages."""
    notifications = []

    for tid, info in current.items():
        if tid not in previous:
            notifications.append(
                f"[NEW] Task \"{info['title']}\" ({info['status']})"
            )
        else:
            prev = previous[tid]
            if prev["status"] != info["status"]:
                notifications.append(
                    f"[CHANGED] Task \"{info['title']}\": {prev['status']} -> {info['status']}"
                )
                if info["assignee"] and info["assignee"] != prev.get("assignee", ""):
                    notifications.append(
                        f"  assignee -> {info['assignee']}"
                    )
            elif info["assignee"] and prev.get("assignee", "") != info["assignee"]:
                notifications.append(
                    f"[ASSIGNED] Task \"{info['title']}\" -> {info['assignee']}"
                )

    removed = set(previous.keys()) - set(current.keys())
    for tid in removed:
        prev = previous[tid]
        notifications.append(f"[REMOVED] Task \"{prev['title']}\" ({prev['status']})")

    return notifications


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
                lines.append(f"[GATE] Task \"{title}\" — gate {gate_id} (asked), auto-approving...")

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
                        lines.append(f"  -> approved + unblocked")
                    else:
                        lines.append(f"  -> approved but unblock failed ({sc2})")
                else:
                    lines.append(f"  -> approve failed ({sc1})")


def main():
    token = os.environ.get("ACB_ORCHESTRATOR_TOKEN")
    if not token:
        print("ACB_ORCHESTRATOR_TOKEN not set", file=sys.stderr)
        sys.exit(1)

    # Fetch all tasks
    status, tasks = acb_request("GET", "/tasks", token)
    if isinstance(tasks, dict) and "error" in tasks:
        print(f"[ERROR] ACB: {tasks['error']}", file=sys.stderr)
        sys.exit(1)

    if not isinstance(tasks, list):
        print("[ERROR] Unexpected response from /tasks", file=sys.stderr)
        sys.exit(1)

    lines = []

    # Handle blocked tasks (auto-approve gates)
    handle_blocked_tasks(tasks, token, lines)

    # Build current state
    current = build_task_state(tasks)
    previous = load_state()

    # Detect changes
    changes = detect_changes(current, previous)
    lines.extend(changes)

    # Save state
    save_state(current)

    # Output (only if there are changes)
    if lines:
        print("\n".join(lines))


if __name__ == "__main__":
    main()
