# rat-plugin-http-source

Adds an `http` source connector: fetch a JSON REST endpoint, decode the
response into an Arrow table, and use it as input to a pipeline. Filling
the "I just want to pull from this API into a table" gap without writing
a custom Python pipeline.

## How it works

Registers under `rat.sources` entry-point. The runner's `ref()` resolver
checks the `rat.sources` registry: when a pipeline says
`SELECT * FROM http_source('<config>')`, this plugin issues the HTTP
request, JSON-decodes the body, and hands an Arrow table back.

## Install

Runner extension. Add to runner deps:

```bash
# In runner/pyproject.toml:
#   "rat-plugin-http-source"
```

## Usage

In a pipeline.sql:

```sql
-- @merge_strategy: incremental
-- @unique_key: id
-- @watermark_column: updated_at

SELECT *
FROM http_source('{
  "url": "https://api.example.com/orders",
  "method": "GET",
  "headers": { "Authorization": "Bearer ${API_TOKEN}" },
  "json_path": "data.orders",
  "page_param": "page",
  "page_size_param": "limit",
  "page_size": 100
}')
```

The source handles pagination via the `page_param` / `page_size_param`
fields, follows `Link: <…>; rel="next"` headers when present, and stops
on empty pages. Environment variable interpolation (`${...}`) lets you
keep tokens out of pipeline.sql — combine with
[`rat-plugin-secrets`](../rat-plugin-secrets/) for proper credential
management.

## Limitations

- **No retry policy.** A failed HTTP request fails the pipeline.
- **JSON only.** XML / CSV / other formats not currently supported.
- **No streaming.** The whole response is buffered into memory before
  conversion to Arrow.

## Build & test

```bash
cd plugins/rat-plugin-http-source
pip install -e '.[dev]'
pytest
```
