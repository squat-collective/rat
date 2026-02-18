# TypeScript SDK Reference

> Package: `@squat-collective/rat-client` v2.0.0
> Source: `sdk-typescript/src/`
> Build: `make sdk-build` | Test: `make sdk-test` (27 tests)

---

## Client

```typescript
import { RatClient } from "@squat-collective/rat-client";

const client = new RatClient({
  apiUrl: "http://localhost:8080",  // default
  timeout: 30000,                   // ms, default
  maxRetries: 3,                    // default
  headers: {},                      // extra headers
});
```

The client exposes 7 resources as readonly properties:

```typescript
client.health      // HealthResource
client.pipelines   // PipelinesResource
client.runs        // RunsResource
client.query       // QueryResource
client.tables      // TablesResource
client.storage     // StorageResource
client.namespaces  // NamespacesResource
```

---

## Resources

### health

| Method | Endpoint | Returns |
|--------|----------|---------|
| `getHealth()` | `GET /health` | `HealthResponse` |
| `getFeatures()` | `GET /api/v1/features` | `FeaturesResponse` |

### pipelines

| Method | Endpoint | Returns |
|--------|----------|---------|
| `list(params?)` | `GET /api/v1/pipelines?namespace=X&layer=Y` | `PipelineListResponse` |
| `get(ns, layer, name)` | `GET /api/v1/pipelines/:ns/:layer/:name` | `Pipeline` |
| `create(req)` | `POST /api/v1/pipelines` | `CreatePipelineResponse` |
| `update(ns, layer, name, req)` | `PUT /api/v1/pipelines/:ns/:layer/:name` | `Pipeline` |
| `delete(ns, layer, name)` | `DELETE /api/v1/pipelines/:ns/:layer/:name` | `void` |

**Params**: `{ namespace?: string; layer?: string }`

### runs

| Method | Endpoint | Returns |
|--------|----------|---------|
| `list(params?)` | `GET /api/v1/runs?namespace=X&status=Y` | `RunListResponse` |
| `get(id)` | `GET /api/v1/runs/:id` | `Run` |
| `create(req)` | `POST /api/v1/runs` | `CreateRunResponse` |
| `cancel(id)` | `POST /api/v1/runs/:id/cancel` | `{ run_id, status }` |
| `logs(id)` | `GET /api/v1/runs/:id/logs` | `RunLogsResponse` |

**Params**: `{ namespace?: string; status?: string }`

### query

| Method | Endpoint | Returns |
|--------|----------|---------|
| `execute(req)` | `POST /api/v1/query` | `QueryResult` |

### tables

| Method | Endpoint | Returns |
|--------|----------|---------|
| `list(params?)` | `GET /api/v1/tables?namespace=X&layer=Y` | `TableListResponse` |
| `get(ns, layer, name)` | `GET /api/v1/tables/:ns/:layer/:name` | `TableDetail` |
| `preview(ns, layer, name)` | `GET /api/v1/tables/:ns/:layer/:name/preview` | `QueryResult` |

**Params**: `{ namespace?: string; layer?: string }`

### storage

| Method | Endpoint | Returns |
|--------|----------|---------|
| `list(prefix?)` | `GET /api/v1/files?prefix=X` | `FileListResponse` |
| `read(path)` | `GET /api/v1/files/:path` | `FileContent` |
| `write(path, content)` | `PUT /api/v1/files/:path` | `{ path, status }` |
| `delete(path)` | `DELETE /api/v1/files/:path` | `void` |
| `upload(path, file, filename?)` | `POST /api/v1/files/upload` | `{ path, filename, size, status }` |

### namespaces

| Method | Endpoint | Returns |
|--------|----------|---------|
| `list()` | `GET /api/v1/namespaces` | `NamespaceListResponse` |
| `create(name)` | `POST /api/v1/namespaces` | `Namespace` |
| `delete(name)` | `DELETE /api/v1/namespaces/:name` | `void` |

---

## Model Types

### Pipeline

```typescript
type Layer = "bronze" | "silver" | "gold";

interface Pipeline {
  id: string;
  namespace: string;
  layer: Layer;
  name: string;
  type: string;              // "sql" | "python"
  s3_path: string;
  description: string;
  owner: string | null;
  created_at: string;
  updated_at: string;
}

interface PipelineListResponse {
  pipelines: Pipeline[];
  total: number;
}

interface CreatePipelineRequest {
  namespace: string;
  layer: string;
  name: string;
  type?: string;
  source?: string;
  unique_key?: string;
  description?: string;
}

interface CreatePipelineResponse {
  namespace: string;
  layer: string;
  name: string;
  s3_path: string;
  files_created: string[];
}

interface UpdatePipelineRequest {
  description?: string;
  type?: string;
}
```

### Run

```typescript
type RunStatus = "pending" | "running" | "success" | "failed" | "cancelled";

interface Run {
  id: string;
  pipeline_id: string;
  status: RunStatus;
  trigger: string;
  started_at: string | null;
  finished_at: string | null;
  duration_ms: number | null;
  rows_written: number | null;
  error: string | null;
  logs_s3_path: string | null;
  created_at: string;
}

interface RunListResponse { runs: Run[]; total: number; }
interface CreateRunRequest { namespace: string; layer: string; pipeline: string; trigger: string; }
interface CreateRunResponse { run_id: string; status: string; }
interface RunLog { timestamp: string; level: string; message: string; }
interface RunLogsResponse { logs: RunLog[]; status: string; }
```

### Query

```typescript
interface QueryColumn { name: string; type: string; }
interface QueryResult {
  columns: QueryColumn[];
  rows: Record<string, unknown>[];  // objects, not arrays
  total_rows: number;
  duration_ms: number;
}
interface QueryRequest { sql: string; namespace?: string; limit?: number; }
```

### Table

```typescript
interface TableInfo {
  namespace: string;
  layer: string;
  name: string;
  row_count: number;
  size_bytes: number;
}
interface TableDetail extends TableInfo {
  columns: QueryColumn[];
}
interface TableListResponse { tables: TableInfo[]; total: number; }
```

### Storage

```typescript
interface FileInfo { path: string; size: number; modified: string; type: string; }
interface FileContent { path: string; content: string; size: number; modified: string; }
interface FileListResponse { files: FileInfo[]; }
```

### Namespace

```typescript
interface Namespace { name: string; created_at: string; }
interface NamespaceListResponse { namespaces: Namespace[]; total: number; }
```

### Pipeline Config

```typescript
type MergeStrategy = "full_refresh" | "incremental" | "append_only"
  | "delete_insert" | "scd2" | "snapshot";

interface PipelineConfig {
  description?: string;
  materialized?: "table" | "view";
  unique_key?: string[];
  merge_strategy?: MergeStrategy;
  watermark_column?: string;
  archive_landing_zones?: boolean;
  partition_column?: string;
  scd_valid_from?: string;
  scd_valid_to?: string;
}
```

Used by the portal's `usePipelineConfig()` hook to read/write `config.yaml` on S3. Not sent via REST API — these types model the per-pipeline config file stored at `{ns}/pipelines/{layer}/{name}/config.yaml`.

### Health

```typescript
interface HealthResponse { status: string; }
interface FeaturesResponse {
  edition: string;
  plugins: Record<string, PluginFeature>;
  namespaces: boolean;
  multi_user: boolean;
}
interface PluginFeature { enabled: boolean; type?: string; }
```

---

## Error Classes

All extend `RatError`:

| Class | Status | Description |
|-------|--------|-------------|
| `RatError` | any | Base error class |
| `ValidationError` | 400/422 | Invalid request |
| `AuthenticationError` | 401 | Unauthenticated (Pro only) |
| `AuthorizationError` | 403 | Forbidden (Pro only) |
| `NotFoundError` | 404 | Resource not found |
| `ConflictError` | 409 | Duplicate / not cancellable |
| `ServerError` | 5xx | Server-side failure |
| `ConnectionError` | — | Network / timeout failure |

---

## Transport Details

- **Error format**: v2 Go API returns plain text errors (`response.text()`), not JSON `{ detail }`
- **Retry**: Exponential backoff — `500ms * (attempt + 1)`, only for `ConnectionError`
- **Timeout**: Configurable via `AbortController` (default: 30s)
- **Auth**: No auth headers in Community Edition. Pro adds Bearer token injection.
- **Upload**: Multipart `FormData` for `storage.upload()` (bypasses JSON serialization)

---

## Build & Test

```bash
make sdk-build    # Docker: npm install + tsup → dist/
make sdk-test     # Docker: npm install + tsup + vitest (27 tests)
```

Output: `dist/index.js` (ESM), `dist/index.cjs` (CJS), `dist/index.d.ts` (declarations)
