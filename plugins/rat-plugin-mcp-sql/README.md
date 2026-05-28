# rat-plugin-mcp-sql

MCP (Model Context Protocol) server exposing RAT's SQL query surface to
external MCP clients. Lets an AI assistant outside RAT (e.g. Claude
Desktop) inspect your tables and run read-only queries.

## Install

```bash
docker run -d --name mcp-sql --network infra_default --restart unless-stopped \
  -e RATD_URL=http://ratd:8080 \
  -e RATD_INTERNAL_URL=http://ratd:8090 \
  ghcr.io/squat-collective/rat-plugin-mcp-sql:latest
```

Or uncomment the `mcp-sql:` block in
[`infra/docker-compose.plugins.yml`](../../infra/docker-compose.plugins.yml).

## Wire to an MCP client

Point your MCP client at:

```
http://<host>:8080/api/v1/x/mcp-sql/mcp
```

The plugin exposes tools `list_tables`, `describe_table`, `query`
(read-only, scoped to ratq).

## Environment

| Var | Default | Purpose |
|---|---|---|
| `RATD_URL` | `http://ratd:8080` | ratd base URL |
| `RATD_INTERNAL_URL` | `http://ratd:8090` | ratd internal listener |
| `GRPC_PORT` | `50118` | Port to serve on |

## Security

- All queries go through ratq (read-only by design).
- No write capabilities exposed — schema mutations require the portal
  or ratd's REST API.
- If you wire this to an MCP client outside your trust boundary,
  consider placing ratd behind an auth proxy first (the MCP server has
  no authentication of its own).

## Build from source

```bash
git clone https://github.com/squat-collective/rat
cd rat/plugins/rat-plugin-mcp-sql
make build && make run
```
