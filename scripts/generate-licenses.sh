#!/usr/bin/env bash
# Generate third-party license notices for all RAT packages.
# Produces per-package reports in each component directory and
# a combined THIRD-PARTY-NOTICES.md at the repo root.
#
# Usage: ./scripts/generate-licenses.sh
#
# Requirements (installed automatically in CI):
#   - go-licenses (Go)
#   - pip-licenses (Python)
#   - license-checker (Node.js)

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUTPUT_DIR="${REPO_ROOT}/licenses"
mkdir -p "$OUTPUT_DIR"

header() {
  echo ""
  echo "================================================================"
  echo "  $1"
  echo "================================================================"
  echo ""
}

# ── Go (platform/) ──────────────────────────────────────────────
generate_go_licenses() {
  header "Go (platform/) — go-licenses"

  cd "$REPO_ROOT/platform"

  if ! command -v go-licenses &>/dev/null; then
    echo "Installing go-licenses..."
    go install github.com/google/go-licenses@latest
  fi

  go-licenses report ./... --template "$REPO_ROOT/scripts/licenses.tpl" \
    > "$OUTPUT_DIR/platform.md" 2>/dev/null || true

  # Fallback: if template fails, use CSV
  if [ ! -s "$OUTPUT_DIR/platform.md" ]; then
    echo "# Third-Party Licenses — platform (ratd)" > "$OUTPUT_DIR/platform.md"
    echo "" >> "$OUTPUT_DIR/platform.md"
    echo "| Module | License |" >> "$OUTPUT_DIR/platform.md"
    echo "|--------|---------|" >> "$OUTPUT_DIR/platform.md"
    go-licenses report ./... 2>/dev/null | sort | while IFS=',' read -r mod url license; do
      echo "| \`${mod}\` | ${license} |" >> "$OUTPUT_DIR/platform.md"
    done
  fi

  cp "$OUTPUT_DIR/platform.md" "$REPO_ROOT/platform/THIRD-PARTY-NOTICES.md"
  echo "  -> platform/THIRD-PARTY-NOTICES.md"
}

# ── Python (runner/) ────────────────────────────────────────────
generate_python_licenses() {
  local pkg_name="$1"
  local pkg_dir="$2"

  header "Python (${pkg_dir}/) — pip-licenses"

  cd "$REPO_ROOT/$pkg_dir"

  if ! command -v pip-licenses &>/dev/null; then
    echo "Installing pip-licenses..."
    pip install --quiet pip-licenses
  fi

  # Install the package deps in a temp way if not already installed
  pip install --quiet -e . 2>/dev/null || true

  {
    echo "# Third-Party Licenses — ${pkg_name}"
    echo ""
    echo "| Package | Version | License |"
    echo "|---------|---------|---------|"
    pip-licenses --format=json --with-license-file --no-license-path 2>/dev/null \
      | python3 -c "
import json, sys
data = json.load(sys.stdin)
for pkg in sorted(data, key=lambda x: x['Name'].lower()):
    name = pkg['Name']
    version = pkg['Version']
    lic = pkg['License']
    # Skip the package itself
    if name.lower() in ('${pkg_name}',):
        continue
    print(f'| \`{name}\` | {version} | {lic} |')
" 2>/dev/null || echo "| _error generating_ | | |"
  } > "$OUTPUT_DIR/${pkg_dir}.md"

  cp "$OUTPUT_DIR/${pkg_dir}.md" "$REPO_ROOT/${pkg_dir}/THIRD-PARTY-NOTICES.md"
  echo "  -> ${pkg_dir}/THIRD-PARTY-NOTICES.md"
}

# ── Node.js (portal/, sdk-typescript/) ──────────────────────────
generate_node_licenses() {
  local pkg_name="$1"
  local pkg_dir="$2"

  header "Node.js (${pkg_dir}/) — license-checker"

  cd "$REPO_ROOT/$pkg_dir"

  if ! command -v license-checker &>/dev/null; then
    echo "Installing license-checker..."
    npm install -g license-checker 2>/dev/null
  fi

  {
    echo "# Third-Party Licenses — ${pkg_name}"
    echo ""
    echo "| Package | License |"
    echo "|---------|---------|"
    license-checker --json --production 2>/dev/null \
      | python3 -c "
import json, sys
data = json.load(sys.stdin)
for pkg_key in sorted(data.keys(), key=str.lower):
    info = data[pkg_key]
    lic = info.get('licenses', 'Unknown')
    # Skip the package itself
    if '${pkg_name}' in pkg_key.lower():
        continue
    print(f'| \`{pkg_key}\` | {lic} |')
" 2>/dev/null || echo "| _error generating_ | |"
  } > "$OUTPUT_DIR/${pkg_dir}.md"

  cp "$OUTPUT_DIR/${pkg_dir}.md" "$REPO_ROOT/${pkg_dir}/THIRD-PARTY-NOTICES.md"
  echo "  -> ${pkg_dir}/THIRD-PARTY-NOTICES.md"
}

# ── Combine into root THIRD-PARTY-NOTICES.md ────────────────────
combine_notices() {
  header "Combining into THIRD-PARTY-NOTICES.md"

  {
    echo "# Third-Party Licenses"
    echo ""
    echo "This file lists all third-party dependencies used by RAT and their licenses."
    echo "Generated automatically — do not edit manually."
    echo ""
    echo "---"

    for report in "$OUTPUT_DIR"/*.md; do
      [ -f "$report" ] || continue
      echo ""
      cat "$report"
      echo ""
      echo "---"
    done
  } > "$REPO_ROOT/THIRD-PARTY-NOTICES.md"

  echo "  -> THIRD-PARTY-NOTICES.md (root)"
}

# ── Main ────────────────────────────────────────────────────────
main() {
  echo "Generating third-party license reports for RAT..."

  generate_go_licenses
  generate_python_licenses "rat-runner" "runner"
  generate_python_licenses "rat-query" "query"
  generate_node_licenses "rat-portal" "portal"
  generate_node_licenses "rat-client" "sdk-typescript"
  combine_notices

  echo ""
  echo "Done! Reports in:"
  echo "  - THIRD-PARTY-NOTICES.md (combined)"
  echo "  - platform/THIRD-PARTY-NOTICES.md"
  echo "  - runner/THIRD-PARTY-NOTICES.md"
  echo "  - query/THIRD-PARTY-NOTICES.md"
  echo "  - portal/THIRD-PARTY-NOTICES.md"
  echo "  - sdk-typescript/THIRD-PARTY-NOTICES.md"
}

main "$@"
