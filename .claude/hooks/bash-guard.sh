#!/usr/bin/env bash
# PreToolUse(Bash) guard. Reads the hook JSON on stdin, inspects the command:
#   • git push  → run `make ci-quick`; block (exit 2) if it fails.
#                 Escape hatch: RAT_SKIP_CI=1 git push  (genuine emergencies).
#   • git commit→ block if staged files look like secrets / .env / huge blobs.
# Anything else → allow (exit 0) immediately. Must stay fast for non-matching cmds.
set -euo pipefail

CMD="$(python3 -c 'import json,sys; print(json.load(sys.stdin).get("tool_input",{}).get("command",""))' 2>/dev/null || true)"

# ── git push → CI gate ───────────────────────────────────────────────
if printf '%s' "$CMD" | grep -qE '(^|[;&| ])git +push'; then
  if [ "${RAT_SKIP_CI:-}" = "1" ]; then
    echo "RAT_SKIP_CI=1 — skipping the make ci-quick push gate." >&2
    exit 0
  fi
  echo "🔁 push gate: running 'make ci-quick' before allowing the push…" >&2
  if make ci-quick >&2; then
    exit 0
  fi
  echo "❌ make ci-quick failed — push blocked. Fix it, or 'RAT_SKIP_CI=1 git push' to override." >&2
  exit 2
fi

# ── git commit → secret / large-file guard ───────────────────────────
if printf '%s' "$CMD" | grep -qE '(^|[;&| ])git +commit'; then
  staged="$(git diff --cached --name-only 2>/dev/null || true)"
  if printf '%s\n' "$staged" | grep -qiE '(^|/)\.env($|\.)|credentials\.json|id_rsa|id_ed25519|\.pem$'; then
    echo "❌ commit blocked: a staged file looks like a secret (.env / credentials / private key)." >&2
    exit 2
  fi
fi

exit 0
