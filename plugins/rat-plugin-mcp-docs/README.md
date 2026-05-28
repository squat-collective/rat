# rat-plugin-mcp-docs

MCP (Model Context Protocol) server exposing RAT's documentation
(`docs/`, ADRs, plugin READMEs) to external MCP clients. Lets an AI
assistant outside RAT (e.g. Claude Desktop) answer "how does the merge
strategy work?" by reading the project's own docs.

## Install

```bash
docker run -d --name mcp-docs --network infra_default --restart unless-stopped \
  -e RATD_URL=http://ratd:8080 \
  -e RATD_INTERNAL_URL=http://ratd:8090 \
  ghcr.io/squat-collective/rat-plugin-mcp-docs:latest
```

Or uncomment the `mcp-docs:` block in
[`infra/docker-compose.plugins.yml`](../../infra/docker-compose.plugins.yml).

## Wire to an MCP client

Point your MCP client at:

```
http://<host>:8080/api/v1/x/mcp-docs/mcp
```

The plugin exposes tools `search_docs`, `read_doc`, and `list_docs` per
the MCP spec.

## Environment

| Var | Default | Purpose |
|---|---|---|
| `RATD_URL` | `http://ratd:8080` | ratd base URL |
| `RATD_INTERNAL_URL` | `http://ratd:8090` | ratd internal listener |
| `GRPC_PORT` | `50117` | Port to serve on |

## Build from source

```bash
git clone https://github.com/squat-collective/rat
cd rat/plugins/rat-plugin-mcp-docs
make build && make run
```
