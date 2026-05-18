#!/usr/bin/env python3
"""
ACB State Watcher — Amanda's clean notification script.
Only notifies when there are meaningful state changes.
Runs as a Hermes cronjob (no_agent=true). Silent when no changes.

Changes that trigger notification:
- New task created
- Task completed
- Task failed
- Task blocked (needs human)
- Task assigned to an agent
"""

import json
import os
import urllib.request
import urllib.error
from datetime import datetime

ACB_URL = os.environ.get("ACB_URL", "http://localhost:8090")
STATE_FILE = "/tmp/acb-watcher-state.json"

AGENTS = {
    "quique": "quique-acb-token-2026-secure-004",
    "braulio": "braulio-acb-token-2026-secure-002",
    "armando": "armando-acb-token-2026-secure-003",
}


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
    })
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            return json.loads(resp.read().decode())
    except Exception as e:
        return {"error": str(e)}


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
        with open(STATE_FILE, "w") as f:
            json.dump(state, f)
    except OSError:
        pass


def format_task(task):
    """Format a single task for display."""
    status_emoji = {
        "pending": "⏳",
        "claimed": "📋",
        "in_progress": "🔧",
        "blocked": "🚧",
        "completed": "✅",
        "failed": "❌",
    }
    emoji = status_emoji.get(task.get("status", ""), "❓")
    assignee = task.get("assignee") or "sin asignar"
    title = task.get("title", "Sin título")
    return f"{emoji} {title} → {task.get('status')} ({assignee})"


def main():
    token = get_admin_token()
    if not token:
        return  # Silent — no token

    tasks = fetch_tasks(token)
    if isinstance(tasks, dict) and "error" in tasks:
        print(f"⚠️ ACB error: {tasks['error']}")
        return

    # Build current state: task_id -> {status, assignee, title}
    current = {}
    for t in tasks:
        current[t["id"]] = {
            "status": t.get("status", ""),
            "assignee": t.get("assignee") or "",
            "title": t.get("title", ""),
        }

    previous = load_state()

    # Detect changes
    notifications = []
    for tid, info in current.items():
        if tid not in previous:
            # New task
            emoji = {"pending": "🆕", "completed": "✅", "failed": "❌", "blocked": "🚧"}.get(info["status"], "📋")
            assignee_str = f" → {info['assignee']}" if info["assignee"] else " (sin asignar)"
            notifications.append(f"{emoji} Nueva: {info['title']}{assignee_str}")
        else:
            prev = previous[tid]
            if prev["status"] != info["status"]:
                # Status change
                task_emoji = {"completed": "✅", "failed": "❌", "blocked": "🚧", "in_progress": "🔧"}.get(info["status"], "🔄")
                notifications.append(f"{task_emoji} {info['title']}: {prev['status']} → {info['status']}")
            elif prev["assignee"] != info["assignee"] and info["assignee"]:
                # Assignment change
                notifications.append(f"📋 {info['title']}: asignado a {info['assignee']}")

    # Check for tasks that disappeared (completed and removed — unlikely but handle)
    for tid in set(previous.keys()) - set(current.keys()):
        prev = previous[tid]
        notifications.append(f"🗑️ Eliminada: {prev['title']}")

    save_state(current)

    if notifications:
        print(f"📋 ACB ({len(current)} tareas activas):\n" + "\n".join(f"  {n}" for n in notifications))
    # Silent if no changes — cron won't notify


if __name__ == "__main__":
    main()