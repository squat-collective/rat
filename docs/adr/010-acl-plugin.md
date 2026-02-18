# ADR-010: ACL Plugin — Ownership + Sharing + Enforcement (v2.7)

## Status: Accepted

## Context

The community edition is single-user — no access control needed. The Pro edition
needs multi-user ownership and sharing:

- **Ownership**: every pipeline/namespace has an owner (the creator). Transferable.
- **Sharing**: owners grant per-object access (`SHARE gold.revenue WITH bob READ`)
- **Enforcement**: API-level checks — ratd asks "can this user do this?" before mutations

The plugin system (ADR-007) defined extension points for `sharing` and `enforcement`
plugins. The proto definitions (`sharing/v1/sharing.proto`, `enforcement/v1/enforcement.proto`)
were already shipped with the platform. This ADR implements both services in a single
Pro container plugin.

## Decision

### Single `acl` plugin container

Both `SharingService` and `EnforcementService` are implemented in one Go binary.
ConnectRPC routes by service path, so both services run on the same port (50080).

```
ratd (Go)                           acl plugin (Pro)
+----------------------+            +-------------------------+
| Authorizer interface |            | SharingService          |
|  CanAccess()         |--ConnectRPC|  ShareResource()        |
|  requireAccess()     |            |  RevokeAccess()         |
|                      |            |  ListAccess()           |
| SharingRoutes        |            |  TransferOwnership()    |
|  POST /sharing       |--ConnectRPC|                         |
|  GET  /sharing       |            | EnforcementService      |
|  DELETE /sharing/:id |            |  CanAccess()            |
|  POST /transfer      |            |                         |
|                      |            | PluginService           |
| Pipeline handlers    |            |  HealthCheck()          |
|  create -> set owner |            +-------------------------+
|  update -> check     |            SQLite: access_grants table
|  delete -> check     |
+----------------------+
```

### Access Resolution Order

1. No user in context (community) -> **allow** (NoopAuthorizer)
2. Owner of resource -> **allow** (full access, checked locally in ratd)
3. Sharing grant exists -> **allow** at granted level (checked via enforcement plugin)
4. Default -> **deny**

Ownership check happens in ratd (it owns the Postgres `owner` column). The ACL
plugin handles sharing grants only.

### Authorizer abstraction in community repo

```go
type Authorizer interface {
    CanAccess(ctx context.Context, userID, resourceType, resourceID, action string) (bool, error)
}
```

- `NoopAuthorizer` (community): always returns true
- `PluginAuthorizer` (Pro): checks ownership locally, then delegates to enforcement plugin
- `requireAccess()` helper on `Server`: extracts user from context, calls authorizer, writes 403

### Permission hierarchy

`admin > write > read`:
- An `admin` grant allows everything including `delete` and `admin`
- A `write` grant allows both `write` and `read` actions
- A `read` grant allows `read` only

### SQLite for ACL state

The plugin stores access grants in SQLite (`modernc.org/sqlite`, pure Go, no CGO).
This keeps the plugin self-contained with no external dependencies.

```sql
CREATE TABLE IF NOT EXISTS access_grants (
    grant_id      TEXT PRIMARY KEY,
    grantee_id    TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id   TEXT NOT NULL,
    permission    TEXT NOT NULL CHECK (permission IN ('read', 'write', 'admin')),
    granted_by    TEXT NOT NULL,
    granted_at    TEXT NOT NULL,
    revoked_at    TEXT,
    UNIQUE(grantee_id, resource_type, resource_id) ON CONFLICT REPLACE
);
```

Revocation is a soft-delete (`revoked_at` set). Duplicate grants for the same
(grantee, resource_type, resource_id) replace the existing grant (upsert).

### REST sharing endpoints on ratd

ratd exposes REST endpoints that proxy to the sharing plugin:
- `POST /api/v1/sharing` -> `ShareResource()`
- `GET /api/v1/sharing?resource_type=X&resource_id=Y` -> `ListAccess()`
- `DELETE /api/v1/sharing/{grant_id}` -> `RevokeAccess()`
- `POST /api/v1/sharing/transfer` -> `TransferOwnership()`

All require auth context. If sharing plugin not loaded -> 501 Not Implemented.

### Enforcement on mutation handlers

Write/delete operations in ratd handlers call `requireAccess()`:
- `HandleUpdatePipeline`: `requireAccess("pipeline", id, "write")`
- `HandleDeletePipeline`: `requireAccess("pipeline", id, "delete")`
- `HandleCreateRun`: `requireAccess("pipeline", id, "write")`
- `HandleDeleteNamespace`: `requireAccess("namespace", name, "delete")`
- `HandleWriteFile`/`HandleDeleteFile`/`HandleUploadFile`: `requireAccess("namespace", ns, "write")`

Read operations stay open (enforcement on reads is v2.8+ scope).

## Consequences

### Positive

- **Clean separation**: community repo has `NoopAuthorizer` (zero overhead), Pro adds enforcement
- **Single container**: sharing + enforcement in one binary, simple deployment
- **SQLite**: no external database dependency for the plugin
- **Ownership is local**: ratd checks ownership in Postgres before calling the plugin, avoiding network roundtrips for owner access
- **Graceful degradation**: if enforcement plugin is unavailable, `requireAccess()` allows all (no user = community mode)

### Negative

- **No RLS**: enforcement is at the API level only (not at the storage/query level). Row-Level Security is deferred to v2.8+
- **No wildcard sharing**: `SHARE bronze.* WITH role:engineer` not supported yet
- **No audit trail**: sharing events aren't logged to an audit service
- **SQLite single-writer**: concurrent writes are serialized (WAL mode helps reads). Sufficient for the expected write volume (sharing mutations are infrequent)

### Not implemented (future scope)

- `TransferOwnership` — returns `CodeUnimplemented` (ownership lives in ratd Postgres, needs a pipeline UPDATE endpoint)
- `GetCredentials` — STS credential vending for S3 IAM is v2.8+ scope
- Read-path enforcement — deferred to v2.8+
- Audit logging of access checks — deferred to audit plugin
