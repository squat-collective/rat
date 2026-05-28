# RAT Example Plugins

Example plugins for the RAT plugin system. The five **runner** plugins below are standalone, pip-installable Python packages covering all five Layer-1 extension points. The remaining plugins are **Go platform plugins** covering Layer 2 (platform / gRPC) and Layer 3 (portal UI): [`rat-plugin-event-notifier`](./rat-plugin-event-notifier), [`rat-plugin-interconnect`](./rat-plugin-interconnect), [`rat-plugin-ai-provider`](./rat-plugin-ai-provider), [`rat-plugin-dev-assistant`](./rat-plugin-dev-assistant), [`rat-plugin-docs-assistant`](./rat-plugin-docs-assistant), [`rat-plugin-demo-loader`](./rat-plugin-demo-loader), [`rat-plugin-agents`](./rat-plugin-agents), [`rat-plugin-chat`](./rat-plugin-chat), [`rat-plugin-compaction`](./rat-plugin-compaction), [`rat-plugin-diff`](./rat-plugin-diff), [`rat-plugin-lineage`](./rat-plugin-lineage), [`rat-plugin-mcp-docs`](./rat-plugin-mcp-docs), [`rat-plugin-mcp-sql`](./rat-plugin-mcp-sql), [`rat-plugin-pg-sync`](./rat-plugin-pg-sync) and [`rat-plugin-secrets`](./rat-plugin-secrets). Copy any of them as a starting point for your own plugin. Every plugin here is free and open-source.

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

### `rat-plugin-ai-provider` — Configurable AI Provider (L2 + L3)

A reusable, **configurable AI provider** — a backend LLM service other AI extensions reuse (no LLM code or keys of their own). It wraps any OpenAI-compatible API and exposes `POST /complete` and `POST /chat`. It is **the first RAT plugin to use plugin configuration**: it declares a `config_schema_json`, the portal renders a settings form from it, and the plugin **polls ratd for its own config** (RAT stores config but does not push it) — so changes apply live. It also registers `ai.complete` / `ai.chat` capabilities with the interconnect plugin. See its [README](./rat-plugin-ai-provider/README.md).

### `rat-plugin-dev-assistant` — AI Dev Assistant (L2 + L3)

An **AI dev assistant** docked into the pipeline editor (via the core `pipeline-editor-sidebar` slot). It chats, explains, fixes, and **writes pipeline code from a described goal** — the reply's code block applies straight into the editor. A *thin consumer plugin*: no LLM code of its own — it brokers `ai.chat` to `rat-plugin-ai-provider` through the interconnect broker, with your current file and an optional data sample as context. The payoff of the provider + broker architecture. See its [README](./rat-plugin-dev-assistant/README.md).

### `rat-plugin-docs-assistant` — AI Docs Assistant (L2 + L3)

An **AI documentation writer for datasets**. Adds a *🤖 Suggest docs* button to a table-detail page (via the existing `table-actions` slot) — opens a modal of AI-generated **table description** and **per-column descriptions** you can edit and save through the core table-metadata API. Another *thin consumer plugin* — no LLM code of its own; brokers `ai.chat` to `rat-plugin-ai-provider` and asks for strict JSON, grounding the suggestions in the table's columns and a small data sample. See its [README](./rat-plugin-docs-assistant/README.md).

### `rat-plugin-demo-loader` — One-Click Sample Demos (L2 + L3)

A **one-click sample-data installer**. Ships three self-contained demos (🚀 Cosmos / 🎤 Underground / 🛒 Shop), each a full bronze → silver → gold pipeline with quality tests and synthetic data via `generate_series`. Installing one creates the namespace, all pipelines, writes their SQL, creates the quality tests and triggers the bronze runs — all through ratd's HTTP API. Adds a "Demos" sidebar entry. See its [README](./rat-plugin-demo-loader/README.md).

### `rat-plugin-secrets` — Encrypted Secrets Vault (L2 + L3)

An **AES-256-GCM encrypted vault**. Other plugins consume secrets by name via the interconnect capability `secrets.get`, so credentials never live in the calling plugin's config. See its [README](./rat-plugin-secrets/README.md).

### `rat-plugin-pg-sync` — Postgres → Iceberg Sync (L2 + L3)

Syncs an external Postgres database into Iceberg via auto-generated SQL pipelines (snapshot + incremental modes). Consumes DB credentials from the secrets plugin via the broker. See its [README](./rat-plugin-pg-sync/README.md).

### `rat-plugin-diff` — Activity Feed + Row-Level Diff (L2 + L3)

A live activity feed (15s poll of Nessie commits) plus a Nessie-backed row-level diff drill-in for any Iceberg table. See its [README](./rat-plugin-diff/README.md).

### `rat-plugin-compaction` — Iceberg Small-File Compactor (L2 + L3)

Detects Iceberg tables with too many small data files and rewrites them (hybrid Go + Python). See its [README](./rat-plugin-compaction/README.md).

### `rat-plugin-lineage` — Lineage Graph (L2 + L3)

A platform plugin that surfaces a lineage view of pipelines, tables, and landing zones. See its [README](./rat-plugin-lineage/README.md).

### `rat-plugin-agents` — AI Agents (L2 + L3)

An AI-agents plugin (seeds default agents on first run with an empty catalog). See its [README](./rat-plugin-agents/README.md).

### `rat-plugin-chat` — Data-Aware Chat (L2 + L3)

A conversational chat UI in the portal — a data-aware assistant with conversations persisted in ratd's plugin-config. See its [README](./rat-plugin-chat/README.md).

### `rat-plugin-mcp-sql` — MCP SQL Server (L2 + L3)

Exposes RAT's query surface over the Model Context Protocol so external MCP clients can run SQL. See its [README](./rat-plugin-mcp-sql/README.md).

### `rat-plugin-mcp-docs` — MCP Docs Server (L2 + L3)

Exposes RAT's docs/metadata over the Model Context Protocol for MCP clients. See its [README](./rat-plugin-mcp-docs/README.md).

## Quick Start

### Install a plugin (editable mode)

```bash
cd plugins/rat-plugin-dbt-compat
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
  -v $(pwd)/plugins/rat-plugin-dbt-compat:/plugin \
  -v $(pwd)/runner:/runner \
  -w /plugin python:3.12-slim \
  sh -c "pip install -q uv && uv pip install --system -q -e /runner -e '.[dev]' && pytest -v tests/"

# row-stats
docker run --rm \
  -v $(pwd)/plugins/rat-plugin-row-stats:/plugin \
  -v $(pwd)/runner:/runner \
  -w /plugin python:3.12-slim \
  sh -c "pip install -q uv && uv pip install --system -q -e /runner -e '.[dev]' && pytest -v tests/"

# soft-delete
docker run --rm \
  -v $(pwd)/plugins/rat-plugin-soft-delete:/plugin \
  -v $(pwd)/runner:/runner \
  -w /plugin python:3.12-slim \
  sh -c "pip install -q uv && uv pip install --system -q -e /runner -e '.[dev]' && pytest -v tests/"

# prql
docker run --rm \
  -v $(pwd)/plugins/rat-plugin-prql:/plugin \
  -v $(pwd)/runner:/runner \
  -w /plugin python:3.12-slim \
  sh -c "pip install -q uv && uv pip install --system -q -e /runner -e '.[dev]' && pytest -v tests/"

# http-source
docker run --rm \
  -v $(pwd)/plugins/rat-plugin-http-source:/plugin \
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
