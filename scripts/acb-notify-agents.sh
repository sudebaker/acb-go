#!/bin/bash
# ACB Task Notifier — checks tasks for all agents and sends notifications via webhook
# Runs as system cron every 15 minutes. Only sends if there are pending/claimed tasks.

ACB_URL="http://localhost:8090"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
STATE_DIR="/tmp/acb-agent-notify"

mkdir -p "$STATE_DIR"

notify_agent() {
    local agent_name="$1"
    local webhook_url="$2"
    local webhook_secret="$3"
    local token="$4"
    
    # Fetch tasks for this agent
    local tasks=$(curl -sf "$ACB_URL/tasks" -H "Authorization: Bearer $token" | \
        python3 -c "
import json,sys
tasks=json.load(sys.stdin)
mine=[t for t in tasks if t.get('assignee')=='$agent_name' and t.get('status') in ('pending','claimed','in_progress','blocked')]
if not mine:
    sys.exit(0)
for t in mine:
    emoji={'claimed':'📋','in_progress':'🔧','blocked':'🚧','pending':'⏳'}.get(t['status'],'❓')
    print(f\"{emoji} [{t['status'].upper()}] {t['title']}\")
    if t.get('body_goal'): print(f\"   Objetivo: {t['body_goal'][:120]}\")
    print(f\"   ID: {t['id']}\")
print()
print('Acciones: start=/tasks/{id}/start  complete=/tasks/{id}/complete')
" 2>/dev/null)
    
    if [ -z "$tasks" ]; then
        return 0
    fi
    
    # Check if we already notified about these exact tasks
    local state_file="$STATE_DIR/${agent_name}.txt"
    local current_hash=$(echo "$tasks" | md5sum | cut -d' ' -f1)
    local prev_hash=$(cat "$state_file" 2>/dev/null || echo "")
    
    if [ "$current_hash" = "$prev_hash" ]; then
        return 0  # Already notified, no change
    fi
    
    echo "$current_hash" > "$state_file"
    
    # Send via webhook
    local payload=$(python3 -c "
import json
msg = '''Tienes tareas pendientes en el ACB:

$tasks

Empieza a trabajar: curl -X POST $ACB_URL/tasks/ID/start -H 'Authorization: Bearer $token'
Cuando termines: curl -X POST $ACB_URL/tasks/ID/complete -H 'Authorization: Bearer $token' -H 'Content-Type: application/json' -d '{\"summary\":\"lo que hiciste\"}'
'''
print(json.dumps({'message': msg}))
")
    
    curl -sf -X POST "$webhook_url" \
        -H "Content-Type: application/json" \
        -H "X-Webhook-Secret: $webhook_secret" \
        -d "$payload" >/dev/null 2>&1
    
    echo "[$agent_name] Notified: tasks changed"
}

notify_agent "quique" "http://localhost:8647/webhook/amanda" "<WEBHOOK_SECRET>" "ACB_AGENT_QUIQUE_TOKEN"
notify_agent "braulio" "http://localhost:8645/webhook/amanda" "<WEBHOOK_SECRET>" "ACB_AGENT_BRAULIO_TOKEN"
notify_agent "armando" "http://localhost:8646/webhook/amanda" "<WEBHOOK_SECRET>" "ACB_AGENT_ARMANDO_TOKEN"