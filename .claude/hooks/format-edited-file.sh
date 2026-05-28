#!/usr/bin/env bash
# PostToolUse(Edit|Write|MultiEdit): format the single file that was just
# edited, using a NATIVE host formatter if one is installed. If the tool
# isn't on PATH (this repo runs everything in Docker, so it often isn't),
# skip silently — `make lint`/the push gate is the real backstop. Never
# blocks (always exit 0); must be fast (single file, native tools only).
set -euo pipefail

FILE="$(python3 -c 'import json,sys; print(json.load(sys.stdin).get("tool_input",{}).get("file_path",""))' 2>/dev/null || true)"
[ -n "$FILE" ] && [ -f "$FILE" ] || exit 0

case "$FILE" in
  *.go)
    command -v gofmt >/dev/null 2>&1 && gofmt -w "$FILE" || true
    ;;
  *.py)
    # Prefer the pinned ruff if present; never auto-install here (too slow).
    command -v ruff >/dev/null 2>&1 && ruff format "$FILE" >/dev/null 2>&1 || true
    ;;
  *.ts|*.tsx|*.js|*.jsx)
    # eslint --fix needs node_modules; only run if a local binary exists.
    if [ -x "portal/node_modules/.bin/eslint" ] && printf '%s' "$FILE" | grep -q '^portal/'; then
      portal/node_modules/.bin/eslint --fix "$FILE" >/dev/null 2>&1 || true
    fi
    ;;
esac
exit 0
