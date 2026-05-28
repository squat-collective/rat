#!/usr/bin/env bash
# SessionStart: print a short situational-awareness banner. stdout is added
# to the session context, so keep it tight and factual.
set -euo pipefail

branch="$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo '?')"
dirty="$(git status --porcelain 2>/dev/null | wc -l | tr -d ' ')"
ahead="$(git rev-list --count '@{u}'..HEAD 2>/dev/null || echo '?')"

echo "RAT session — branch '${branch}', ${dirty} uncommitted file(s), ${ahead} commit(s) ahead of upstream."

# Dev-stack status (best-effort; never fail the session).
if docker compose -f infra/docker-compose.yml ps --format '{{.Service}} {{.State}}' >/tmp/.rat-stack 2>/dev/null && [ -s /tmp/.rat-stack ]; then
  up="$(grep -c running /tmp/.rat-stack || true)"
  echo "Dev stack: ${up} service(s) running. Reminder: 'make ci-quick' before push (hook enforces)."
else
  echo "Dev stack: not running (make up). Reminder: 'make ci-quick' before push (hook enforces)."
fi
rm -f /tmp/.rat-stack 2>/dev/null || true
exit 0
