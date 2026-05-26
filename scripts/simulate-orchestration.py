#!/usr/bin/env python3
"""
ACB End-to-End Orchestration Simulation.

Simulates the full orchestration lifecycle:
  1. Admin pre-registers 2 agents on ACB (bootstrap)
  2. Agent threads start with their pre-assigned tokens
  3. Orchestrator creates 5 tasks with varied profiles
  4. Agents poll dispatch, claim, work, complete/fail
  5. Gate flow: agent blocks -> orchestrator answers -> agent unblocks -> completes
  6. Summary report at the end

Usage:
  ACB_ADMIN_TOKEN=<admin_token> python3 simulate-orchestration.py

Environment:
  ACB_URL           - ACB base URL (default: http://localhost:8090)
  ACB_ADMIN_TOKEN   - Admin Bearer token (required)
  ACB_E2E_TIMEOUT   - Max simulation duration in seconds (default: 120)
"""

import json
import os
import random
import sys
import threading
import time
import urllib.error
import urllib.request

ACB_URL = os.environ.get("ACB_URL", "http://localhost:8090")
ADMIN_TOKEN = os.environ.get("ACB_ADMIN_TOKEN", "")
TIMEOUT = int(os.environ.get("ACB_E2E_TIMEOUT", "120"))
POLL_INTERVAL = 3
GATE_TASK_TITLE = "Review authentication PR"

AGENT_1 = {"name": "orchestra-agent-1", "skills": ["go", "coding", "review", "security"]}
AGENT_2 = {"name": "orchestra-agent-2", "skills": ["python", "docker", "linux", "osint", "research", "review", "security"]}

TASKS = [
    {"title": "Implement health endpoint", "required_skills": ["go", "coding"], "priority": 3},
    {"title": "Fix critical security bug", "required_skills": ["review", "security"], "priority": 1},
    {"title": "Deploy monitoring stack", "required_skills": ["docker", "linux"], "priority": 5},
    {"title": GATE_TASK_TITLE, "required_skills": ["review", "go"], "priority": 2, "gate": True},
    {"title": "Research vulnerability", "required_skills": ["osint", "research"], "priority": 4},
]

report_lock = threading.Lock()
report = {
    "agents": {},
    "tasks": {},
    "started_at": None,
    "finished_at": None,
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


def register_agent(name, agent_token, skills, admin_token):
    """Register an agent on ACB using admin auth. Idempotent via upsert."""
    data = {"name": name, "token": agent_token, "skills": skills}
    status, resp = acb_request("POST", "/agents", admin_token, data)
    if status == 200:
        return True
    print(f"  [{name}] register returned {status}: {resp.get('error', '')}")
    return False


def register_agent_with_retry(name, token, skills, admin_token, max_retries=5):
    """Register an agent with retry + backoff for rate limiting."""
    for attempt in range(max_retries):
        ok = register_agent(name, token, skills, admin_token)
        if ok:
            return True
        wait = min(2 ** attempt, 8)
        print(f"  [{name}] retrying in {wait}s...")
        time.sleep(wait)
    return False


def simulate_agent(config, token):
    """Run a simulated agent lifecycle in a thread. Token is pre-assigned by admin."""
    name = config["name"]
    skills = config["skills"]
    fail_keywords = config.get("fail_keywords", [])

    with report_lock:
        report["agents"][name] = {
            "token": token,
            "skills": skills,
            "claimed": 0,
            "completed": 0,
            "failed": 0,
            "blocked": 0,
            "gate_resolved": 0,
        }

    agent_summary = report["agents"][name]
    blocked = {}
    held_tasks = {}
    deadline = time.time() + TIMEOUT

    while time.time() < deadline:
        now_remaining = deadline - time.time()
        if now_remaining < 5:
            break

        # Check held blocked tasks for gate resolution
        for task_id, gate_id in list(blocked.items()):
            status_code, resp = acb_request("POST", f"/tasks/{task_id}/unblock", token, {"gate_id": gate_id})
            if status_code == 200:
                agent_summary["gate_resolved"] += 1
                work_time = random.uniform(3, 6)
                if work_time > now_remaining - 2:
                    work_time = max(1, now_remaining - 2)
                time.sleep(work_time)
                status_code2, _ = acb_request(
                    "POST", f"/tasks/{task_id}/complete", token,
                    {"summary": "Completed after gate resolution"}
                )
                if status_code2 == 200:
                    agent_summary["completed"] += 1
                del blocked[task_id]
                del held_tasks[task_id]
            else:
                pass  # Gate not answered yet, try again next cycle

        # Check held claimed tasks (non-gate) that are still in_progress
        for task_id in list(held_tasks.keys()):
            if task_id in blocked:
                continue
            status_code, task = acb_request("GET", f"/tasks/{task_id}", token)
            if status_code == 200 and isinstance(task, dict):
                if task.get("status") in ("completed", "failed"):
                    del held_tasks[task_id]

        # Poll dispatch for new tasks
        status_code, task = acb_request("GET", f"/tasks/dispatch?agent={name}", token)
        if status_code == 200 and isinstance(task, dict) and task.get("id"):
            task_id = task["id"]
            title = task.get("title", "")
            agent_summary["claimed"] += 1

            # Claim
            acb_request("POST", f"/tasks/{task_id}/claim", token, {"assignee": name})
            held_tasks[task_id] = True

            # Start
            acb_request("POST", f"/tasks/{task_id}/start", token)

            # Check if should fail
            should_fail = any(kw.lower() in title.lower() for kw in fail_keywords)
            if should_fail:
                work_time = random.uniform(2, 5)
                if work_time > deadline - time.time() - 2:
                    work_time = max(1, deadline - time.time() - 2)
                time.sleep(work_time)
                acb_request("POST", f"/tasks/{task_id}/fail", token, {"reason": "Simulated failure"})
                agent_summary["failed"] += 1
                del held_tasks[task_id]
                with report_lock:
                    if task_id in report["tasks"]:
                        report["tasks"][task_id]["actual_outcome"] = "failed"
                continue

            # Check if gate flow
            is_gate = False
            with report_lock:
                if task_id in report["tasks"] and report["tasks"][task_id].get("gate"):
                    is_gate = True

            if is_gate:
                gate_id = f"gate_{task_id[:8]}"
                status_b, _ = acb_request(
                    "POST", f"/tasks/{task_id}/block", token,
                    {"gate_id": gate_id, "question": "Does this meet security requirements?"}
                )
                if status_b == 200:
                    agent_summary["blocked"] += 1
                    blocked[task_id] = gate_id
                    with report_lock:
                        if task_id in report["tasks"]:
                            report["tasks"][task_id]["gate_id"] = gate_id
                    continue
                # If block failed (wrong status, etc), fall through

            # Normal work
            work_time = random.uniform(4, 10)
            if work_time > deadline - time.time() - 2:
                work_time = max(1, deadline - time.time() - 2)
            time.sleep(work_time)

            status_c, _ = acb_request(
                "POST", f"/tasks/{task_id}/complete", token,
                {"summary": f"Implemented {title}"}
            )
            if status_c == 200:
                agent_summary["completed"] += 1
            del held_tasks[task_id]
            with report_lock:
                if task_id in report["tasks"]:
                    report["tasks"][task_id]["actual_outcome"] = "completed"

        time.sleep(POLL_INTERVAL)

    agent_summary["finished"] = True


def run_orchestrator():
    """Pre-register agents, create tasks, monitor simulation."""
    with report_lock:
        report["started_at"] = time.time()

    # Generate stable tokens for both agents
    agent1_token = f"e2e-token-orch-1-{random.randint(10000, 99999)}"
    agent2_token = f"e2e-token-orch-2-{random.randint(10000, 99999)}"

    print("\n=== Phase 1: Admin pre-registers agents ===")
    print(f"  Registering {AGENT_1['name']}...")
    a1_ok = register_agent(AGENT_1["name"], agent1_token, AGENT_1["skills"], ADMIN_TOKEN)
    print(f"  Registering {AGENT_2['name']}...")
    a2_ok = register_agent_with_retry(AGENT_2["name"], agent2_token, AGENT_2["skills"], ADMIN_TOKEN)

    print(f"  {AGENT_1['name']}: {'OK' if a1_ok else 'FAILED'}")
    print(f"  {AGENT_2['name']}: {'OK' if a2_ok else 'FAILED'}")

    if not a1_ok and not a2_ok:
        print("  FATAL: No agents registered. Aborting.")
        return

    # Start agent threads with pre-assigned tokens
    print("\n=== Phase 2: Agent threads start ===")
    thread_1 = threading.Thread(
        target=simulate_agent,
        args=({"name": AGENT_1["name"], "skills": AGENT_1["skills"],
               "fail_keywords": []}, agent1_token),
        daemon=True,
    )
    thread_2 = threading.Thread(
        target=simulate_agent,
        args=({"name": AGENT_2["name"], "skills": AGENT_2["skills"],
               "fail_keywords": ["bug"]}, agent2_token),
        daemon=True,
    )

    thread_1.start()
    thread_2.start()

    print("\n=== Phase 3: Task Creation ===")
    for i, tdef in enumerate(TASKS, 1):
        task_data = {
            "title": tdef["title"],
            "required_skills": tdef["required_skills"],
            "priority": tdef.get("priority", 3),
            "body_goal": f"E2E test task {i}",
        }
        status, resp = acb_request("POST", "/tasks", ADMIN_TOKEN, task_data)
        task_id = resp.get("id", "unknown") if isinstance(resp, dict) else "unknown"
        outcome = "gate" if tdef.get("gate") else "pending"
        with report_lock:
            report["tasks"][task_id] = {
                "title": tdef["title"],
                "expected_skills": tdef["required_skills"],
                "gate": tdef.get("gate", False),
                "status": "created",
                "assignee": None,
                "actual_outcome": outcome,
            }
        status_text = "created" if status in (200, 201) else f"FAILED ({status})"
        marker = " [GATE]" if tdef.get("gate") else ""
        print(f"  {i}. {tdef['title']}: {status_text}{marker}")

    print("\n=== Phase 4: Gate Monitoring (Orchestrator) ===")
    deadline = time.time() + TIMEOUT
    gate_answers_done = set()

    while time.time() < deadline:
        status, tasks_data_resp = acb_request("GET", "/tasks", ADMIN_TOKEN)
        if status == 200 and isinstance(tasks_data_resp, list):
            with report_lock:
                terminal = 0
                total = len(report["tasks"])
                for t in tasks_data_resp:
                    tid = t.get("id", "")
                    if tid in report["tasks"]:
                        report["tasks"][tid]["status"] = t.get("status", "")
                        report["tasks"][tid]["assignee"] = t.get("assignee", "")

                # Check for blocked tasks to answer gates
                for t in tasks_data_resp:
                    tid = t.get("id", "")
                    if tid in gate_answers_done:
                        continue
                    if t.get("status") == "blocked" and tid in report["tasks"]:
                        gate_id = report["tasks"][tid].get("gate_id", "")
                        if gate_id:
                            print(f"  Orchestrator answering gate {gate_id} for task {tid[:8]}...")
                            sc1, _ = acb_request(
                                "POST", f"/tasks/{tid}/gates/{gate_id}/answer",
                                ADMIN_TOKEN, {"answer": "LGTM, approved"}
                            )
                            if sc1 in (200, 201):
                                print(f"  Gate answer submitted (sc={sc1}), approving...")
                                sc2, _ = acb_request(
                                    "POST", f"/tasks/{tid}/gates/{gate_id}/approve",
                                    ADMIN_TOKEN, {"answer": "Approved after review"}
                                )
                                if sc2 in (200, 201):
                                    gate_answers_done.add(tid)
                                    print(f"  Gate {gate_id} approved (sc={sc2})")
                                else:
                                    print(f"  Gate approve returned {sc2}")
                            else:
                                print(f"  Gate answer returned {sc1}")

                # Count terminal tasks
                for tid, info in report["tasks"].items():
                    if info.get("status") in ("completed", "failed"):
                        terminal += 1

                all_terminal = terminal == total

            if all_terminal:
                print("  All tasks reached terminal state.")
                break
        else:
            print(f"  Warning: /tasks returned {status}")

        time.sleep(POLL_INTERVAL)

    with report_lock:
        report["finished_at"] = time.time()
        report["orchestrator_done"] = True

    thread_1.join(timeout=5)
    thread_2.join(timeout=5)

    print_report()


def print_report():
    """Print the final simulation report."""
    with report_lock:
        print("\n" + "=" * 60)
        print("            ORCHESTRATION SIMULATION REPORT")
        print("=" * 60)

        duration = 0
        if report["started_at"] and report["finished_at"]:
            duration = report["finished_at"] - report["started_at"]

        print(f"\nDuration: {duration:.1f}s")

        print("\n--- Agents ---")
        for name, info in report["agents"].items():
            print(f"  {name}")
            print(f"    claimed:   {info['claimed']}")
            print(f"    completed: {info['completed']}")
            print(f"    failed:    {info['failed']}")
            print(f"    blocked:   {info['blocked']}")
            print(f"    resolved:  {info.get('gate_resolved', 0)}")

        print("\n--- Tasks ---")
        passed = 0
        failed = 0
        for tid, info in sorted(report["tasks"].items()):
            title = info["title"]
            assignee = info.get("assignee") or "-"
            status = info.get("status", "unknown")
            outcome = info.get("actual_outcome", "unknown")
            is_gate = " [GATE]" if info.get("gate") else ""
            symbol = "✓" if status in ("completed",) else "✗"
            if status == "completed":
                passed += 1
            else:
                failed += 1
            print(f"  {symbol} | {title:<40} | {assignee:<20} | {status:<12}{is_gate}")

        print(f"\nResults: {passed} passed, {failed} failed")
        print("=" * 60)

        if failed > 0:
            print("\nNOTE: expected failures (Fix critical security bug) are by design.")


def main():
    if not ADMIN_TOKEN:
        print("ACB_ADMIN_TOKEN is required", file=sys.stderr)
        sys.exit(1)

    print(f"ACB URL: {ACB_URL}")
    print(f"Timeout: {TIMEOUT}s")
    print(f"Poll interval: {POLL_INTERVAL}s")

    run_orchestrator()


if __name__ == "__main__":
    main()
