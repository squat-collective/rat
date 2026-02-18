#!/usr/bin/env bash
# RAT — Quick Installer
# Downloads docker-compose.yml + .env and optionally starts all services.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/squat-collective/rat/main/install/install.sh | bash
#   curl -fsSL ... | bash -s -- --start
#   curl -fsSL ... | bash -s -- --version=2.1.0 --dir=my-rat
set -euo pipefail

REPO="squat-collective/rat"
BRANCH="main"
DIR="rat"
VERSION=""
START=false

for arg in "$@"; do
  case "$arg" in
    --start)       START=true ;;
    --version=*)   VERSION="${arg#*=}" ;;
    --dir=*)       DIR="${arg#*=}" ;;
    --help|-h)
      echo "Usage: install.sh [--start] [--version=X.Y.Z] [--dir=NAME]"
      echo "  --start        Start services after downloading"
      echo "  --version=X.Y.Z  Pin image tags to a specific release"
      echo "  --dir=NAME     Directory name (default: rat)"
      exit 0 ;;
    *) echo "Unknown option: $arg (try --help)"; exit 1 ;;
  esac
done

# ── Prerequisites ─────────────────────────────────────────────
check_cmd() {
  if ! command -v "$1" &>/dev/null; then
    echo "Error: $1 is required but not installed." >&2
    exit 1
  fi
}

check_cmd docker
if ! docker compose version &>/dev/null; then
  echo "Error: Docker Compose V2 is required (docker compose plugin)." >&2
  echo "       Install it: https://docs.docker.com/compose/install/" >&2
  exit 1
fi

# ── Download ──────────────────────────────────────────────────
BASE_URL="https://raw.githubusercontent.com/${REPO}/${BRANCH}/install"

echo "Creating ${DIR}/ ..."
mkdir -p "$DIR"

echo "Downloading docker-compose.yml ..."
curl -fsSL "${BASE_URL}/docker-compose.yml" -o "${DIR}/docker-compose.yml"

echo "Downloading .env ..."
curl -fsSL "${BASE_URL}/.env" -o "${DIR}/.env"

# ── Pin version ───────────────────────────────────────────────
if [ -n "$VERSION" ]; then
  echo "Pinning images to version ${VERSION} ..."
  # Cross-platform sed (GNU vs BSD)
  if sed --version &>/dev/null 2>&1; then
    sed -i "s|ghcr.io/squat-collective/\(.*\):latest|ghcr.io/squat-collective/\1:${VERSION}|g" "${DIR}/docker-compose.yml"
  else
    sed -i '' "s|ghcr.io/squat-collective/\(.*\):latest|ghcr.io/squat-collective/\1:${VERSION}|g" "${DIR}/docker-compose.yml"
  fi
fi

echo ""
echo "Done! RAT files are in ./${DIR}/"
echo ""

# ── Optionally start ─────────────────────────────────────────
if [ "$START" = true ]; then
  echo "Starting RAT ..."
  docker compose -f "${DIR}/docker-compose.yml" --env-file "${DIR}/.env" up -d
  echo ""
  echo "RAT is starting! Check status with:"
  echo "  docker compose -f ${DIR}/docker-compose.yml ps"
else
  echo "To start RAT:"
  echo "  cd ${DIR}"
  echo "  docker compose up -d"
  echo ""
  echo "Then open http://localhost:3000"
fi
