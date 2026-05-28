# rat-plugin-row-stats

Post-write hook that logs row counts + schema summary after every
pipeline run. Useful for monitoring data volume, spotting regressions
("we wrote 10x fewer rows than yesterday"), and the kind of routine
visibility that's annoying to wire up per-pipeline.

## How it works

Registers under `rat.hooks` entry-point. The runner invokes every
registered hook after a pipeline's table write succeeds, passing the
written Arrow table + pipeline metadata. The hook logs:

- Row count
- Column count
- Schema diff vs the previous run (if available)
- Approximate uncompressed bytes

Output goes to the runner's standard logger — `slog` JSON in production,
captured in run logs surfaced by the portal.

## Install

Runner extension. Add to runner deps:

```bash
# In runner/pyproject.toml:
#   "rat-plugin-row-stats"
```

The runner discovers `rat.hooks` entry-points on startup; no per-
pipeline configuration needed — once installed, the hook fires on every
write.

## Sample output

```
{"level":"INFO","msg":"row_stats","pipeline":"shop.bronze.orders",
 "rows":9493,"columns":6,"bytes":117893,
 "schema":["order_id","customer_id","product_id","quantity","ordered_at","status"]}
```

## Build & test

```bash
cd plugins/rat-plugin-row-stats
pip install -e '.[dev]'
pytest
```

## Disabling per-pipeline

Hooks run for every pipeline by default. To skip for a specific pipeline,
the runner's hook framework respects a `skip_hooks: [row_stats]` field in
`config.yaml`.
