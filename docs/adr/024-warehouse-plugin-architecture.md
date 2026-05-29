# ADR-024: Warehouse plugin architecture

## Status: Accepted (2026-05-29)

## Context

RAT's **runner** side is already pluggable. A Python package can register
extensions through five `entry_points` groups and the runner picks them up at
load time:

- `rat.pipeline_types` — new source languages beyond SQL/Python
- `rat.strategies` — merge strategies (the six built-ins are themselves plugins)
- `rat.sources` — ingestion sources
- `rat.hooks` — lifecycle hooks
- `rat.jinja_helpers` — template helpers

The soft-delete, prql, and dbt-compat plugins are proof the runner seam works.

The **warehouse**, by contrast, is hardcoded. `runner/src/rat_runner/iceberg.py`
and `nessie.py` bake in exactly one stack: Apache Iceberg tables, S3/MinIO
storage, a Nessie REST catalog. Every consumer that touches table state —
`ratq` (query), `rat-plugin-diff`, `rat-plugin-docs-assistant`,
`rat-plugin-pg-sync` — reaches for that concrete stack directly.

This asymmetry is the architectural smell: you can swap the *compute* but not
the *storage substrate*. Supporting Unity Catalog, AWS Glue, Polaris, DuckLake,
or Delta today means editing core, not installing a plugin.

> Earlier framings we explicitly rejected (so we don't re-litigate them):
> - "RAT-as-platform / VSCode model" — over-scoped. The runner is already
>   pluggable; only the warehouse is missing. Scope is bounded to the warehouse.
> - "Merge strategies are deeply Iceberg-coupled in core" — wrong. They are
>   already `rat.strategies` plugins.
> - A two-seam `DataPlane` + `ComputeEngine` model — replaced by the simpler
>   runner + warehouse asymmetric model below.

## Decision

Add a sixth extension seam, **`rat.warehouses`**, that makes the storage
substrate pluggable, and refactor today's Iceberg+Nessie integration into the
reference implementation behind that contract. This completes the "v3" vision
(it is the completion of the plugin system, not a separate v4).

### 1. The warehouse is the load-bearing axis

A warehouse, once configured in `rat.yaml`, constrains what can pair with it:

- **Runners are loosely coupled** to the warehouse — they need an *attach*
  mechanism to read/write through it, but a SQL or Python runner is not
  warehouse-specific.
- **Strategies are tightly coupled** — a merge implementation is format-specific
  (an Iceberg SCD2 is not a Delta SCD2).

So warehouse selection is the primary constraint; runner/strategy availability
is derived from it.

### 2. The `Warehouse` Protocol contract

A warehouse plugin implements a Python `Protocol`:

- **Discovery:** `list_namespaces()`, `list_tables(namespace)`, `get_schema(ref)`
- **Read dispatch:** `attach_for_runner(runner_type, ...)` — hands a runner what
  it needs to read (e.g. a DuckDB `ATTACH`, a catalog handle)
- **Write:** `write(ref, data, strategy, opts)` — the single write entry point;
  the warehouse routes to the strategy implementation
- **Capability-gated extras:** `history(ref)`, `branch(...)`, `row_diff(...)`,
  guarded by a declared capability set:
  `{branching, time_travel, row_diff, scd2_native, partition_evolution, ...}`.
  Consumers check capabilities before calling; a warehouse that lacks
  `row_diff` simply doesn't advertise it and the diff UI degrades gracefully.

### 3. Strategy compatibility dispatch

The strategy **name** (`scd2`, `snapshot`, `append_only`, …) stays universal.
The **implementation** declares which warehouses it supports:

```python
# in a rat.strategies plugin
warehouses = ["iceberg-*"]
```

Dispatch mirrors Python's `__add__`/`__radd__`: given a (warehouse, strategy)
pair, resolve the implementation that declares compatibility. As part of this we
**drop the `_iceberg` suffix** from the existing strategy functions
(`scd2_iceberg` → `scd2`, etc.) — the suffix was leaking the implementation into
the universal name.

### 4. Postgres model, not VSCode model

Opinionated core + well-defined extension points + a **curated default bundle**.
The default bundle ships preinstalled so "one-line deploy, data in 5 minutes"
stays true:

- warehouse: `iceberg-nessie` (today's stack, now a plugin)
- runners: `sql`, `python`
- strategies: the six built-ins
- core plugins: diff, docs-assistant, etc.

Every piece is replaceable, but nothing is required-assembly. Framing: an *empty
RAT orchestrator* with *default preinstalled community plugins for every step*.

### 5. Migration sequence

1. Define the `Warehouse` Protocol + `rat.warehouses` entry point (design — this ADR + the contract). **Zero behavior change.**
2. Refactor today's Iceberg+Nessie integration as the reference impl behind the Protocol (still zero behavior change).
3. Add `warehouses=[...]` compat tags to the existing `rat.strategies` plugins; drop the `_iceberg` suffix.
4. Migrate consumers (`ratq`, `diff`, `docs-assistant`, `pg-sync`) to go through the Protocol instead of importing `iceberg.py`/`nessie.py` directly.
5. Validate with **Unity Catalog** (easy — still Iceberg) before **DuckLake** (spicy — different format), proving the seam holds across catalogs and then across formats.

## Consequences

- **Symmetric architecture:** both axes (compute via runners, storage via
  warehouses) are now plugins. New catalogs/formats arrive as packages, not core
  edits.
- **Bounded blast radius per step:** steps 1–2 are pure refactors with no
  behavior change, verifiable by the existing runner/query/integration suites.
- **One-time churn:** the `_iceberg` strategy rename and the consumer migration
  (step 4) touch several files; gated behind capability checks so partial
  warehouses degrade rather than break.
- **Capability negotiation is now explicit:** features like row-diff and
  branching become opt-in per warehouse instead of assumed-present, which the
  diff/history UIs must handle (they already guard on Nessie availability).
- This ADR supersedes the stale memory note that pinned this to "ADR-018";
  018 is the cloud-credential-vending ADR. The warehouse ADR is **024**.

## References

- Runner plugin seams: `runner/src/rat_runner/` (`strategies.py` + the
  `rat.*` entry-point groups in `runner/pyproject.toml`)
- Reference impl to wrap: `runner/src/rat_runner/iceberg.py`, `nessie.py`
- Merge strategies: ADR-era merge-strategy work (`full_refresh`, `incremental`,
  `append_only`, `delete_insert`, `scd2`, `snapshot`)
- Plugin trust model: [ADR-017](017-python-pipeline-trust-model.md)
