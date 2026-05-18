#!/usr/bin/env python3
"""
ACB Status Monitor for Amanda
Checks ACB for active tasks and notifies only if there are actionable items.
Runs as a Hermes cronjob (no_agent=true) — outputs summary or stays silent.
"""

import json
import os
import urllib.request
import urllib.error

ACB_URL = os.environ.get("ACB_URL", "http://localhost:8090")
STATE_FILE = "/tmp/acb-task-state.txt"


def get_admin_token():
    """Read ACB admin token from .env file."""
    env_path = os.path.expanduser("~/src/acb-go/.env")
    try:
        with open(env_path) as f:
            for line in f:
                line = line.strip()
                if line.startswith("ACB_ADMIN_TOKEN="):
                    return line.split("=", 1)[1]
    except FileNotFoundError:
        pass
    return None


def fetch_tasks(token):
    """Fetch all tasks from ACB."""
    url = f"{ACB_URL}/tasks"
    req = urllib.request.Request(url, headers={
        "Authorization": f"Bearer {token}",
        "X-Agent-Name": "admin",
    })
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            return json.loads(resp.read().decode())
    except urllib.error.HTTPError as e:
        return {"error": f"HTTP {e.code}: {e.read().decode()}"}
    except Exception as e:
        return {"error": str(e)}


def main():
    token = get_admin_token()
    if not token:
        # Silent — no token, no check
        return

    tasks = fetch_tasks(token)
    if isinstance(tasks, dict) and "error" in tasks:
        print(f"⚠️ ACB error: {tasks['error']}")
        return

    # Filter active tasks (not completed/failed)
    active = [t for t in tasks if t.get("status") in ("pending", "claimed", "in_progress", "blocked")]
    if not active:
        # Check state file for changes
        if os.path.exists(STATE_FILE):
            try:
                os.remove(STATE_FILE)
            except OSError:
                pass
        # Silent — no active tasks
        return

    # Build current state signature
    current_lines = []
    for t in sorted(active, key=lambda x: x["id"]):
        current_lines.append(f"{t['status']}|{t.get('assignee', 'none')}|{t['title']}|{t['id'][:8]}")
    current_state = "\n".join(current_lines)

    # Compare with previous state
    previous_state = ""
    if os.path.exists(STATE_FILE):
        try:
            with open(STATE_FILE) as f:
                previous_state = f.read()
        except OSError:
            pass

    if current_state == previous_state:
        # No changes — silent
        return

    # Something changed — build notification
    print(f"📋 {len(active)} tarea(s) activa(s) en ACB:\n")
    status_emoji = {
        "pending": "⏳", "claimed": "📋",
        "in_progress": "🔧", "blocked": "🚧",
    }

    # Identify changes
    prev_set = set(previous_state.split("\n")) if previous_state else set()
    curr_set = set(current_lines)

    for line in current_lines:
        parts = line.split("|")
        status, assignee, title, tid = parts[0], parts[1], parts[2], parts[3]
        emoji = status_emoji.get(status, "❓")

        if line not in prev_set:
            # New or changed task
            if any(tid in p for p in prev_set):
                print(f"🔄 {emoji} {title} → {status} ({assignee})")
            else:
                print(f"🆕 {emoji} {title} [{tid}] — {status} ({assignee})")
        else:
            print(f"   {emoji} {title} — {status} ({assignee})")

    # Save state
    try:
        with open(STATE_FILE, "w") as f:
            f.write(current_state)
    except OSError:
        pass


if __name__ == "__main__":
    main()