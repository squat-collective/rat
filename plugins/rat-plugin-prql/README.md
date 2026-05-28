# rat-plugin-prql

Adds [PRQL](https://prql-lang.org/) as a first-class pipeline language
alongside SQL and Python. PRQL is a pipelined relational query language
that compiles to SQL — readable, composable, and especially nice for
multi-step transforms.

## How it works

Adds a `prql` pipeline type via the `rat.pipeline_types` entry-point.
When the runner encounters a `pipeline.prql` file, this plugin:

1. Reads the PRQL source.
2. Compiles to DuckDB-flavoured SQL via the official PRQL compiler.
3. Hands the SQL to the runner's normal SQL execution path —
   `ref()`, Jinja templating, merge strategies all work as usual.

## Install

Runner extension (Python entry-point). Add to runner deps:

```bash
# In runner/pyproject.toml:
#   "rat-plugin-prql"
```

The runner discovers it via `rat.pipeline_types` on startup.

## Usage

Create a `pipeline.prql` file in your pipeline directory:

```prql
from bronze.orders
filter status == "shipped"
group customer_id (
  aggregate {
    total_revenue = sum amount,
    order_count = count this,
  }
)
sort {-total_revenue}
take 100
```

Set the pipeline type in `config.yaml`:

```yaml
type: prql
merge_strategy: full_refresh
```

The runner compiles the PRQL → SQL, then executes through DuckDB.

## Build & test

```bash
cd plugins/rat-plugin-prql
pip install -e '.[dev]'
pytest
```

## See also

- [PRQL docs](https://prql-lang.org/book/)
- [`docs/PLUGIN_AUTHOR_GUIDE.md`](../../docs/PLUGIN_AUTHOR_GUIDE.md) — how to write a runner plugin
