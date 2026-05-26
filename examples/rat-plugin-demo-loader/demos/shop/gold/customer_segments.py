# =============================================================================
# gold.customer_segments — a PYTHON pipeline
# -----------------------------------------------------------------------------
# Most RAT pipelines are SQL files, but a pipeline.py file works the same way.
# The runner executes this script with three useful globals already wired up:
#
#   pa            — pyarrow (Apache Arrow Python bindings)
#   duckdb_conn   — a DuckDB connection that can read every Iceberg table
#                   under any namespace (via iceberg_scan())
#   result        — set this to a pyarrow.Table; the runner writes it to
#                   {namespace}.{layer}.{name} using the chosen merge strategy
#
# This pipeline is intentionally self-contained: it doesn't read any upstream
# table, it just constructs a small reference dataset with PyArrow. The point
# is to show the format. A real Python pipeline would call duckdb_conn to
# read from `ref()` upstreams and use pyarrow / pandas / numpy to compute
# things SQL can't easily express.
# =============================================================================
# Annotations work in pipeline.py the same way they do in pipeline.sql:
# any leading `# @key: value` lines are parsed by the runner.
# =============================================================================
# @merge_strategy: full_refresh
# @description: Customer segments — Python pipeline showcase.

import pyarrow as pa

# A toy segmentation: deterministic synthesis so the demo reproduces.
ids: list[int] = list(range(1, 101))


def segment_of(i: int) -> str:
    if i % 20 == 0:
        return "whale"
    if i % 5 == 0:
        return "loyal"
    return "casual"


segments = [segment_of(i) for i in ids]
estimated_spend = [round(i * 17.3 + (i * i % 200), 2) for i in ids]

# `result` MUST be a pyarrow.Table — that is the contract.
result = pa.Table.from_pydict(
    {
        "customer_id": ids,
        "segment": segments,
        "estimated_spend_eur": estimated_spend,
    }
)
