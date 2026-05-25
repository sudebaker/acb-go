#!/bin/bash
# provision-agent.sh — Configure an agent container for ACB integration
#
# Usage: ./provision-agent.sh <agent_name> [chat_id] [acb_url]
# Example: ./provision-agent.sh quique 5874591 http://localhost:8090
#
# This script:
# 1. Copies acb-task-checker.py into the agent container
# 2. Creates a cronjob in jobs.json to run it every 15 minutes
# 3. Restarts the container to pick up changes
#
# Prerequisites:
# - Agent container must be running
# - ACB must be accessible from the container
# - Agent must have cron_mode: allow in config.yaml

set -euo pipefail

AGENT_NAME="${1:-}"
CHAT_ID="${2:-5874591}"
ACB_URL="${3:-http://localhost:8090}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONTAINER="${AGENT_NAME}-agent"

if [ -z "$AGENT_NAME" ]; then
    echo "Uso: $0 <agent_name> [chat_id] [acb_url]"
    echo "Agentes disponibles: quique, braulio, armando"
    exit 1
fi

echo "=== Provisionando agente: ${AGENT_NAME} ==="
echo "Container: ${CONTAINER}"
echo "Chat ID: ${CHAT_ID}"
echo "ACB URL: ${ACB_URL}"
echo ""

# 1. Verify container is running
if ! docker ps --format '{{.Names}}' | grep -q "^${CONTAINER}$"; then
    echo "❌ Error: Container '${CONTAINER}' no está corriendo"
    exit 1
fi

# 2. Verify ACB is reachable from container
echo "Verificando conectividad al ACB..."
if ! docker exec "${CONTAINER}" curl -sf "${ACB_URL}/health" > /dev/null 2>&1; then
    echo "❌ Error: ACB (${ACB_URL}) no accesible desde el container"
    echo "   Verifica que el ACB está corriendo y el container usa network_mode: host"
    exit 1
fi
echo "✅ ACB accesible"

# 3. Verify cron_mode is enabled in config
echo "Verificando cron_mode..."
if docker exec "${CONTAINER}" grep -q "cron_mode: allow" /opt/data/config.yaml 2>/dev/null; then
    echo "✅ cron_mode: allow"
else
    echo "⚠️  cron_mode no está en 'allow'. Añadiendo..."
    docker exec "${CONTAINER}" bash -c 'echo "cron_mode: allow" >> /opt/data/config.yaml'
fi

# 4. Copy checker script
# 1. Copiar el checker script
docker exec "${CONTAINER}" mkdir -p /opt/data/scripts/
docker cp "${SCRIPT_DIR}/acb-task-checker.py" "${CONTAINER}:/opt/data/scripts/acb-task-checker.py"
docker exec "$CONTAINER" chmod +x /opt/data/scripts/acb-task-checker.py
echo "✅ Script copiado"

# 5. Create cron directory if needed
docker exec "${CONTAINER}" mkdir -p /opt/data/cron/output 2>/dev/null || true

# 6. Create/update cronjob using Python
echo "Configurando cronjob..."
python3 -c "
import json, uuid, subprocess, sys

container = '${CONTAINER}'
agent_name = '${AGENT_NAME}'
chat_id = '${CHAT_ID}'

# Read existing jobs
result = subprocess.run(
    ['docker', 'exec', container, 'cat', '/opt/data/cron/jobs.json'],
    capture_output=True, text=True
)

if result.returncode == 0 and result.stdout.strip():
    data = json.loads(result.stdout)
else:
    data = {'jobs': []}

# Check if ACB job already exists
existing = [j for j in data['jobs'] if 'acb' in j.get('name', '').lower()]
if existing:
    print(f'⚠️  ACB cronjob ya existe (id={existing[0][\"id\"]}), actualizando...')
    # Update existing job
    job = existing[0]
    job['prompt'] = f'Ejecuta python3 /opt/data/scripts/acb-task-checker.py {agent_name} con la herramienta terminal. Si el script no devuelve nada (sin output), responde exactamente [SILENT] y nada más. Si devuelve tareas, empieza a trabajar en ellas y responde con tu progreso.'
    job['enabled'] = True
    job['state'] = 'scheduled'
else:
    # Create new job
    job = {
        'id': uuid.uuid4().hex[:12],
        'name': 'acb-task-check',
        'prompt': f'Ejecuta python3 /opt/data/scripts/acb-task-checker.py {agent_name} con la herramienta terminal. Si el script no devuelve nada (sin output), responde exactamente [SILENT] y nada más. Si devuelve tareas, empieza a trabajar en ellas y responde con tu progreso.',
        'skills': [],
        'skill': None,
        'model': None,
        'provider': None,
        'base_url': None,
        'script': 'acb-task-checker.py',
        'context_from': None,
        'schedule': {
            'kind': 'cron',
            'expr': '*/15 * * * *',
            'display': '*/15 * * * *'
        },
        'schedule_display': '*/15 * * * *',
        'repeat': {'times': None, 'completed': 0},
        'enabled': True,
        'state': 'scheduled',
        'paused_at': None,
        'paused_reason': None,
        'created_at': '2026-05-17T12:00:00.000000+02:00',
        'next_run_at': None,
        'last_run_at': None,
        'last_status': None,
        'last_error': None,
        'deliver': 'origin',
        'origin': {
            'platform': 'telegram',
            'chat_id': chat_id,
            'chat_name': 'Jesús Cifuentes',
            'thread_id': None
        },
        'last_delivery_error': None,
        'enabled_toolsets': None,
        'workdir': None
    }
    data['jobs'].append(job)

# Write updated jobs
with open('/tmp/acb_cron_jobs.json', 'w') as f:
    json.dump(data, f, indent=2, ensure_ascii=False)

# Copy into container
subprocess.run(['docker', 'cp', '/tmp/acb_cron_jobs.json', f'{container}:/opt/data/cron/jobs.json'], check=True)
print('✅ Cronjob ACB configurado')
"

# 7. Restart container
echo "Reiniciando container..."
docker restart "${CONTAINER}"

echo ""
echo "=== ✅ Agente ${AGENT_NAME} provisionado ==="
echo "  - Script: /opt/data/scripts/acb-task-checker.py"
echo "  - Cronjob: */15 * * * * (cada 15 minutos, silencioso si no hay tareas)"
echo "  - El primer run será en el próximo ciclo de 15 min"
echo ""
echo "Para verificar:"
echo "  docker exec ${CONTAINER} python3 /opt/data/scripts/acb-task-checker.py ${AGENT_NAME}"
echo "  docker exec ${CONTAINER} cat /opt/data/cron/jobs.json | python3 -m json.tool"