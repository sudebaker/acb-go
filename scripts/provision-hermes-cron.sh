#!/bin/bash
# provision-hermes-cron.sh — Hermes-specific cron hook for ACB agent polling.
#
# Creates/updates a cronjob in Hermes jobs.json to run acb-agent-poller.py.
# Designed to be called from provision-agent.sh via ACB_CRON_HOOK.
#
# Environment variables (inherited from provision-agent.sh):
#   ACB_AGENT_NAME     - Agent name (required)
#   ACB_EXEC_PREFIX    - How to run commands on the target (required)
#   ACB_CP_PREFIX      - How to copy files to the target (required)
#   ACB_CP_DEST        - Destination host prefix (default: ${ACB_AGENT_NAME}:)
#
# Hermes-specific variables:
#   ACB_CRON_EXPR      - Cron expression (default: "*/15 * * * *")
#   ACB_CHAT_ID        - Telegram chat ID (required)
#   ACB_CHAT_NAME      - Telegram chat name (required)
#   ACB_PLATFORM       - Delivery platform (default: "telegram")
#   ACB_PROMPT_TEMPLATE - Custom prompt template.
#                         Default: "Execute: python3 /opt/data/scripts/acb-agent-poller.py <AGENT_NAME>.
#                          If output is [SILENT], respond [SILENT] and nothing else.
#                          If output contains tasks, work on them and report progress."

set -euo pipefail

AGENT_NAME="${ACB_AGENT_NAME:-}"
EXEC_PREFIX="${ACB_EXEC_PREFIX:-}"
CP_PREFIX="${ACB_CP_PREFIX:-}"
CP_DEST="${ACB_CP_DEST:-${ACB_AGENT_NAME}:}"
CRON_EXPR="${ACB_CRON_EXPR:-*/5 * * * *}"
CHAT_ID="${ACB_CHAT_ID:-}"
CHAT_NAME="${ACB_CHAT_NAME:-}"
PLATFORM="${ACB_PLATFORM:-telegram}"
PROMPT_TEMPLATE="${ACB_PROMPT_TEMPLATE:-}"

if [ -z "$AGENT_NAME" ] || [ -z "$EXEC_PREFIX" ] || [ -z "$CP_PREFIX" ]; then
    echo "ACB_AGENT_NAME, ACB_EXEC_PREFIX, ACB_CP_PREFIX are required" >&2
    exit 1
fi

if [ -z "$CHAT_ID" ] || [ -z "$CHAT_NAME" ]; then
    echo "ACB_CHAT_ID and ACB_CHAT_NAME are required" >&2
    exit 1
fi

# Default prompt template
if [ -z "$PROMPT_TEMPLATE" ]; then
    PROMPT_TEMPLATE="Execute: python3 /opt/data/scripts/acb-agent-poller.py ${AGENT_NAME}
If output is [SILENT], respond [SILENT] and nothing else.
If output contains tasks, work on them and report progress."
fi

echo "=== Configuring Hermes cronjob for ${AGENT_NAME} ==="

# Generate and write cronjob JSON via Python (all inputs via env vars for safety)
export AGENT_NAME EXEC_PREFIX CP_PREFIX CP_DEST CRON_EXPR CHAT_ID CHAT_NAME PLATFORM PROMPT_TEMPLATE
python3 -c "
import json, os, subprocess, sys

agent_name = os.environ['AGENT_NAME']
exec_pfx = os.environ['EXEC_PREFIX'].split()
cp_pfx = os.environ['CP_PREFIX'].split()
cp_dest = os.environ['CP_DEST']
cron_expr = os.environ['CRON_EXPR']
chat_id = os.environ['CHAT_ID']
chat_name = os.environ['CHAT_NAME']
platform = os.environ['PLATFORM']
prompt_tpl = os.environ['PROMPT_TEMPLATE']

# Read existing jobs from target
result = subprocess.run(
    exec_pfx + ['cat', '/opt/data/cron/jobs.json'],
    capture_output=True, text=True
)

if result.returncode == 0 and result.stdout.strip():
    data = json.loads(result.stdout)
else:
    data = {'jobs': []}

# Find existing ACB job
existing = [j for j in data['jobs'] if 'acb' in j.get('name', '').lower()]

if existing:
    job = existing[0]
    job['prompt'] = prompt_tpl
    job['enabled'] = True
    job['state'] = 'scheduled'
    print(f'Updated existing cronjob (id={job[\"id\"]})')
else:
    import uuid
    job = {
        'id': uuid.uuid4().hex[:12],
        'name': 'acb-task-check',
        'prompt': prompt_tpl,
        'skills': [],
        'skill': None,
        'model': None,
        'provider': None,
        'base_url': None,
        'script': 'acb-agent-poller.py',
        'context_from': None,
        'schedule': {'kind': 'cron', 'expr': cron_expr, 'display': cron_expr},
        'schedule_display': cron_expr,
        'repeat': {'times': None, 'completed': 0},
        'enabled': True,
        'state': 'scheduled',
        'paused_at': None,
        'paused_reason': None,
        'created_at': None,
        'next_run_at': None,
        'last_run_at': None,
        'last_status': None,
        'last_error': None,
        'deliver': 'origin',
        'origin': {
            'platform': platform,
            'chat_id': chat_id,
            'chat_name': chat_name,
            'thread_id': None,
        },
        'last_delivery_error': None,
        'enabled_toolsets': None,
        'workdir': None,
    }
    data['jobs'].append(job)
    print(f'Created new cronjob (id={job[\"id\"]})')

# Write updated jobs to temp file and copy to target
tmpfile = '/tmp/acb_cron_jobs.json'
with open(tmpfile, 'w') as f:
    json.dump(data, f, indent=2, ensure_ascii=False)

cp_args = cp_pfx + [tmpfile, f'{cp_dest}/opt/data/cron/jobs.json']
subprocess.run(cp_args, check=True)
print('Cronjob written to target')
"

# Restart target to pick up changes
if echo "$EXEC_PREFIX" | grep -q docker; then
    $EXEC_PREFIX sh -c "kill -HUP 1 2>/dev/null || true"
    echo "OK: restart signal sent (PID 1 HUP)"
else
    echo "Restart the target service to apply cron changes"
fi

echo "=== Hermes cronjob configured for ${AGENT_NAME} ==="
echo "  Script: /opt/data/scripts/acb-agent-poller.py"
echo "  Schedule: ${CRON_EXPR}"
echo "  Platform: ${PLATFORM}"
echo "  Chat: ${CHAT_NAME} (${CHAT_ID})"
