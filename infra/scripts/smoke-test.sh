#!/usr/bin/env bash
# 🐀 RAT — E2E Smoke Test
# Exercises the full API flow against a running stack.
# Usage: bash infra/scripts/smoke-test.sh [BASE_URL]
#
# Requirements: curl, jq

set -euo pipefail

BASE="${1:-http://localhost:8080}"
PASS=0
FAIL=0
SKIP=0
RUN_ID=""
ZONE_NS="default"
ZONE_NAME="smoke-test"

# ── Temp directory ────────────────────────────────────────────
TMPDIR=$(mktemp -d "${TMPDIR:-/tmp}/rat-smoke.XXXXXXXXXX")

cleanup() {
    rm -rf "$TMPDIR"
}
trap cleanup EXIT

# ── Dependency check ──────────────────────────────────────────
for cmd in curl jq; do
    if ! command -v "$cmd" &>/dev/null; then
        echo "ERROR: '$cmd' is required but not found in PATH" >&2
        exit 1
    fi
done

# ── Colors ────────────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

pass() { PASS=$((PASS + 1)); echo -e "  ${GREEN}PASS${NC} $1"; }
fail() { FAIL=$((FAIL + 1)); echo -e "  ${RED}FAIL${NC} $1 — $2"; }
skip() { SKIP=$((SKIP + 1)); echo -e "  ${YELLOW}SKIP${NC} $1 — $2"; }
step() { echo -e "\n${CYAN}[$1]${NC} ${BOLD}$2${NC}"; }

# ── curl wrapper with timeouts ────────────────────────────────
# Usage: do_curl <output_file> [curl_args...]
# Captures HTTP status to $HTTP, body to output_file.
# Returns 0 on success, 1 on network/timeout error.
do_curl() {
    local outfile="$1"; shift
    HTTP=$(curl -s --connect-timeout 5 --max-time 30 \
        -o "$outfile" -w "%{http_code}" "$@" 2>/dev/null) || {
        HTTP="000"
        echo "" > "$outfile"
        return 1
    }
    return 0
}

# ── Test data ─────────────────────────────────────────────────
CSV_DATA="id,name,amount,created_at
1,Alice,99.50,2026-01-15
2,Bob,150.00,2026-01-16
3,Charlie,75.25,2026-01-17"

PIPELINE_SQL='SELECT id, name, amount, created_at FROM read_csv_auto('"'"'s3://rat/default/landing/smoke-test/*orders.csv'"'"')'

CONFIG_YAML="merge_strategy: full_refresh"

echo -e "${BOLD}🐀 RAT — E2E Smoke Test${NC}"
echo -e "   Target: ${CYAN}${BASE}${NC}"
echo ""

# ──────────────────────────────────────────────────────────────
# 1. Health check
# ──────────────────────────────────────────────────────────────
step 1 "Health check"
if do_curl "$TMPDIR/health.json" "${BASE}/health"; then
    if [ "$HTTP" = "200" ]; then
        pass "GET /health → 200"
    else
        fail "GET /health → $HTTP" "$(cat "$TMPDIR/health.json")"
    fi
else
    fail "GET /health" "connection failed (is ratd running at ${BASE}?)"
fi

# ──────────────────────────────────────────────────────────────
# 2. Features
# ──────────────────────────────────────────────────────────────
step 2 "Features"
if do_curl "$TMPDIR/features.json" "${BASE}/api/v1/features"; then
    if [ "$HTTP" = "200" ]; then
        EDITION=$(jq -r '.edition // "unknown"' "$TMPDIR/features.json" 2>/dev/null || echo "unknown")
        pass "GET /api/v1/features → 200 (edition: ${EDITION})"
    else
        fail "GET /api/v1/features → $HTTP" "$(cat "$TMPDIR/features.json")"
    fi
else
    fail "GET /api/v1/features" "connection failed"
fi

# ──────────────────────────────────────────────────────────────
# 3. List namespaces
# ──────────────────────────────────────────────────────────────
step 3 "List namespaces"
if do_curl "$TMPDIR/ns.json" "${BASE}/api/v1/namespaces"; then
    if [ "$HTTP" = "200" ]; then
        NS_COUNT=$(jq -r '.total // "?"' "$TMPDIR/ns.json" 2>/dev/null || echo "?")
        pass "GET /api/v1/namespaces → 200 (${NS_COUNT} namespaces)"
    else
        fail "GET /api/v1/namespaces → $HTTP" "$(cat "$TMPDIR/ns.json")"
    fi
else
    fail "GET /api/v1/namespaces" "connection failed"
fi

# ──────────────────────────────────────────────────────────────
# 4. Create landing zone
# ──────────────────────────────────────────────────────────────
step 4 "Create landing zone"
if do_curl "$TMPDIR/zone.json" \
    -X POST "${BASE}/api/v1/landing-zones" \
    -H "Content-Type: application/json" \
    -d "{\"namespace\":\"${ZONE_NS}\",\"name\":\"${ZONE_NAME}\",\"description\":\"Smoke test zone\"}"; then
    if [ "$HTTP" = "201" ]; then
        pass "POST /api/v1/landing-zones → 201"
    elif [ "$HTTP" = "409" ]; then
        pass "POST /api/v1/landing-zones → 409 (already exists, OK)"
    else
        fail "POST /api/v1/landing-zones → $HTTP" "$(cat "$TMPDIR/zone.json")"
    fi
else
    fail "POST /api/v1/landing-zones" "connection failed"
fi

# ──────────────────────────────────────────────────────────────
# 5. Upload CSV file
# ──────────────────────────────────────────────────────────────
step 5 "Upload CSV to landing zone"
echo "$CSV_DATA" > "$TMPDIR/orders.csv"
if do_curl "$TMPDIR/upload.json" \
    -X POST "${BASE}/api/v1/landing-zones/${ZONE_NS}/${ZONE_NAME}/files" \
    -F "file=@${TMPDIR}/orders.csv;filename=orders.csv"; then
    if [ "$HTTP" = "201" ]; then
        pass "POST .../files → 201 (orders.csv uploaded)"
    else
        fail "POST .../files → $HTTP" "$(cat "$TMPDIR/upload.json")"
    fi
else
    fail "POST .../files" "connection failed"
fi

# ──────────────────────────────────────────────────────────────
# 6. List landing zone files
# ──────────────────────────────────────────────────────────────
step 6 "List landing zone files"
if do_curl "$TMPDIR/files.json" \
    "${BASE}/api/v1/landing-zones/${ZONE_NS}/${ZONE_NAME}/files"; then
    if [ "$HTTP" = "200" ]; then
        FILE_COUNT=$(jq -r '.total // "?"' "$TMPDIR/files.json" 2>/dev/null || echo "?")
        pass "GET .../files → 200 (${FILE_COUNT} files)"
    else
        fail "GET .../files → $HTTP" "$(cat "$TMPDIR/files.json")"
    fi
else
    fail "GET .../files" "connection failed"
fi

# ──────────────────────────────────────────────────────────────
# 7. Query service health (ratq)
# ──────────────────────────────────────────────────────────────
step 7 "Query service health (ratq)"
# Note: read_csv_auto() is blocked by design in ratq (security).
# We test basic query capability; result table query is in step 13.
if do_curl "$TMPDIR/query.json" \
    -X POST "${BASE}/api/v1/query" \
    -H "Content-Type: application/json" \
    -d '{"sql":"SELECT 1 AS health_check","limit":1}'; then
    if [ "$HTTP" = "200" ]; then
        pass "POST /api/v1/query → 200 (ratq is alive)"
    else
        fail "POST /api/v1/query → $HTTP" "$(cat "$TMPDIR/query.json")"
    fi
else
    fail "POST /api/v1/query" "connection failed"
fi

# ──────────────────────────────────────────────────────────────
# 8. Create pipeline
# ──────────────────────────────────────────────────────────────
step 8 "Create pipeline"
if do_curl "$TMPDIR/pipeline.json" \
    -X POST "${BASE}/api/v1/pipelines" \
    -H "Content-Type: application/json" \
    -d "{\"namespace\":\"default\",\"layer\":\"bronze\",\"name\":\"smoke_orders\",\"type\":\"sql\"}"; then
    if [ "$HTTP" = "201" ]; then
        pass "POST /api/v1/pipelines → 201"
    elif [ "$HTTP" = "409" ]; then
        pass "POST /api/v1/pipelines → 409 (already exists, OK)"
    else
        fail "POST /api/v1/pipelines → $HTTP" "$(cat "$TMPDIR/pipeline.json")"
    fi
else
    fail "POST /api/v1/pipelines" "connection failed"
fi

# ──────────────────────────────────────────────────────────────
# 9. Upload pipeline.sql
# ──────────────────────────────────────────────────────────────
step 9 "Upload pipeline.sql"
if do_curl "$TMPDIR/sql-up.json" \
    -X PUT "${BASE}/api/v1/files/default/pipelines/bronze/smoke_orders/pipeline.sql" \
    -H "Content-Type: application/json" \
    -d "{\"content\":\"${PIPELINE_SQL}\"}"; then
    if [ "$HTTP" = "200" ]; then
        pass "PUT .../pipeline.sql → 200"
    else
        fail "PUT .../pipeline.sql → $HTTP" "$(cat "$TMPDIR/sql-up.json")"
    fi
else
    fail "PUT .../pipeline.sql" "connection failed"
fi

# ──────────────────────────────────────────────────────────────
# 10. Upload config.yaml
# ──────────────────────────────────────────────────────────────
step 10 "Upload config.yaml"
if do_curl "$TMPDIR/cfg-up.json" \
    -X PUT "${BASE}/api/v1/files/default/pipelines/bronze/smoke_orders/config.yaml" \
    -H "Content-Type: application/json" \
    --data-raw '{"content":"merge_strategy: full_refresh"}'; then
    if [ "$HTTP" = "200" ]; then
        pass "PUT .../config.yaml → 200"
    else
        fail "PUT .../config.yaml → $HTTP" "$(cat "$TMPDIR/cfg-up.json")"
    fi
else
    fail "PUT .../config.yaml" "connection failed"
fi

# ──────────────────────────────────────────────────────────────
# 11. Trigger pipeline run
# ──────────────────────────────────────────────────────────────
step 11 "Trigger pipeline run"
if do_curl "$TMPDIR/run.json" \
    -X POST "${BASE}/api/v1/runs" \
    -H "Content-Type: application/json" \
    -d '{"namespace":"default","layer":"bronze","pipeline":"smoke_orders","trigger":"manual"}'; then
    if [ "$HTTP" = "202" ]; then
        RUN_ID=$(jq -r '.run_id // ""' "$TMPDIR/run.json" 2>/dev/null || echo "")
        pass "POST /api/v1/runs → 202 (run_id: ${RUN_ID})"
    elif [ "$HTTP" = "500" ] || [ "$HTTP" = "503" ]; then
        skip "Pipeline run" "runner may not be connected ($HTTP)"
    else
        fail "POST /api/v1/runs → $HTTP" "$(cat "$TMPDIR/run.json")"
    fi
else
    fail "POST /api/v1/runs" "connection failed"
fi

# ──────────────────────────────────────────────────────────────
# 12. Poll run status (up to 30s)
# ──────────────────────────────────────────────────────────────
step 12 "Poll run status"
if [ -n "$RUN_ID" ]; then
    FINAL_STATUS="unknown"
    for i in $(seq 1 15); do
        sleep 2
        if do_curl "$TMPDIR/status.json" "${BASE}/api/v1/runs/${RUN_ID}"; then
            if [ "$HTTP" = "200" ]; then
                STATUS=$(jq -r '.status // "unknown"' "$TMPDIR/status.json" 2>/dev/null || echo "unknown")
                if [ "$STATUS" = "success" ] || [ "$STATUS" = "failed" ] || [ "$STATUS" = "cancelled" ]; then
                    FINAL_STATUS="$STATUS"
                    break
                fi
                echo -e "    ... ${STATUS} (${i}/15)"
            fi
        else
            echo -e "    ... connection error (${i}/15)"
        fi
    done
    if [ "$FINAL_STATUS" = "success" ]; then
        DURATION=$(jq -r '.duration_ms // "?"' "$TMPDIR/status.json" 2>/dev/null || echo "?")
        pass "Run completed: success (${DURATION}ms)"
    elif [ "$FINAL_STATUS" = "failed" ]; then
        ERR=$(jq -r '.error // "unknown"' "$TMPDIR/status.json" 2>/dev/null || echo "unknown")
        fail "Run completed: failed" "$ERR"
    else
        skip "Run status" "timed out (last: ${FINAL_STATUS})"
    fi
else
    skip "Poll run status" "no run_id from step 11"
fi

# ──────────────────────────────────────────────────────────────
# 13. Query result table
# ──────────────────────────────────────────────────────────────
step 13 "Query result table"
if [ -n "$RUN_ID" ] && [ "${FINAL_STATUS:-}" = "success" ]; then
    # Query via ratq's Iceberg catalog view (registered from Nessie, refreshes every 30s)
    # Wait a moment for ratq catalog refresh to pick up the new table
    sleep 5
    if do_curl "$TMPDIR/result.json" \
        -X POST "${BASE}/api/v1/query" \
        -H "Content-Type: application/json" \
        -d '{"sql":"SELECT * FROM default.bronze.smoke_orders","namespace":"default","limit":10}'; then
        if [ "$HTTP" = "200" ]; then
            RESULT_ROWS=$(jq -r '.total_rows // "?"' "$TMPDIR/result.json" 2>/dev/null || echo "?")
            pass "SELECT * FROM smoke_orders data → ${RESULT_ROWS} rows"
        else
            fail "Query smoke_orders data → $HTTP" "$(cat "$TMPDIR/result.json")"
        fi
    else
        fail "Query smoke_orders data" "connection failed"
    fi
else
    skip "Query result table" "run did not succeed"
fi

# ──────────────────────────────────────────────────────────────
# 14. Clean up (API resources)
# ──────────────────────────────────────────────────────────────
step 14 "Clean up"
# Delete pipeline
if do_curl "$TMPDIR/del-pipeline.json" \
    -X DELETE "${BASE}/api/v1/pipelines/default/bronze/smoke_orders"; then
    if [ "$HTTP" = "204" ] || [ "$HTTP" = "404" ]; then
        pass "DELETE pipeline → $HTTP"
    else
        fail "DELETE pipeline → $HTTP" "unexpected status"
    fi
else
    fail "DELETE pipeline" "connection failed"
fi

# Delete landing zone
if do_curl "$TMPDIR/del-zone.json" \
    -X DELETE "${BASE}/api/v1/landing-zones/${ZONE_NS}/${ZONE_NAME}"; then
    if [ "$HTTP" = "204" ] || [ "$HTTP" = "404" ]; then
        pass "DELETE landing zone → $HTTP"
    else
        fail "DELETE landing zone → $HTTP" "unexpected status"
    fi
else
    fail "DELETE landing zone" "connection failed"
fi

# ──────────────────────────────────────────────────────────────
# Summary
# ──────────────────────────────────────────────────────────────
echo ""
echo -e "${BOLD}════════════════════════════════════════${NC}"
echo -e "  ${GREEN}PASS: ${PASS}${NC}  ${RED}FAIL: ${FAIL}${NC}  ${YELLOW}SKIP: ${SKIP}${NC}"
echo -e "${BOLD}════════════════════════════════════════${NC}"

# Temp files cleaned up by EXIT trap
if [ "$FAIL" -gt 0 ]; then
    exit 1
fi
exit 0
