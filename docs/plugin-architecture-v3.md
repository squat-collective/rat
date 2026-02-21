# RAT Plugin Architecture — v3 Community Extensibility

> **Status**: Draft — working document for plugin system design
> **Date**: 2026-02-21
> **Branch**: `feat/plugin-system-v3` (from `main`)
> **Remove this file before release**

---

## Vision

RAT Community Edition becomes fully extensible via a three-layer plugin system.
Plugins are code artifacts (Docker images, Python packages, JS bundles) managed through
a catalog stored in Postgres. They can be discovered, connected, enabled, disabled, and
removed at runtime — no restart, no config file editing.

---

## Decisions Log

| # | Decision | Rationale |
|---|----------|-----------|
| 1 | Branch from `main`, abandon `feat/permission-engine-v2` | Main has plugin slot infrastructure we need. Permission engine becomes a community plugin. |
| 2 | **No Docker socket** — ratd connects only, never manages containers | Preserves ADR-007. Users manage containers via compose/K8s. Hot-connect via API. |
| 3 | **Fully open plugin registry** — migrate away from 7 hardcoded slots | All plugins (auth, executor, etc.) go through the same dynamic `map[string]*Plugin`. |
| 4 | **Python entry points** for runner plugins | Standard packaging (pip install, pyproject.toml). Proper deps, versioning, testing. |
| 5 | **Generic plugin proto** in core — plugins own specific protos | Core has `plugin/v1/plugin.proto` (HealthCheck + Describe). No plugin-specific protos in core. |
| 6 | **URL-only** portal plugins with externals pattern | All plugins (community + pro) use URL bundles. Portal loads via `import(url)`. Plugin SDK handles build. |
| 7 | **Phone-home** discovery — plugins self-register with ratd on startup | Plugin calls `POST /internal/plugins/register` on ratd. Zero config on ratd side. |
| 8 | **Remove** existing plugin-specific protos from core repo | Clean break. auth/v1, sharing/v1, enforcement/v1, cloud/v1, identity/v1 move to pro plugin repos. |

---

## 1. Three-Layer Plugin System

```
┌─────────────────────────────────────────────────────────────────┐
│  Layer 3: Portal Plugins (UI)          — JS bundles (URL)        │
│  ───────────────────────────────────────────────────────────────│
│  Layer 2: Platform Plugins (API/Logic) — gRPC containers         │
│  ───────────────────────────────────────────────────────────────│
│  Layer 1: Runner Plugins (Data)        — Python entry points     │
└─────────────────────────────────────────────────────────────────┘
```

---

## 2. Layer 1: Runner Plugins (Data Layer)

**Mechanism**: Python entry points (setuptools) discovered per-run.

### Extension Points

**A. Merge Strategies** — Protocol-based registry replacing enum dispatch:

```python
class MergeStrategyProtocol(Protocol):
    name: str
    requires_unique_key: bool
    requires_partition_column: bool

    def execute(
        self,
        catalog: RestCatalog,
        table_id: str,
        data: pa.Table,
        config: PipelineConfig,
        branch: str,
    ) -> int:  # rows written
        ...
```

**B. Pipeline Types** — Extensible type detection + execution:

```python
class PipelineTypeProtocol(Protocol):
    name: str
    file_extension: str

    def detect(self, source_files: list[str]) -> bool: ...
    def execute(self, source: str, engine: DuckDBEngine, ...) -> pa.Table: ...
```

**C. Jinja Helpers** — Custom template functions:

```python
class JinjaHelperProtocol(Protocol):
    name: str
    def __call__(self, *args, **kwargs) -> str: ...
```

**D. Execution Hooks** — Run at phase boundaries:

```python
class HookProtocol(Protocol):
    name: str
    phase: Literal["pre_detect", "post_detect", "pre_compile", "post_compile",
                    "pre_execute", "post_execute", "pre_write", "post_write",
                    "pre_quality", "post_quality", "pre_merge", "post_merge",
                    "on_success", "on_failure"]
    def run(self, ctx: HookContext) -> None: ...
```

**E. Data Source Connectors** — Non-S3 ingestion:

```python
class SourceConnectorProtocol(Protocol):
    name: str
    config_schema: dict
    def fetch(self, config: dict, logger: PipelineLogger) -> pa.Table: ...
```

### Plugin Discovery via Entry Points

```toml
# Community plugin's pyproject.toml:
[project.entry-points."rat.strategies"]
custom_merge = "rat_plugin_custom:CustomMergeStrategy"

[project.entry-points."rat.pipeline_types"]
r_lang = "rat_plugin_r:RLangPipelineType"

[project.entry-points."rat.jinja_helpers"]
hash_column = "rat_plugin_helpers:hash_column_fn"

[project.entry-points."rat.hooks"]
slack_notify = "rat_plugin_slack:SlackNotifyHook"

[project.entry-points."rat.sources"]
kafka = "rat_plugin_kafka:KafkaSource"
```

### Per-Run Discovery

Plugins are discovered at the start of each pipeline run (not server startup).
Each run is a thread with its own context — naturally isolated, consistent plugin set.

---

## 3. Layer 2: Platform Plugins (API/Logic Layer)

**Mechanism**: gRPC sidecar containers with open, dynamic registry.

### Generic Plugin Proto (core owns this only)

```protobuf
// proto/plugin/v1/plugin.proto — the ONLY plugin proto in core
service PluginService {
  rpc HealthCheck(HealthCheckRequest) returns (HealthCheckResponse);
  rpc Describe(DescribeRequest) returns (DescribeResponse);
  rpc HandleEvent(HandleEventRequest) returns (HandleEventResponse);
}

message DescribeResponse {
  string name = 1;
  string version = 2;
  string description = 3;
  repeated RouteDeclaration routes = 4;
  repeated string event_subscriptions = 5;
  bool provides_worker = 6;
  PluginUIDescriptor ui = 7;
  string config_schema_json = 8;
}

message RouteDeclaration {
  string method = 1;
  string path = 2;
}

message PluginUIDescriptor {
  string bundle_url = 1;
  repeated UISlotDeclaration slots = 2;
  repeated UINavItem nav_items = 3;
  repeated UIRoute routes = 4;
}

message HandleEventRequest {
  string event_type = 1;  // "run.completed", "pipeline.published", etc.
  bytes payload = 2;      // JSON-encoded event data
}
```

### Phone-Home Registration

Plugins self-register with ratd on startup:

```
1. Plugin container starts
2. Plugin calls POST http://ratd:8080/internal/plugins/register
   Body: { "name": "monitoring", "addr": "monitoring:50070" }
3. ratd calls plugin's Describe RPC
4. ratd registers routes, events, UI metadata in catalog
5. ratd marks plugin as enabled
```

Plugin needs one env var: `RATD_URL=http://ratd:8080`

### Route Proxying

Single catch-all route handles all plugin traffic dynamically:

```go
r.HandleFunc("/api/v1/x/{plugin}/*", srv.handlePluginProxy)
```

No route tree rebuild needed. One static route, dynamic dispatch.

### Event Bus

```go
const (
    EventRunCompleted      = "run.completed"
    EventRunFailed         = "run.failed"
    EventRunCancelled      = "run.cancelled"
    EventPipelinePublished = "pipeline.published"
    EventPipelineCreated   = "pipeline.created"
    EventPipelineDeleted   = "pipeline.deleted"
    EventFileUploaded      = "file.uploaded"
    EventQualityFailed     = "quality.failed"
    EventScheduleFired     = "schedule.fired"
)
```

Events dispatched to subscribed plugins via `HandleEvent` RPC (async, best-effort).

---

## 4. Layer 3: Portal Plugins (UI Layer)

**Mechanism**: URL-loaded JS bundles with React/ReactDOM as externals.

### How It Works

1. Portal calls `GET /api/v1/plugins?status=enabled` on page load
2. For each plugin with UI metadata, loads bundle from `bundle_url`
3. Bundles are built with React as external (portal exposes `window.React`)
4. Each bundle exports `loadRegistry()` → returns slots, routes, navItems
5. Portal merges all registries and renders

### Expanded Slot Catalog (20+ slots)

**Layout**: `main-header`, `sidebar-nav-extra`, `sidebar-user`, `sidebar-footer`, `global-banner`
**Dashboard**: `dashboard-stats`, `dashboard-widgets`, `dashboard-actions`
**Pipeline**: `pipeline-detail-tabs`, `pipeline-detail-header`, `pipeline-editor-toolbar`, `pipeline-actions`
**Run**: `run-detail-header`, `run-detail-tabs`, `run-actions`, `run-log-filters`
**Query**: `query-toolbar`, `query-results-tabs`
**Explorer**: `table-detail-tabs`, `table-actions`
**Settings**: `settings-sections`, `settings-nav`

### Plugin Registry Type

```typescript
export type PluginRegistry = {
  slots: Record<string, SlotComponent<any>[]>;
  routes: PluginRoute[];
  navItems: PluginNavItem[];
};

interface PluginRoute {
  path: string;
  component: React.ComponentType;
  title: string;
}

interface PluginNavItem {
  label: string;
  href: string;
  icon: string;
  position: "main" | "bottom";
}
```

### Plugin SDK (Build Tooling)

Plugin authors run `rat plugin build` which produces a correctly-configured bundle
with React/ReactDOM as externals. Like Grafana's `@grafana/create-plugin`.

---

## 5. Plugin Catalog (Postgres)

Source of truth for all connected plugins.

```sql
-- Migration: 016_plugin_catalog.sql

CREATE TABLE plugin_catalog (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL UNIQUE,
    kind        TEXT NOT NULL,                -- "platform", "runner", "portal"
    version     TEXT,
    status      TEXT NOT NULL DEFAULT 'registered', -- "registered", "enabled", "disabled", "error"
    error       TEXT,
    descriptor  JSONB,                        -- from Describe RPC
    config      JSONB DEFAULT '{}',           -- user-provided config
    addr        TEXT,                         -- gRPC address (platform plugins)
    healthy     BOOLEAN DEFAULT false,
    registered_at TIMESTAMPTZ DEFAULT now(),
    enabled_at    TIMESTAMPTZ,
    updated_at    TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE plugin_sources (
    id      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    type    TEXT NOT NULL,                    -- "oci_registry", "github", "url"
    url     TEXT NOT NULL,
    trusted BOOLEAN DEFAULT false,
    enabled BOOLEAN DEFAULT true
);

CREATE TABLE plugin_policies (
    id      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    rule    TEXT NOT NULL,                    -- "allow", "deny"
    pattern TEXT NOT NULL,
    kind    TEXT                              -- NULL = all, or "platform", "runner", "portal"
);
```

---

## 6. Plugin Descriptor (rat-plugin.json)

Shipped with every plugin. Read by Describe RPC or directly from artifact.

```json
{
  "name": "monitoring",
  "version": "1.2.0",
  "description": "Pipeline execution monitoring & alerting",
  "kind": "platform",
  "author": "community",
  "license": "MIT",
  "rat_version": ">=2.4.0",

  "config_schema": {
    "type": "object",
    "properties": {
      "retention_days": { "type": "integer", "default": 30 },
      "alert_webhook": { "type": "string", "format": "uri" }
    }
  },

  "capabilities": {
    "routes": [
      { "method": "GET", "path": "/dashboard" },
      { "method": "GET", "path": "/metrics/:ns/:layer/:name" }
    ],
    "events": ["run.completed", "run.failed", "quality.failed"],
    "worker": true
  },

  "ui": {
    "slots": {
      "pipeline-detail-tabs": { "label": "Monitoring", "component": "MonitoringTab" },
      "dashboard-widgets": { "component": "MetricsWidget" }
    },
    "nav_items": [
      { "label": "Monitoring", "href": "/x/monitoring", "icon": "activity", "position": "main" }
    ],
    "routes": [
      { "path": "/x/monitoring", "component": "MonitoringPage" }
    ],
    "bundle_url": "/api/v1/plugins/monitoring/ui/bundle.js"
  }
}
```

---

## 7. Hot-Connect Flow

### Platform Plugins

```
1. User starts plugin container (compose, docker run, k8s)
2. Plugin calls POST /internal/plugins/register on ratd (phone-home)
3. ratd calls Describe RPC → gets capabilities
4. ratd registers routes (proxied under /api/v1/x/{name}/*)
5. ratd subscribes plugin to declared events
6. ratd stores in plugin_catalog, marks enabled
7. Portal picks up new plugin on next /api/v1/plugins fetch
```

### Runner Plugins

```
1. User installs Python package in runner container (pip install)
2. Next pipeline run discovers new entry points automatically
3. New strategies/types/hooks/helpers available immediately
```

### Portal Plugins

```
1. Platform plugin with UI section registers with ratd
2. Portal fetches /api/v1/plugins, sees new plugin with bundle_url
3. Portal loads bundle, merges registry
4. New slots/routes/nav items appear in UI
```

### Disable/Remove

```
PUT  /api/v1/plugins/{name}/disable  → ratd disconnects, stops proxying
DELETE /api/v1/plugins/{name}        → ratd removes from catalog
```

Plugin container stays running — user stops it themselves.

---

## 8. Plugin Manager API

```
GET    /api/v1/plugins                          # List all plugins
GET    /api/v1/plugins/{name}                   # Plugin details + descriptor
PUT    /api/v1/plugins/{name}/enable             # Enable (reconnect)
PUT    /api/v1/plugins/{name}/disable            # Disable (disconnect)
PUT    /api/v1/plugins/{name}/config             # Update user config
DELETE /api/v1/plugins/{name}                    # Remove from catalog

# Internal (plugin phone-home)
POST   /internal/plugins/register               # Plugin self-registers

# Sources & Policies
GET    /api/v1/plugin-sources                    # List allowed sources
POST   /api/v1/plugin-sources                    # Add source
DELETE /api/v1/plugin-sources/{id}               # Remove source
GET    /api/v1/plugin-policies                   # List allow/deny rules
POST   /api/v1/plugin-policies                   # Add rule
DELETE /api/v1/plugin-policies/{id}              # Remove rule
```

---

## 9. Security & Policies

### Source Allowlisting
Only allow plugins from configured sources. Anything not listed = blocked.

### Plugin Policies (Allow/Deny Rules)
```json
{ "rule": "allow", "pattern": "monitoring" }
{ "rule": "deny",  "pattern": "untrusted-*" }
```

### Health Monitoring
ratd periodically health-checks all enabled plugins.
Unhealthy plugins are marked as `status: "error"` but not disconnected.
After N consecutive failures → auto-disabled with error message.

---

## 10. What Gets Removed from Core

### Proto files to delete:
- `proto/auth/v1/auth.proto`
- `proto/sharing/v1/sharing.proto`
- `proto/enforcement/v1/enforcement.proto`
- `proto/cloud/v1/cloud.proto`
- `proto/identity/v1/identity.proto`
- `proto/permission/v1/permission.proto`
- `proto/executor/v1/executor.proto`

### Keep:
- `proto/plugin/v1/plugin.proto` (enhanced with Describe + HandleEvent)
- `proto/runner/v1/runner.proto` (core service)
- `proto/query/v1/query.proto` (core service)
- `proto/common/v1/common.proto` (shared types)

### Generated code to delete:
- `platform/gen/auth/`, `platform/gen/sharing/`, `platform/gen/enforcement/`
- `platform/gen/cloud/`, `platform/gen/identity/`, `platform/gen/permission/`
- `platform/gen/executor/`
- Corresponding Python gen dirs in runner/ and query/

### Platform code to refactor:
- `internal/plugins/loader.go` → open registry (remove 7 typed fields + switch)
- `internal/plugins/auth_middleware.go` → generic plugin middleware
- `internal/plugins/authorizer.go` → remove (enforcement plugin responsibility)
- `internal/plugins/context.go` → keep (user context still useful)
- `internal/executor/plugin.go` → remove (executor becomes a plugin like any other)
- `cmd/ratd/main.go` → wire new PluginManager instead of typed plugin loading

---

## 11. rat.yaml — Bootstrap/Seed Role

rat.yaml seeds the catalog on first boot. After that, Postgres is the source of truth.

```yaml
edition: community

# Plugins auto-connect on startup (phone-home addresses ratd discovers)
# This section is optional — plugins can self-register without it
plugin_sources:
  - ghcr.io/squat-collective
  - ghcr.io/rat-community

runner_plugins:
  - rat-plugin-kafka>=1.0
```

---

## 12. Implementation Phases

### Phase 1: Core Plugin Infrastructure
- Postgres migration (plugin_catalog, plugin_sources, plugin_policies)
- Enhance `plugin.proto` (Describe, HandleEvent RPCs)
- Refactor `internal/plugins/` → open registry
- Plugin Manager service (register, enable, disable, remove)
- Plugin REST API endpoints
- Phone-home registration endpoint (`/internal/plugins/register`)
- Route proxy (`/api/v1/x/{plugin}/*`)
- Event dispatcher (subscribe + HandleEvent dispatch)
- Remove old typed plugin protos + generated code
- Health-check loop for enabled plugins

### Phase 2: Runner Plugin System
- `PluginRegistry` class with entry point discovery
- Protocol definitions (strategies, types, hooks, helpers, sources)
- Refactor built-in strategies to use protocols (dogfooding)
- Per-run plugin discovery in executor
- Hook dispatch at phase boundaries

### Phase 3: Portal Multi-Plugin Loading
- Replace NEXT_PUBLIC_PLUGIN_PACKAGE with URL-based multi-plugin loading
- Expose React/ReactDOM as window globals
- Fetch plugin manifest from /api/v1/plugins
- Dynamic bundle loading + registry merging
- Expanded PluginSlot catalog (20+ slots across all pages)
- Plugin route registration + catch-all /x/[...path] page
- Plugin nav item injection in sidebar

### Phase 4: Plugin Management UI
- `/settings/plugins` page (list, configure, enable/disable)
- Auto-generated config forms from config_schema
- Plugin health status display
- Source & policy management UI

### Phase 5: Developer Experience
- Plugin SDK (Go for platform, Python for runner, TS for portal)
- `rat plugin init` scaffold CLI
- `rat plugin build` (portal bundle builder with externals)
- Example plugins (monitoring, slack-alerts, custom-merge)
- Documentation

---

## References

- **Grafana**: subprocess + gRPC backend, SystemJS frontend, plugin.json manifest, Plugin Catalog
- **Vault/go-plugin**: subprocess + gRPC + mTLS, SHA256 verification, hot-reload via API, lazy spawn
- **VS Code**: Extension Host process, lazy activation events, contributes manifest (static declaration)
- **Kong**: Lua plugins, phase-based hooks, kong reload, schema.lua for config forms
- **n8n**: npm community nodes, LazyPackageDirectoryLoader, INodeType interface
- **Home Assistant/HACS**: GitHub as registry, manifest.json, requires restart
