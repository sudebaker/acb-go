#!/bin/bash
# provision-agent.sh — Configure an agent target for ACB integration.
#
# Generic layer. No platform-specific assumptions (no Docker, no Hermes, no Telegram).
# Delegates cron setup to an external hook script if configured.
#
# Usage:
#   ACB_EXEC_PREFIX="docker exec my-container" \
#   ACB_CP_PREFIX="docker cp" \
#   ACB_CP_DEST="my-container:" \
#   ./provision-agent.sh <agent_name>
#
# Environment variables:
#   ACB_EXEC_PREFIX   - How to run commands on the target (required)
#   ACB_CP_PREFIX     - How to copy files to the target (required)
#   ACB_CP_DEST       - Destination host prefix (default: ${AGENT_NAME}:)
#                        Docker: "container-name:"  SSH: "user@host:"  Local: ""
#   ACB_CRON_HOOK     - Script to run for cron setup (optional)
#   ACB_URL           - ACB base URL for connectivity check (default: http://localhost:8090)
#   ACB_SCRIPTS_DIR   - Directory with scripts to copy (default: dirname of this script)

set -euo pipefail

AGENT_NAME="${1:-}"
ACB_URL="${ACB_URL:-http://localhost:8090}"
ACB_CP_DEST="${ACB_CP_DEST:-${AGENT_NAME}:}"
SCRIPT_DIR="${ACB_SCRIPTS_DIR:-"$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"}"

if [ -z "$AGENT_NAME" ]; then
    echo "Usage: ACB_EXEC_PREFIX=... ACB_CP_PREFIX=... $0 <agent_name>" >&2
    exit 1
fi

if [ -z "${ACB_EXEC_PREFIX:-}" ]; then
    echo "ACB_EXEC_PREFIX is required" >&2
    exit 1
fi

if [ -z "${ACB_CP_PREFIX:-}" ]; then
    echo "ACB_CP_PREFIX is required" >&2
    exit 1
fi

echo "=== Provisioning agent: ${AGENT_NAME} ==="
echo "Target exec: ${ACB_EXEC_PREFIX}"
echo "Target cp:   ${ACB_CP_PREFIX}"
echo ""

# 1. Check connectivity
echo "Checking target reachability..."
if ! $ACB_EXEC_PREFIX sh -c "echo reachable" > /dev/null 2>&1; then
    echo "Error: cannot reach target with '${ACB_EXEC_PREFIX}'"
    exit 1
fi
echo "OK: target reachable"

# 2. Check ACB connectivity from target
echo "Checking ACB connectivity..."
if ! $ACB_EXEC_PREFIX curl -sf "${ACB_URL}/health" > /dev/null 2>&1; then
    echo "Warning: ACB (${ACB_URL}) not reachable from target"
    echo "  Check network connectivity. Continuing anyway."
else
    echo "OK: ACB reachable"
fi

# 3. Create scripts directory on target
$ACB_EXEC_PREFIX mkdir -p /opt/data/scripts

# 4. Copy poller script
$ACB_CP_PREFIX "${SCRIPT_DIR}/acb-agent-poller.py" "${ACB_CP_DEST}/opt/data/scripts/acb-agent-poller.py"
$ACB_EXEC_PREFIX chmod +x /opt/data/scripts/acb-agent-poller.py
echo "OK: acb-agent-poller.py copied"

# 5. Run cron hook if configured
if [ -n "${ACB_CRON_HOOK:-}" ]; then
    if [ -x "$ACB_CRON_HOOK" ]; then
        echo "Running cron hook: ${ACB_CRON_HOOK}"
        ACB_AGENT_NAME="$AGENT_NAME" \
        ACB_EXEC_PREFIX="$ACB_EXEC_PREFIX" \
        ACB_CP_PREFIX="$ACB_CP_PREFIX" \
        ACB_CP_DEST="$ACB_CP_DEST" \
            "$ACB_CRON_HOOK"
    else
        echo "Warning: ACB_CRON_HOOK '${ACB_CRON_HOOK}' not executable, skipping"
    fi
fi

echo ""
echo "=== Agent ${AGENT_NAME} provisioned ==="
echo "  Script: /opt/data/scripts/acb-agent-poller.py"
echo "  Usage:  ACB_TOKEN=<token> $ACB_EXEC_PREFIX python3 /opt/data/scripts/acb-agent-poller.py ${AGENT_NAME}"
echo ""
echo "To verify:"
echo "  $ACB_EXEC_PREFIX python3 /opt/data/scripts/acb-agent-poller.py ${AGENT_NAME}"
