# rat-plugin-dbt-compat

A compatibility shim that adds dbt-style Jinja helpers (`env_var()`,
`var()`) to RAT's SQL template engine. Eases migration of existing dbt
projects into RAT — paste your existing model SQL and the most common
Jinja idioms still work.

## What it adds

| Helper | dbt equivalent | Behaviour |
|---|---|---|
| `env_var('NAME')` | dbt's `env_var('NAME')` | Reads from `os.environ`; raises if unset and no default |
| `env_var('NAME', 'default')` | dbt's `env_var('NAME', 'default')` | Falls back to default when unset |
| `var('key')` | dbt's `var('key')` | Reads from a `vars:` mapping (passed via pipeline config) |

## How it works

Registers under `rat.jinja_helpers` entry-point. The runner pulls every
registered helper into the Jinja environment used to render
`pipeline.sql`, so they're available alongside RAT's built-in
`ref()`, `is_incremental()`, etc.

## Install

Runner extension. Add to runner deps:

```bash
# In runner/pyproject.toml:
#   "rat-plugin-dbt-compat"
```

## Usage

In a pipeline.sql ported from dbt:

```sql
SELECT *
FROM {{ ref('bronze.orders') }}
WHERE region = '{{ var("target_region") }}'
  AND order_date >= '{{ env_var("CUTOFF_DATE", "2026-01-01") }}'
```

`vars` are supplied per-pipeline in `config.yaml`:

```yaml
vars:
  target_region: us-east
```

## Not included (intentionally)

This plugin only covers Jinja helpers. It is **not** a full dbt
runtime — dbt-specific features that don't fit RAT's model are out of
scope:

- `dbt_utils` macro library
- `incremental()` / `is_incremental()` (RAT has its own helpers with
  the same names — they work but are RAT-flavoured)
- `dbt_run_results.json` / dbt Cloud integration

For full dbt parity, run dbt-core alongside RAT and have it write to
RAT's S3 bucket. This plugin covers the "I just want my SQL to keep
working" case.

## Build & test

```bash
cd plugins/rat-plugin-dbt-compat
pip install -e '.[dev]'
pytest
```
