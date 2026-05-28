#!/usr/bin/env bash
# Refresh @sha256 digests pinned in all Dockerfiles to the digest each
# `image:tag` resolves to right now. Run when a base-image CVE patch
# needs to flow into builds; commit the resulting diff.
set -euo pipefail
cd "$(dirname "$0")/.."

mapfile -t FILES < <(find platform runner query portal examples \
  -name 'Dockerfile' -not -path '*/node_modules/*' | sort)

mapfile -t REFS < <(
  grep -hE '^FROM [a-z0-9][a-z0-9._/-]*:[A-Za-z0-9._-]+' "${FILES[@]}" \
    | awk '{print $2}' | sed -E 's/@sha256:[a-f0-9]+//' \
    | grep -v '^scratch$' | sort -u
)

declare -A DIGESTS
for ref in "${REFS[@]}"; do
  echo "→ pulling $ref"
  docker pull -q "$ref" >/dev/null
  DIGESTS[$ref]=$(docker inspect --format='{{index .RepoDigests 0}}' "$ref" | sed -E 's/.*@//')
  echo "  $ref → ${DIGESTS[$ref]}"
done

changed=0
for file in "${FILES[@]}"; do
  before=$(sha256sum "$file" | awk '{print $1}')
  for ref in "${!DIGESTS[@]}"; do
    sed -i -E "s#(FROM )${ref}(@sha256:[a-f0-9]+)?( |\$)#\\1${ref}@${DIGESTS[$ref]}\\3#g" "$file"
  done
  [[ $(sha256sum "$file" | awk '{print $1}') != "$before" ]] && changed=$((changed+1))
done

echo
echo "Summary:"
for ref in "${!DIGESTS[@]}"; do printf '  %-32s %s\n' "$ref" "${DIGESTS[$ref]}"; done
echo "Files scanned: ${#FILES[@]}, changed: $changed"
if [[ $changed -eq 0 ]]; then echo "No changes — digests already current."
else echo "Updated $changed Dockerfile(s) — review with: git diff -- platform runner query portal examples"; fi
