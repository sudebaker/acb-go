#!/usr/bin/env python3
"""
ACB Task Checker for Agents
Checks pending/claimed tasks for a specific agent and outputs a summary.
Designed to be run by each agent's heartbeat or cron.

Usage: python3 acb-task-checker.py <agent_name>
  e.g. python3 acb-task-checker.py quique

Silent if no tasks. Outputs actionable info if there are tasks.

Environment variables (override defaults):
  ACB_URL    - ACB base URL (default: http://localhost:8090)
  ACB_TOKEN_<AGENT> - Token for agent, e.g. ACB_TOKEN_QUIQUE
"""

import json
import os
import sys
import urllib.request
import urllib.error

ACB_URL = os.environ.get("ACB_URL", "http://localhost:8090")


def get_agent_token(agent_name):
    """Get agent token from environment variable ACB_TOKEN_<NAME>."""
    env_key = f"ACB_TOKEN_{agent_name.upper()}"
    token = os.environ.get(env_key)
    if token:
        return token

    # Fallback: read from the ACB .env file using admin token
    # (only for Amanda's monitoring use)
    return None


def check_tasks(agent_name, token=None):
    if not token:
        token = get_agent_token(agent_name)
    if not token:
        # Silent — no token configured
        return

    url = f"{ACB_URL}/tasks"
    req = urllib.request.Request(url, headers={
        "Authorization": f"Bearer {token}",
        "X-Agent-Name": agent_name,
    })

    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            tasks = json.loads(resp.read().decode())
    except urllib.error.HTTPError as e:
        if e.code == 401:
            print(f"⚠️ ACB auth error for {agent_name}: token invalid or expired")
        else:
            print(f"Error HTTP consultando ACB: {e.code} {e.reason}")
        return
    except Exception as e:
        print(f"Error consultando ACB: {e}")
        return

    # Filter for tasks assigned to this agent that are still active
    active_statuses = ("pending", "claimed", "in_progress", "blocked")
    my_tasks = [
        t for t in tasks
        if t.get("assignee") == agent_name and t.get("status") in active_statuses
    ]

    if not my_tasks:
        return  # Silent — no tasks

    print(f"Tienes {len(my_tasks)} tarea(s) en el ACB:\n")
    for t in my_tasks:
        status_emoji = {"claimed": "📋", "in_progress": "🔧", "blocked": "🚧", "pending": "⏳"}.get(t["status"], "❓")
        print(f'{status_emoji} [{t["status"].upper()}] {t["title"]}')
        if t.get("body_goal"):
            print(f'   Objetivo: {t["body_goal"]}')
        if t.get("body_context"):
            print(f'   Contexto: {t["body_context"]}')
        print(f'   ID: {t["id"]}')
        print()

    print("Acciones:")
    print("- Si es CLAIMED y vas a empezar: POST /tasks/{id}/start")
    print("- Si es IN_PROGRESS y terminaste: POST /tasks/{id}/complete")
    print("- Si está BLOCKED: POST /tasks/{id}/unblock")


if __name__ == "__main__":
    if len(sys.argv) < 2:
        print(f"Uso: {sys.argv[0]} <agent_name>")
        print("Define ACB_TOKEN_<AGENT> en el entorno del agente")
        sys.exit(1)
    check_tasks(sys.argv[1])