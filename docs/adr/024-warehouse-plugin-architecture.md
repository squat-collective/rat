# ADR-024: Warehouse plugin architecture

## Status: Proposed (under review 2026-05-29)

## Context

RAT's **runner** side is already pluggable: a Python package registers
extensions via five `entry_points` groups (`rat.pipeline_types`, `rat.strategies`,
`rat.sources`, `rat.hooks`, `rat.jinja_helpers`) and the runner discovers them at
load time. The soft-delete / prql / dbt-compat plugins prove the seam works.

The **warehouse** is hardcoded. `runner/src/rat_runner/iceberg.py` and `nessie.py`
bake in exactly one stack — Apache Iceberg tables, S3/MinIO storage, a Nessie REST
catalog — and every consumer that touches table state reaches for it directly.
Those consumers span **three languages/runtimes**, which is the crux:

| Consumer | Runtime | Today |
|---|---|---|
| runner (write/execute) | Python | imports `iceberg.py`/`nessie.py` |
| ratq (query/discovery) | Python | imports the same |
| `rat-plugin-diff`, `rat-plugin-docs-assistant`, `rat-plugin-pg-sync` | **Go** | hit Nessie/S3 directly |
| portal (history/diff/browse UI) | TS | via ratd REST |

So the warehouse abstraction cannot be a Python-only `Protocol` — a Go plugin
can't implement or call one. The contract has to be **cross-language**.

> Rejected framings (don't re-litigate): "RAT-as-platform/VSCode model"
> (over-scoped — only the warehouse is missing); "merge strategies are
> Iceberg-coupled in core" (wrong — they're already `rat.strategies` plugins);
> the two-seam DataPlane/ComputeEngine model (replaced by runner + warehouse).

## Decision

Make the warehouse a **first-class platform plugin type** with a **wire contract
defined in protobuf (`warehouse/v1`)** and served over ConnectRPC — the same
transport ratd already uses for runner/query/plugins. This completes the "v3"
plugin vision (it is the completion, not a separate v4).

### 1. `warehouse/v1` is the contract; the warehouse is a plugin

A warehouse plugin implements the `WarehouseService` ConnectRPC service and
registers with ratd via the existing plugin phone-home mechanism (`rat.yaml`
selects the active warehouse). ratd hosts/routes it, so **every consumer — Go
plugins, ratq, runner, portal — calls one cross-language abstraction** instead of
reaching into Nessie/S3.

`proto/warehouse/v1/warehouse.proto` (package `ratatouille.warehouse.v1`),
service shape:

```
service WarehouseService {
  // — required surface (every warehouse) —
  rpc Describe(...)        // name + capability set
  rpc ListNamespaces(...)
  rpc ListTables(...)
  rpc GetSchema(TableRef)  // Arrow schema (IPC)
  rpc Attach(...)          // returns an opaque AttachDescriptor (see §4)
  rpc Write(stream ...)    // Arrow IPC in → rows-written out (see §2)

  // — capability-gated groups (see §3) —
  rpc GetHistory(...)                          // TIME_TRAVEL
  rpc CreateBranch/MergeBranch/ListBranches(…) // BRANCHING
  rpc RowDiff(...)                             // ROW_DIFF
}
```

### 2. Fully uniform: every op goes through the service, data as Arrow IPC

Including writes. Payloads (`Write`, `GetSchema`, `RowDiff` results) move as
**Arrow IPC**, exactly as ratd↔runner/ratq already stream Arrow today, so this
isn't a new transport concern. The runner's write/attach path stops calling
`iceberg.py` in-process and instead calls `WarehouseService`. Uniform and
language-agnostic by construction; the cost is Arrow-over-the-wire on the write
path, accepted as the price of one abstraction for all consumers.

### 3. Capability model — generic, but typed contracts (no fat interface)

A single **capability enum** is the source of truth:
`{BRANCHING, TIME_TRAVEL, ROW_DIFF, SCD2_NATIVE, PARTITION_EVOLUTION, …}`.

- **Required surface** (above) — every warehouse implements it.
- **Optional surface** — one discrete, capability-keyed RPC group per capability
  (`BRANCHING` → branch RPCs, `ROW_DIFF` → `RowDiff`, …). A warehouse advertises
  what it supports in `Describe`; calling an unadvertised RPC returns
  `CodeUnimplemented`. **Consumers always gate on the negotiated capability set**
  before calling, so a missing capability degrades the feature (e.g. the diff UI
  hides row-diff) rather than erroring.
- Adding a future capability = enum entry + one optional RPC group (+ one Python
  optional Protocol, §6). Generic and extensible; each contract stays small.

### 4. `Attach` returns an opaque descriptor (backend-agnostic)

No DuckDB in the contract. `Attach` returns an `AttachDescriptor` (catalog URI,
credentials/handles, format hint). A DuckDB-backed runner turns that into an
`ATTACH`; a future non-DuckDB engine consumes it however it needs. The runner
stays warehouse-agnostic and the warehouse stays runner-agnostic.

### 5. Strategy compatibility dispatch

Strategy **name** (`scd2`, `snapshot`, …) stays universal; the **implementation**
declares which warehouses it supports (`warehouses=["iceberg-*"]`). Dispatch
resolves the (warehouse, strategy) pair to a compatible impl. Drop the
`_iceberg` suffix from the internal strategy functions — it was leaking the
implementation into the universal name.

### 6. Plugin-author ergonomics (Python `WarehouseProtocol`)

Authors shouldn't hand-write ConnectRPC. The runner SDK ships a Python
`WarehouseProtocol` (the impl-contract) plus a thin adapter that serves it as
`WarehouseService`; optional capabilities are separate `@runtime_checkable`
Protocols (`BranchingWarehouse`, `TimeTravelWarehouse`, `RowDiffWarehouse`) the
adapter maps to the optional RPC groups and reflects in `Describe`. Go warehouse
authors implement the ConnectRPC service directly. (PR #35's `WarehouseProtocol`
is this piece — it will be reconciled to the proto: opaque attach descriptor,
split capability Protocols.)

### 7. Postgres model, not VSCode model

Opinionated core + extension points + a **curated default bundle** that ships
preinstalled so "one-line deploy, data in 5 minutes" holds: the **`iceberg-nessie`**
warehouse (today's stack, refactored into the reference plugin), `sql`/`python`
runners, the six built-in strategies, core plugins (diff, docs-assistant, …).
Every piece replaceable; nothing required-assembly.

## Migration sequence (re-sliced for the cross-language design)

1. **Contracts (slice 1):** `warehouse/v1` proto (required surface + capability
   enum + optional groups) with codegen wired (Go + Python), and the Python
   `WarehouseProtocol` + optional capability Protocols as the author SDK.
   **No behavior change** — nothing calls it yet.
2. **Reference impl:** refactor Iceberg+Nessie into the `iceberg-nessie`
   warehouse plugin serving `WarehouseService`; ratd selects/hosts it via
   `rat.yaml`; switch the runner write/attach path onto it. **Behavior-preserving**,
   validated by the existing runner/query/integration suites.
3. **Strategy compat:** add `warehouses=[...]` tags to `rat.strategies` plugins;
   drop the `_iceberg` suffix.
4. **Migrate consumers:** `ratq`, `diff`, `docs-assistant`, `pg-sync`, portal
   call `warehouse/v1` (via ratd) instead of Nessie/S3 directly.
5. **Validate:** Unity Catalog (easy — still Iceberg) before DuckLake (different
   format), proving the seam holds across catalogs then formats.

## Consequences

- **Symmetric, cross-language architecture:** storage joins compute as a
  pluggable, language-agnostic seam. New catalogs/formats are plugins, not core
  edits, usable from Go and Python alike.
- **Bigger blast radius than a Python seam:** a new proto package, ratd hosting/
  routing, and the runner write-path rework. Mitigated by the behavior-preserving
  slice 2 and the existing Arrow-over-gRPC precedent.
- **Write-path wire cost:** writes now stream Arrow through `WarehouseService`.
  Acceptable for the uniformity; revisit with a local fast-path only if profiling
  demands it.
- **Capability negotiation is explicit:** features become opt-in per warehouse;
  consumer UIs must already handle "capability absent" (the diff/history UIs
  already guard on Nessie availability).
- Supersedes the stale memory note pinning this to "ADR-018" — that's
  cloud-credential-vending; the warehouse ADR is **024**.

## References

- Existing ConnectRPC services + Arrow-over-gRPC: `proto/{runner,query}/v1`,
  `platform/internal/arrowutil`, `query/.../arrow_ipc.py`
- Runner plugin seams: `runner/pyproject.toml` `rat.*` entry points,
  `runner/src/rat_runner/plugin_protocols.py`, `plugin_registry.py`
- Reference impl to wrap: `runner/src/rat_runner/iceberg.py`, `nessie.py`
- Plugin model: [.claude/rules/plugins.md], plugin phone-home / ratd hosting
- Trust model: [ADR-017](017-python-pipeline-trust-model.md)
