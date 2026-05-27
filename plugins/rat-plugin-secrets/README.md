# rat-plugin-secrets

AES-256-GCM encrypted vault. Other plugins consume secrets by name via
the interconnect capability `secrets.get`, so credentials never live in
the calling plugin's config.

## Install

```bash
docker volume create rat-secrets-key  # one-time
docker run -d --name secrets --network infra_default --restart unless-stopped \
  -e RATD_URL=http://ratd:8080 \
  -e RATD_INTERNAL_URL=http://ratd:8090 \
  -v rat-secrets-key:/data \
  ghcr.io/squat-collective/rat-plugin-secrets:latest
```

Or uncomment the `secrets:` block in
[`infra/docker-compose.plugins.yml`](../../infra/docker-compose.plugins.yml).

Once running, manage secrets at `/x/secrets` in the portal.

## How it works

The master key is generated on first start and persisted to
`/data/key.bin` inside the container. **Back up that file or the encrypted
volume — losing it makes every stored secret unrecoverable.**

Each secret is `AES-256-GCM(key, plaintext, random_nonce)`. The
ciphertext + nonce + name + description are stored in ratd's plugin-config
(via the standard plugin-config persist endpoint), so a fresh container
re-hydrates from ratd alongside the key on disk.

## API (proxied at `/api/v1/x/secrets/*`)

| Route | Purpose |
|---|---|
| `GET /secrets` | List secrets (names + descriptions, never values) |
| `POST /secrets` | Create a secret `{name, value, description}` |
| `PUT /secrets/{name}` | Update value (rotation) |
| `DELETE /secrets/{name}` | Remove |
| `POST /secrets/{name}/reveal` | One-shot reveal — returns plaintext (audit-logged) |

Other plugins use the `secrets.get` interconnect capability instead of
calling `/reveal` directly — that path requires the calling plugin to
already be registered and authenticated.

## Environment

| Var | Default | Purpose |
|---|---|---|
| `RATD_URL` | `http://ratd:8080` | ratd base URL |
| `RATD_INTERNAL_URL` | `http://ratd:8090` | ratd internal listener (phone-home) |
| `GRPC_PORT` | `50095` | Port to serve on |
| `PLUGIN_NAME` | `secrets` | Registered plugin name |
| `PLUGIN_ADDR` | `secrets:50095` | Address ratd dials back |

## Build from source

```bash
git clone https://github.com/squat-collective/rat
cd rat/plugins/rat-plugin-secrets
make build && make run
```
