# RAT Example Plugins

Example plugins demonstrating all three runner extension points. Each is a standalone, pip-installable Python package — copy one as a starting point for your own plugin.

## Extension Points

| Extension Point | Entry Point Group | Protocol | Example Plugin |
|---|---|---|---|
| **Merge Strategies** | `rat.strategies` | `MergeStrategyProtocol` | `rat-plugin-soft-delete` |
| **Hooks** | `rat.hooks` | `HookProtocol` | `rat-plugin-row-stats` |
| **Jinja Helpers** | `rat.jinja_helpers` | `JinjaHelperProtocol` | `rat-plugin-dbt-compat` |

## Plugins

### `rat-plugin-soft-delete` — Custom Merge Strategy

A `soft_delete` strategy that marks rows as deleted (`_deleted_at` timestamp) instead of physically removing them. Useful for audit trails and regulatory compliance.

- Requires `unique_key` in pipeline config
- Falls back to `full_refresh` without a unique key
- Preserves previously soft-deleted rows across runs

### `rat-plugin-row-stats` — Post-Write Hook

A `post_write` hook that logs row counts and schema summary after each Iceberg write. Demonstrates the hook lifecycle — fires at a specific phase boundary with access to run metadata via `HookContext`.

### `rat-plugin-dbt-compat` — dbt Jinja Helpers

Three dbt-compatible Jinja functions for SQL templates:

| Helper | Template Usage | Behavior |
|---|---|---|
| `env_var` | `{{ env_var('DB_HOST') }}` | Read environment variable, raise on missing (or use default) |
| `var` | `{{ var('batch_size') }}` | Placeholder returning key name (future: config vars) |
| `generate_schema_name` | `{{ generate_schema_name('custom') }}` | Returns argument as-is (RAT doesn't use schemas) |

## Quick Start

### Install a plugin (editable mode)

```bash
cd examples/rat-plugin-dbt-compat
pip install -e '.[dev]'
```

### Verify entry points are discoverable

```bash
python -c "
from importlib.metadata import entry_points
for group in ['rat.strategies', 'rat.hooks', 'rat.jinja_helpers']:
    eps = entry_points(group=group)
    for ep in eps:
        print(f'{group}: {ep.name} = {ep.value}')
"
```

### Run tests (in Docker)

```bash
# dbt-compat (simplest — no I/O dependencies)
docker run --rm \
  -v $(pwd)/examples/rat-plugin-dbt-compat:/plugin \
  -v $(pwd)/runner:/runner \
  -w /plugin python:3.12-slim \
  sh -c "pip install -q uv && uv pip install --system -q -e /runner -e '.[dev]' && pytest -v tests/"

# row-stats
docker run --rm \
  -v $(pwd)/examples/rat-plugin-row-stats:/plugin \
  -v $(pwd)/runner:/runner \
  -w /plugin python:3.12-slim \
  sh -c "pip install -q uv && uv pip install --system -q -e /runner -e '.[dev]' && pytest -v tests/"

# soft-delete
docker run --rm \
  -v $(pwd)/examples/rat-plugin-soft-delete:/plugin \
  -v $(pwd)/runner:/runner \
  -w /plugin python:3.12-slim \
  sh -c "pip install -q uv && uv pip install --system -q -e /runner -e '.[dev]' && pytest -v tests/"
```

## Writing Your Own Plugin

1. **Pick an extension point** from the table above
2. **Copy** the closest example plugin directory
3. **Implement** the protocol (see `rat_runner.plugin_protocols` for exact signatures)
4. **Register** your class in `pyproject.toml` under the right entry point group:
   ```toml
   [project.entry-points."rat.strategies"]
   my_strategy = "my_package.module:MyStrategyClass"
   ```
5. **Install** your plugin (`pip install -e .`) and RAT discovers it automatically
6. **Test** with `pytest` — mock Iceberg functions for unit tests

### Protocol Quick Reference

```python
# Merge Strategy — rat.strategies
class MyStrategy:
    @property
    def name(self) -> str: ...
    def execute(self, data, table_name, s3_config, nessie_config,
                location, config, branch="main", conn=None) -> int: ...

# Hook — rat.hooks
class MyHook:
    @property
    def phase(self) -> str: ...  # "pre_execute" | "post_write" | ...
    def __call__(self, context: HookContext) -> None: ...

# Jinja Helper — rat.jinja_helpers
class MyHelper:
    @property
    def name(self) -> str: ...  # function name in templates
    def __call__(self, *args, **kwargs) -> object: ...
```
