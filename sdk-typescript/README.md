# @squat-collective/rat-client — TypeScript SDK

> TypeScript client for the RAT REST API.
> Zero dependencies — uses native `fetch`. Works in Node 18+ and all modern browsers.

## Installation

```bash
npm install @squat-collective/rat-client
```

For local development (portal uses this):
```json
{
  "dependencies": {
    "@squat-collective/rat-client": "file:../sdk-typescript"
  }
}
```

## Quick Start

```typescript
import { RatClient } from "@squat-collective/rat-client";

const client = new RatClient({
  apiUrl: "http://localhost:8080",  // ratd API
});

// List pipelines
const { pipelines, total } = await client.pipelines.list({ namespace: "default" });

// Execute a query
const result = await client.query.execute({
  sql: "SELECT * FROM silver.orders LIMIT 10",
  namespace: "default",
});
console.log(result.columns, result.rows);

// Trigger a pipeline run
const run = await client.runs.create({
  namespace: "default",
  layer: "silver",
  pipeline: "orders",
  trigger: "manual",
});
```

## Client Options

```typescript
new RatClient({
  apiUrl?: string;      // Default: "http://localhost:8080"
  timeout?: number;     // Request timeout in ms (default: 30000)
  maxRetries?: number;  // Retry count for transient failures (default: 3)
  headers?: Record<string, string>;  // Extra headers for all requests
});
```

## Resources

The client exposes 7 resource classes:

| Resource | Methods | Description |
|----------|---------|-------------|
| `client.health` | `getHealth()`, `getFeatures()` | Service health + active plugins |
| `client.pipelines` | `list()`, `get()`, `create()`, `update()`, `delete()` | Pipeline CRUD |
| `client.runs` | `list()`, `get()`, `create()`, `cancel()`, `logs()` | Run lifecycle |
| `client.query` | `execute()` | Interactive SQL queries |
| `client.tables` | `list()`, `get()`, `preview()` | Table browsing + schema |
| `client.storage` | `list()`, `read()`, `write()`, `delete()`, `upload()` | S3 file management |
| `client.namespaces` | `list()`, `create()`, `delete()` | Namespace management |

## Error Handling

All API errors are typed:

```typescript
import { NotFoundError, ValidationError, ServerError } from "@squat-collective/rat-client";

try {
  await client.pipelines.get("default", "silver", "missing");
} catch (err) {
  if (err instanceof NotFoundError) {
    // 404 — pipeline doesn't exist
  }
}
```

| Error Class | HTTP Status | When |
|-------------|-------------|------|
| `ValidationError` | 400/422 | Invalid request body |
| `AuthenticationError` | 401 | Missing/invalid token (Pro) |
| `AuthorizationError` | 403 | Insufficient permissions (Pro) |
| `NotFoundError` | 404 | Resource not found |
| `ConflictError` | 409 | Duplicate resource / not cancellable |
| `ServerError` | 5xx | Server-side error |
| `ConnectionError` | — | Network failure / timeout |

> Note: The v2 Go API returns **plain text** error messages (not JSON `{ detail }` like v1).

## Transport

- Automatic retry with exponential backoff (`500ms * attempt`) for connection failures
- Configurable timeout via `AbortController`
- No auth headers in Community Edition (Pro adds Bearer token injection)

## Building

```bash
make sdk-build   # Build via Docker (node:20-alpine)
make sdk-test    # Build + run 27 vitest tests
```

Or locally:
```bash
npm install && npm run build   # ESM + CJS + DTS output
npm test                        # vitest
```

## Output

```
dist/
  index.js      # ESM
  index.cjs     # CommonJS
  index.d.ts    # TypeScript declarations
  index.d.cts
```
