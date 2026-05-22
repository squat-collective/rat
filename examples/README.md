# RAT Example Plugins

Example plugins for the RAT plugin system. The five **runner** plugins below are standalone, pip-installable Python packages covering all five Layer-1 extension points; [`rat-plugin-event-notifier`](./rat-plugin-event-notifier) and [`rat-plugin-interconnect`](./rat-plugin-interconnect) are Go platform plugins covering Layer 2 (platform / gRPC) and Layer 3 (portal UI). Copy any of them as a starting point for your own plugin.

## Extension Points

| Extension Point | Entry Point Group | Protocol | Example Plugin |
|---|---|---|---|
| **Merge Strategies** | `rat.strategies` | `MergeStrategyProtocol` | `rat-plugin-soft-delete` |
| **Hooks** | `rat.hooks` | `HookProtocol` | `rat-plugin-row-stats` |
| **Jinja Helpers** | `rat.jinja_helpers` | `JinjaHelperProtocol` | `rat-plugin-dbt-compat` |
| **Pipeline Types** | `rat.pipeline_types` | `PipelineTypeProtocol` | `rat-plugin-prql` |
| **Sources** | `rat.sources` | `SourceConnectorProtocol` | `rat-plugin-http-source` |

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

### `rat-plugin-prql` — PRQL Pipeline Type

Registers the `prql` pipeline type: a `pipeline.prql` file is compiled from [PRQL](https://prql-lang.org) to SQL and executed on DuckDB, exactly like a core `pipeline.sql` file. Demonstrates adding a whole new query language.

PRQL s-strings (`s"..."`) pass raw SQL through — that is how a pipeline reads landing-zone files:

```prql
from s"read_csv_auto('s3://rat/default/landing/orders/*.csv')"
filter amount > 100
derive {amount_eur = amount * 0.92}
```

### `rat-plugin-http-source` — HTTP/REST API Source

A `http` source connector that fetches JSON from any REST endpoint and returns it as an Arrow table. Stdlib-only. Demonstrates the `rat.sources` extension point.

> **Note:** the runner executor does not yet *invoke* source connectors — there is no pipeline-side mechanism to declare "use source X". This plugin is a protocol-complete example: discoverable and unit-tested, but not yet runnable as part of a pipeline.

### `rat-plugin-event-notifier` — Platform + Portal Plugin (L2 + L3)

A Go ConnectRPC **platform plugin**: it phones home to ratd's open registry, implements `HealthCheck` / `Describe` / `HandleEvent`, subscribes to platform events (`run_completed`, `quality_failed`), and exposes a proxied `/events` route. It also ships a **portal UI bundle** (dashboard widget + sidebar nav item), so it covers Layer 2 *and* Layer 3 in one container. See its [README](./rat-plugin-event-notifier/README.md) — note it is built and run differently from the runner plugins (Docker image, not a pip package).

### `rat-plugin-interconnect` — Plugin Interconnection (L2 + L3)

A *meta-plugin*: it makes plugin-to-plugin wiring first-class. Plugins register named **capabilities** they offer; any plugin then **invokes a capability by name** and the built-in **broker** routes the call to a healthy provider — no hardcoded plugin names or routes. The portal UI draws the live **plugin mesh** — every plugin, its health, and how capabilities wire them together — and lets you register and invoke capabilities interactively. Build-free portal bundle. See its [README](./rat-plugin-interconnect/README.md).

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
for group in ['rat.strategies', 'rat.hooks', 'rat.jinja_helpers', 'rat.pipeline_types', 'rat.sources']:
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

# prql
docker run --rm \
  -v $(pwd)/examples/rat-plugin-prql:/plugin \
  -v $(pwd)/runner:/runner \
  -w /plugin python:3.12-slim \
  sh -c "pip install -q uv && uv pip install --system -q -e /runner -e '.[dev]' && pytest -v tests/"

# http-source
docker run --rm \
  -v $(pwd)/examples/rat-plugin-http-source:/plugin \
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

# Pipeline Type — rat.pipeline_types
class MyPipelineType:
    @property
    def name(self) -> str: ...
    @property
    def file_extension(self) -> str: ...  # e.g. "prql" -> pipeline.prql
    def execute(self, source, namespace, layer, pipeline_name,
                s3_config, nessie_config, config) -> pa.Table: ...

# Source Connector — rat.sources
class MySource:
    @property
    def name(self) -> str: ...
    def fetch(self, config: dict, s3_config) -> pa.Table: ...
```
