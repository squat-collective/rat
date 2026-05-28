# rat-plugin-agents

Multi-step agent runner. Other plugins (e.g.
[`rat-plugin-chat`](../rat-plugin-chat/)) ask the agents plugin to plan
+ execute multi-step tasks against ratd's API and other plugins via the
[interconnect](../rat-plugin-interconnect/) broker.

Requires [`rat-plugin-ai-provider`](../rat-plugin-ai-provider/) (LLM
backend) and [`rat-plugin-interconnect`](../rat-plugin-interconnect/)
(capability lookup).

## Install

```bash
docker run -d --name agents --network infra_default --restart unless-stopped \
  -e RATD_URL=http://ratd:8080 \
  -e RATD_INTERNAL_URL=http://ratd:8090 \
  ghcr.io/squat-collective/rat-plugin-agents:latest
```

Or uncomment the `agents:` block in
[`infra/docker-compose.plugins.yml`](../../infra/docker-compose.plugins.yml).

## How it works

- Receives a goal (natural-language description of a task).
- Asks `ai.complete` (via interconnect) to break it into steps and pick
  a capability for each step.
- Invokes each capability through interconnect's `/invoke`.
- Streams progress back to the calling plugin via SSE.

State is in-memory only — restart loses in-flight agent runs.

## Environment

| Var | Default | Purpose |
|---|---|---|
| `RATD_URL` | `http://ratd:8080` | ratd base URL |
| `RATD_INTERNAL_URL` | `http://ratd:8090` | ratd internal listener |
| `GRPC_PORT` | `50115` | Port to serve on |

## Build from source

```bash
git clone https://github.com/squat-collective/rat
cd rat/plugins/rat-plugin-agents
make build && make run
```
