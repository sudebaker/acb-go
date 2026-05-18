#!/usr/bin/env python3
"""
ACB Task Poller for Agents
Checks for pending/unclaimed tasks matching each agent's skills,
then claims them and sends a notification via the agent's webhook.

Usage: python3 acb-poll-and-notify.py [--agent quique|braulio|armando|all]
Runs independently — no LLM tokens needed.
"""

import json
import sys
import urllib.request
import urllib.error

ACB_URL = "http://localhost:8090"
AGENTS = {
    "quique": {
        "token": "ACB_AGENT_QUIQUE_TOKEN",
        "skills": ["coding", "security", "go", "testing", "devops", "python"],
        "webhook": "http://localhost:8647/webhook/amanda",
        "webhook_secret": "<WEBHOOK_SECRET>",
    },
    "braulio": {
        "token": "ACB_AGENT_BRAULIO_TOKEN",
        "skills": ["sysadmin", "coding", "docker", "linux", "review", "security", "infra", "go"],
        "webhook": "http://localhost:8645/webhook/amanda",
        "webhook_secret": "<WEBHOOK_SECRET>",
    },
    "armando": {
        "token": "ACB_AGENT_ARMANDO_TOKEN",
        "skills": ["osint", "hacking", "security", "research", "celery"],
        "webhook": "http://localhost:8646/webhook/amanda",
        "webhook_secret": "<WEBHOOK_SECRET>",
    },
}


def api(method, path, token, data=None):
    """Make an API request to ACB."""
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
        return {"error": e.code, "message": e.read().decode()}


def skills_match(task_skills, agent_skills):
    """Check if agent has ALL required skills for a task."""
    if not task_skills:
        return True  # No skills required = open to all
    return all(s.lower() in [a.lower() for a in agent_skills] for s in task_skills)


def poll_agent(agent_name):
    """Poll for tasks and claim matching ones. Returns list of claimed tasks."""
    agent = AGENTS[agent_name]
    claimed = []

    # Get all tasks
    result = api("GET", "/tasks", agent["token"])
    if "error" in result:
        print(f"[{agent_name}] Error fetching tasks: {result}")
        return claimed

    # Find pending/unclaimed tasks matching agent's skills
    for task in result:
        if task.get("status") not in ("pending",):
            continue
        if task.get("assignee"):
            continue  # Already claimed

        task_skills = task.get("skills", []) or task.get("required_skills", [])
        if not skills_match(task_skills, agent["skills"]):
            continue

        # Claim the task
        claim = api("POST", f"/tasks/{task['id']}/claim", agent["token"],
                     {"assignee": agent_name})
        if "error" in claim:
            print(f"[{agent_name}] Failed to claim '{task['title']}': {claim}")
            continue

        claimed.append(claim)
        print(f"[{agent_name}] Claimed: {task['title']}")

        # Try to notify via webhook
        try:
            notify_body = json.dumps({
                "task_id": task["id"],
                "title": task["title"],
                "status": "claimed",
                "assignee": agent_name,
            }).encode()
            headers = {
                "Content-Type": "application/json",
                "X-Webhook-Secret": agent["webhook_secret"],
            }
            req = urllib.request.Request(
                agent["webhook"], data=notify_body, headers=headers, method="POST"
            )
            urllib.request.urlopen(req, timeout=5)
        except Exception as e:
            print(f"[{agent_name}] Webhook notification failed: {e}")

    return claimed


if __name__ == "__main__":
    targets = []
    if len(sys.argv) > 1 and sys.argv[1] == "--agent":
        if len(sys.argv) < 3:
            print("Usage: acb-poll-and-notify.py --agent quique|braulio|armando|all")
            sys.exit(1)
        name = sys.argv[2]
        if name == "all":
            targets = list(AGENTS.keys())
        elif name in AGENTS:
            targets = [name]
        else:
            print(f"Unknown agent: {name}. Available: {', '.join(AGENTS.keys())}")
            sys.exit(1)
    else:
        targets = list(AGENTS.keys())

    all_claimed = []
    for agent in targets:
        claimed = poll_agent(agent)
        all_claimed.extend(claimed)

    if all_claimed:
        # Output summary for cron notification
        print(f"\n📋 {len(all_claimed)} tarea(s) reclamada(s)")
        for t in all_claimed:
            print(f"  → {t.get('assignee', '?')}: {t.get('title', '?')}")
    else:
        # Silent — no output means no notification via cron
        pass