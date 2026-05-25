#!/usr/bin/env python3
"""
ACB Task Checker for Agents

Checks pending/claimed tasks for a specific agent, outputs a summary,
and provides clear instructions for task lifecycle management.

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

# Token mapping for direct execution (not from env)
AGENT_TOKENS = {
    "quique": "quique-acb-token-2026-secure-004",
    "braulio": "braulio-acb-token-2026-secure-002",
    "armando": "armando-acb-token-2026-secure-003",
}


def get_agent_token(agent_name):
    """Get agent token from env var, mapping, or .env file."""
    env_key = f"ACB_TOKEN_{agent_name.upper()}"
    token = os.environ.get(env_key)
    if token:
        return token
    # Fallback to mapping
    return AGENT_TOKENS.get(agent_name)


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
            return json.loads(resp.read().decode())
    except urllib.error.HTTPError as e:
        return {"error": f"HTTP {e.code}: {e.reason}"}
    except Exception as e:
        return {"error": str(e)}


def check_tasks(agent_name):
    """Check and display tasks for an agent."""
    token = get_agent_token(agent_name)
    if not token:
        return  # Silent — no token

    result = acb_request("GET", "/tasks", token)
    if isinstance(result, dict) and "error" in result:
        print(f"⚠️ ACB error: {result['error']}")
        return

    # Filter for this agent's active tasks
    active_statuses = ("pending", "claimed", "in_progress", "blocked")
    my_tasks = [
        t for t in result
        if t.get("assignee") == agent_name and t.get("status") in active_statuses
    ]

    if not my_tasks:
        return  # Silent — no active tasks

    print(f"Tienes {len(my_tasks)} tarea(s) activa(s):\n")

    for t in my_tasks:
        status_info = {
            "pending": ("⏳", "Pendiente — haz claim si quieres trabajar en ella"),
            "claimed": ("📋", "Reclamada —Empieza cuando estés listo"),
            "in_progress": ("🔧", "En progreso — marca completada cuando termines"),
            "blocked": ("🚧", "Bloqueada — necesita intervención humana"),
        }
        emoji, desc = status_info.get(t["status"], ("❓", t["status"]))
        print(f"{emoji} [{t['status'].upper()}] {t['title']}")
        print(f"   {desc}")
        if t.get("body_goal"):
            goal = t["body_goal"]
            if len(goal) > 150:
                goal = goal[:150] + "..."
            print(f"   Objetivo: {goal}")
        print(f"   ID: {t['id']}")

    print(f"\nAcciones (usa tu token: {token}):")
    print(f"  Claim:   POST /tasks/{{id}}/claim")
    print(f"  Start:   POST /tasks/{{id}}/start")
    print(f"  Complete: POST /tasks/{{id}}/complete  {{\"summary\": \"qué hiciste\"}}")
    print(f"  Fail:     POST /tasks/{{id}}/fail  {{\"reason\": \"por qué falló\"}}")
    print(f"  Block:    POST /tasks/{{id}}/block  {{\"gate_id\": \"g_001\", \"question\": \"¿qué necesitas?\"}}")


def main():
    if len(sys.argv) < 2:
        print(f"Usage: {sys.argv[0]} <agent_name>")
        print("Define ACB_TOKEN_<AGENT> en el entorno del agente")
        sys.exit(1)
    check_tasks(sys.argv[1])


if __name__ == "__main__":
    main()