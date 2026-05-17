#!/usr/bin/env python3
"""
ACB Task Checker for Agents
Checks pending/claimed tasks for a specific agent and outputs a summary.
Designed to be run by each agent's heartbeat or cron.

Usage: python3 acb-task-checker.py <agent_name>
  e.g. python3 acb-task-checker.py quique

Silent if no tasks. Outputs actionable info if there are tasks.

Environment variables (optional, override defaults):
  ACB_URL    - ACB base URL (default: http://localhost:8090)
  ACB_TOKEN_<AGENT> - Token for agent, e.g. ACB_TOKEN_QUIQUE
"""

import json
import os
import sys
import urllib.request
import urllib.error

ACB_URL = os.environ.get("ACB_URL", "http://localhost:8090")

# Default tokens — can be overridden via env vars ACB_TOKEN_<NAME>
DEFAULT_TOKENS = {
    "quique": os.environ.get("ACB_TOKEN_QUIQUE", "9IDxRfayMpbAvHRK1DU+nYv4VaMYxEkD0R0xlLfoW/SClYVpPCYvRHxDwbwarm5c"),
    "braulio": os.environ.get("ACB_TOKEN_BRAULIO", "braulio-token"),
    "armando": os.environ.get("ACB_TOKEN_ARMANDO", "armando-token"),
    "amanda": os.environ.get("ACB_TOKEN_AMANDA", "9IDxRfayMpbAvHRK1DU+nYv4VaMYxEkD0R0xlLfoW/SClYVpPCYvRHxDwbwarm5c"),
}


def check_tasks(agent_name):
    token = DEFAULT_TOKENS.get(agent_name) or os.environ.get(f"ACB_TOKEN_{agent_name.upper()}")
    if not token:
        print(f"Agente desconocido: {agent_name}")
        print(f"Agentes disponibles: {', '.join(DEFAULT_TOKENS.keys())}")
        print(f"O define la variable ACB_TOKEN_{agent_name.upper()}")
        return

    url = f"{ACB_URL}/tasks"
    req = urllib.request.Request(url, headers={"Authorization": f"Bearer {token}"})

    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            tasks = json.loads(resp.read().decode())
    except urllib.error.HTTPError as e:
        print(f"Error HTTP consultando ACB: {e.code} {e.reason}")
        return
    except urllib.error.URLError as e:
        print(f"Error conectando a ACB ({ACB_URL}): {e.reason}")
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
    if len(sys.argv) != 2:
        print(f"Uso: {sys.argv[0]} <agent_name>")
        print(f"Agentes disponibles: {', '.join(DEFAULT_TOKENS.keys())}")
        sys.exit(1)
    check_tasks(sys.argv[1])