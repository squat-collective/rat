# rat-plugin-soft-delete

A merge strategy that marks rows as deleted instead of removing them.
Use for audit trails, regulatory compliance, or any workflow where
"the row used to exist" is itself useful information.

Adds a `soft_delete` value to the `merge_strategy` config field, joining
the six built-in strategies (`full_refresh`, `incremental`, etc.).

## How it works

When the pipeline runs:

1. Loads the existing Iceberg table.
2. ANTI JOINs incoming rows vs existing on `unique_key` to find rows
   that are no longer present in the new data.
3. Marks those "missing" rows with `_deleted_at = current_timestamp`.
4. Writes back: incoming rows (`_deleted_at = NULL`) + newly soft-deleted
   rows + previously soft-deleted rows (preserved).

Without `unique_key` in the config, the strategy falls back to
`full_refresh` and logs a warning.

## Install

This is a **runner extension** (Python entry-point), not a standalone
container. Add it to the runner's dependencies:

```bash
# In runner/pyproject.toml, add to dependencies:
#   "rat-plugin-soft-delete"
# Or for development, mount this directory into /plugins of the runner.
```

The runner auto-discovers entry-points under `rat.strategies` on
startup; the new `soft_delete` strategy becomes available immediately.

## Usage

In your pipeline's `config.yaml`:

```yaml
merge_strategy: soft_delete
unique_key:
  - id
```

Or via SQL annotation:

```sql
-- @merge_strategy: soft_delete
-- @unique_key: id

SELECT * FROM {{ ref('bronze.users') }}
```

## Querying

The strategy adds a `_deleted_at` column to your table. To get only
currently-active rows:

```sql
SELECT * FROM silver.users WHERE _deleted_at IS NULL;
```

To audit deletions:

```sql
SELECT id, _deleted_at FROM silver.users
WHERE _deleted_at IS NOT NULL
ORDER BY _deleted_at DESC;
```

## Build & test

```bash
cd plugins/rat-plugin-soft-delete
pip install -e '.[dev]'
pytest
```
