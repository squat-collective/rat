# Infrastructure & Docker Review

**Reviewer**: Senior DevOps / Infrastructure Engineer
**Date**: 2026-02-16
**Scope**: All Dockerfiles, docker-compose.yml, Makefile, smoke test scripts, CI/CD
**Branch**: `feat/ratd-health-and-pipelines`

---

## Executive Summary

**47 findings total:** 5 Critical, 11 High, 14 Medium, 9 Low, 8 Suggestions

The infrastructure demonstrates solid foundations — multi-stage builds, non-root users, health checks on core services, localhost-only port bindings for infrastructure. However, critical issues include dev dependencies leaking into production images, hardcoded secrets in compose, and a Go image inconsistency in the Makefile.

| Severity | Count |
|----------|-------|
| Critical | 5 |
| High | 11 |
| Medium | 14 |
| Low | 9 |
| Suggestion | 8 |
| **Total** | **47** |

---

## 1. Dockerfiles

### 1.1 Platform — `platform/Dockerfile`

```dockerfile
FROM golang:1.24-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o ratd ./cmd/ratd

FROM alpine:3.21
RUN adduser -D -u 1001 appuser
COPY --from=builder /build/ratd /usr/local/bin/ratd
USER appuser
ENTRYPOINT ["ratd"]
```

| # | Severity | Issue | Fix |
|---|----------|-------|-----|
| 1 | 🟡 Medium | **Not using `scratch` base image.** The Go binary is statically compiled (`CGO_ENABLED=0`), so it can run on `scratch`. Using `alpine` adds ~5MB and a shell (attack surface). | Use `FROM scratch` for smallest, most secure image. Add `COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/` for TLS. |
| 2 | 🟡 Medium | **No HEALTHCHECK instruction.** | Add `HEALTHCHECK --interval=5s --timeout=3s --retries=3 CMD ["wget", "-qO-", "http://localhost:8080/health"]` |
| 3 | 🔵 Low | **No OCI labels/metadata.** | Add `LABEL org.opencontainers.image.source`, version, description |
| 4 | 🔵 Low | **Base image not pinned to digest.** `golang:1.24-alpine` can change between builds. | Pin to `golang:1.24.1-alpine3.21@sha256:...` |
| 5 | 💡 Suggestion | **No `.dockerignore` file.** Build context includes tests, docs, git history. | Create `platform/.dockerignore` |

### 1.2 Runner — `runner/Dockerfile`

| # | Severity | Issue | Fix |
|---|----------|-------|-----|
| 6 | 🔴 Critical | **Dev dependencies installed in builder AND leaked to production image.** Line 6 installs `[dev]` deps (pytest, ruff, pyright, grpcio-tools) in the builder. Since the entire `site-packages` directory is copied to the runtime image, all dev dependencies end up in production. Bloats image and introduces unnecessary packages. | Split: install only production deps for the runtime copy. |
| 7 | 🟠 High | **Entire `/usr/local/bin` copied from builder.** Copies ALL binaries including `pip`, `uv`, `ruff`, `pytest` — security risk (allows package installation in running container) and bloats image. | Copy only specific binaries needed, or don't copy `/usr/local/bin` at all. |
| 8 | 🟡 Medium | **No EXPOSE instruction.** Runner listens on gRPC port 50052 but Dockerfile doesn't document it. | Add `EXPOSE 50052`. |
| 9 | 🟡 Medium | **No HEALTHCHECK instruction.** | Add appropriate health check. |
| 10 | 🟡 Medium | **Base image not pinned to digest.** | Pin to specific digest. |
| 11 | 🔵 Low | **No `.dockerignore` file.** | Create `runner/.dockerignore` |
| 12 | 💡 Suggestion | **No labels/metadata.** | Add OCI labels. |

### 1.3 Query — `query/Dockerfile`

| # | Severity | Issue | Fix |
|---|----------|-------|-----|
| 13 | 🔴 Critical | **Same dev-dependency leak as runner (issue #6).** Identical problem. | Same fix as #6. |
| 14 | 🟠 High | **Same `/usr/local/bin` full copy as runner (issue #7).** | Same fix as #7. |
| 15 | 🟡 Medium | **No EXPOSE, no HEALTHCHECK, no `.dockerignore`, no pinned image.** | Same fixes as runner. |

### 1.4 Portal — `portal/Dockerfile`

| # | Severity | Issue | Fix |
|---|----------|-------|-----|
| 16 | 🟠 High | **Build context is the repo root (`context: ..`).** The compose file sets `context: ..` meaning the ENTIRE monorepo is sent as the Docker build context. Without a `.dockerignore` at the repo root, this includes `platform/`, `runner/`, `query/`, `.git/`, `node_modules/`. | Create root-level `.dockerignore` excluding everything except `portal/` and `sdk-typescript/`. |
| 17 | 🟡 Medium | **`package-lock.json*` glob in COPY.** The `*` wildcard means the build won't fail if lockfile is missing — silently falls back to non-deterministic `npm ci`. | Remove the glob: `COPY portal/package.json portal/package-lock.json ./` |
| 18 | 🟡 Medium | **No HEALTHCHECK instruction.** | Add health check. |
| 19 | 🔵 Low | **Node 20 base image not pinned.** | Pin to `node:20.x.x-alpine`. |
| 20 | 💡 Suggestion | **Consider `--mount=type=cache` for npm cache.** | Speeds up rebuilds. |

### 1.5 Missing `.dockerignore` Files

| # | Severity | Issue | Fix |
|---|----------|-------|-----|
| 21 | 🟠 High | **No `.dockerignore` files anywhere.** Every build sends unnecessary files (tests, docs, git history, caches) to Docker daemon. | Create `.dockerignore` for each service and at repo root. |

---

## 2. Docker Compose — `infra/docker-compose.yml`

### 2.1 Security

| # | Severity | Issue | Fix |
|---|----------|-------|-----|
| 22 | 🔴 Critical | **Hardcoded secrets in plain text.** `rat:rat` for Postgres, `minioadmin:minioadmin` for MinIO appear in 13+ locations. | Use env var substitution with defaults for dev: `POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:-rat}`. Document production overrides. Use Docker secrets or gitignored `.env`. |
| 23 | 🔴 Critical | **S3 credentials duplicated across 4 services.** Same credentials in ratd, ratq, runner, and nessie. | Use YAML anchors or `x-` extension fields to define once. |
| 24 | 🟠 High | **No `read_only: true` on any service.** | Add `read_only: true` to application services. Add `tmpfs` mounts for writable dirs. |
| 25 | 🟠 High | **No `cap_drop` or `security_opt`.** No services drop Linux capabilities. | Add `cap_drop: [ALL]` and `security_opt: [no-new-privileges:true]`. |

### 2.2 Resource Management

| # | Severity | Issue | Fix |
|---|----------|-------|-----|
| 26 | 🟠 High | **No resource limits on ANY service.** A runaway DuckDB query could consume all host resources. | Add `deploy: { resources: { limits: { memory: 2G, cpus: '2.0' } } }`. Set `pids_limit: 100`. |
| 27 | 🟡 Medium | **No `stop_grace_period` defined.** Default 10s may not be enough for runner to finish pipeline execution. | Add `stop_grace_period: 30s` to runner and postgres. |

### 2.3 Networking

| # | Severity | Issue | Fix |
|---|----------|-------|-----|
| 28 | 🟡 Medium | **No explicit network definitions.** All services share default network. Nessie, Postgres, MinIO should not be reachable from portal. | Define `frontend` (portal, ratd) and `backend` (ratd, ratq, runner, postgres, minio, nessie) networks. |
| 29 | 🔵 Low | **Nessie port not exposed for dev access.** Makefile prints Nessie URL but no `ports:` mapping. | Add `ports: ["127.0.0.1:19120:19120"]` or remove URL from Makefile. |

### 2.4 Health Checks

| # | Severity | Issue | Fix |
|---|----------|-------|-----|
| 30 | 🟡 Medium | **Portal has no health check in compose.** | Add wget-based health check. |
| 31 | 🟡 Medium | **ratq/runner health checks require Python import.** Slow (~2-3s Python startup). | Consider `grpc_health_probe` binary or HTTP health endpoint. |
| 32 | 🔵 Low | **No `start_period` on ratq/runner health checks.** Python services take time to start. | Add `start_period: 10s`. |
| 33 | 🔵 Low | **Nessie has no health check.** Uses `condition: service_started` instead of `service_healthy`. | Add curl-based health check to Nessie config endpoint. |

### 2.5 Service Configuration

| # | Severity | Issue | Fix |
|---|----------|-------|-----|
| 34 | 🔵 Low | **MinIO uses `latest` tag.** Non-reproducible builds. | Pin to dated release tag. |
| 35 | 🔵 Low | **Nessie uses `latest` tag.** Same issue. | Pin to semver release. |
| 36 | 💡 Suggestion | **`minio-init` could use `restart: on-failure`.** | Use `restart: on-failure` instead of `restart: "no"`. |
| 37 | 💡 Suggestion | **Consider `init: true` on application services.** Adds tini as PID 1 for proper signal handling and zombie reaping. | Add `init: true` to ratq, runner, portal. |

---

## 3. Docker Compose — `infra/docker-compose.test.yml`

| # | Severity | Issue | Fix |
|---|----------|-------|-----|
| 38 | 🔵 Low | **Test MinIO uses `latest` tag.** | Pin version. |
| 39 | 💡 Suggestion | **Test services have no `profiles`.** Could start accidentally. | Consider compose profiles. |

---

## 4. Makefile

### 4.1 Correctness & Robustness

| # | Severity | Issue | Fix |
|---|----------|-------|-----|
| 40 | 🔴 Critical | **`test-go` uses `golang:1.24` but variable defines `GO_IMAGE := golang:1.24-alpine`.** Line 52 hardcodes `golang:1.24` (Debian ~800MB) while the `GO_IMAGE` variable on line 9 is `golang:1.24-alpine` (~250MB). The `lint` target correctly uses `$(GO_IMAGE)`. `test-integration` also hardcodes `golang:1.24`. | Replace hardcoded `golang:1.24` with `$(GO_IMAGE)` everywhere. |
| 41 | 🟠 High | **`test-py` installs dependencies from scratch on EVERY run.** Each invocation starts a fresh container, installs `uv`, then installs all deps (~1-2 min). | Create pre-built test images or use Docker volume caching. |
| 42 | 🟠 High | **`dev-ratd` uses hardcoded network name `infra_default`.** Assumes compose project name is `infra`. | Set explicit `name:` in compose networks section and reference it. |
| 43 | 🟡 Medium | **`clean` target uses `rm -rf` without confirmation.** | Add echo before rm for visibility. |
| 44 | 🟡 Medium | **No `setup` target despite CLAUDE.md documenting `make setup`.** | Add `setup: proto sqlc sdk-build` target. |

### 4.2 Developer Experience

| # | Severity | Issue | Fix |
|---|----------|-------|-----|
| 45 | 🟡 Medium | **`dev-portal` doesn't connect to compose network.** Server-side rendering with `API_URL: http://ratd:8080` will fail. | Add `--network` flag or use localhost-based `API_URL` for dev. |
| 46 | 💡 Suggestion | **No `make restart` target.** | Add `restart` target with optional service argument. |
| 47 | 💡 Suggestion | **No parallel build support.** `make test` runs sequentially. | Consider `make -j3` or parallel test target. |

---

## 5. Smoke Test — `infra/scripts/smoke-test.sh`

| # | Severity | Issue | Fix |
|---|----------|-------|-----|
| 48 | 🟠 High | **Missing `set -e` (errexit).** Non-curl failures won't abort the script. | Add `set -euo pipefail`. |
| 49 | 🟡 Medium | **Depends on `python3` being available on host.** Contradicts "never install on host" guideline. | Replace with `jq` or run smoke test inside container. |
| 50 | 🔵 Low | **Temp files in `/tmp/smoke-*.json` without unique prefix.** Could collide in parallel CI runs. | Use `mktemp -d`. |

---

## 6. CI/CD

| # | Severity | Issue | Fix |
|---|----------|-------|-----|
| 51 | 🟠 High | **No CI/CD configuration exists.** No `.github/workflows/` directory. CLAUDE.md references CI enforcement but none is configured. | Create `.github/workflows/ci.yml` with jobs for test, lint, proto-breaking, docker-build. |

---

## 7. Production Readiness

| # | Severity | Issue | Fix |
|---|----------|-------|-----|
| 52 | 🟠 High | **No logging configuration.** Docker defaults to `json-file` with no rotation. Will fill disk. | Add `logging: { driver: json-file, options: { max-size: "10m", max-file: "3" } }`. |
| 53 | 🟡 Medium | **No backup strategy for persistent volumes.** `postgres_data` and `minio_data` are named volumes with no backup. | Document backup strategy. Add `make backup` target for pg_dump + mc mirror. |
| 54 | 🟡 Medium | **No rolling update / zero-downtime deployment.** `docker compose up -d --build` causes downtime. | Document Kubernetes as production target. Consider Traefik/Caddy for graceful switchover. |

---

## 8. Cross-Cutting Concerns

### 8.1 Image Pinning Summary

| Image | Current | Recommendation |
|-------|---------|----------------|
| `golang:1.24-alpine` | Minor-pinned | Pin patch: `golang:1.24.1-alpine3.21` |
| `python:3.12-slim` | Minor-pinned | Pin patch: `python:3.12.8-slim-bookworm` |
| `node:20-alpine` | Major-pinned | Pin minor: `node:20.18-alpine3.21` |
| `postgres:16-alpine` | Major-pinned | Pin minor: `postgres:16.6-alpine3.21` |
| `minio/minio:latest` | **Unpinned** | Pin to dated release tag |
| `minio/mc:latest` | **Unpinned** | Pin to dated release tag |
| `ghcr.io/projectnessie/nessie:latest` | **Unpinned** | Pin to semver release |
| `bufbuild/buf:latest` | **Unpinned** | Pin to semver release |

### 8.2 Secret Handling

Hardcoded credentials found in compose:
- `POSTGRES_USER: rat` / `POSTGRES_PASSWORD: rat` (5 locations)
- `minioadmin` / `minioadmin` (8 locations)
- `DATABASE_URL` with embedded password (1 location)
- Nessie `S3CREDS_NAME` / `S3CREDS_SECRET` (1 location)

**Recommendation:** Consolidate all secrets into a `.env` file (gitignored), commit a `.env.example` with placeholder values.

### 8.3 Build Performance Optimization Opportunities

1. **BuildKit parallelism:** Add `DOCKER_BUILDKIT=1` to Makefile.
2. **Cache mounts:** Use `--mount=type=cache` for Go modules, pip cache, and npm cache.
3. **Pre-built test images:** Build test images with dependencies cached.
4. **Compose build parallelism:** `docker compose build --parallel`.

---

## 9. Priority Action Items

### Immediate (before public release)
1. **Fix dev dependency leak in Python Dockerfiles** (#6, #13) — security + image size
2. **Fix `/usr/local/bin` full copy** (#7, #14) — security risk
3. **Create `.dockerignore` files** (#21) — build performance + security
4. **Externalize secrets from compose** (#22, #23) — security
5. **Fix Go image inconsistency in Makefile** (#40) — correctness

### Short-term (next sprint)
6. Add resource limits to compose services (#26)
7. Add container security hardening: `cap_drop`, `read_only`, `security_opt` (#24, #25)
8. Pin all image versions (#34, #35)
9. Add missing health checks (#30, #33) and `start_period` (#32)
10. Set up GitHub Actions CI (#51)
11. Add log rotation (#52)

### Medium-term (next milestone)
12. Network segmentation (#28)
13. Optimize test performance with cached images (#41)
14. Add `make setup` target (#44)
15. Consider `scratch` base for ratd (#1)
16. Document backup strategy (#53)
17. Add OCI labels to all images (#3)

---

## 10. What's Done Well

1. **Multi-stage builds on all Dockerfiles.** Build and runtime stages properly separated.
2. **Non-root users in all custom images.** `appuser`, `runner`, `query` users created and used.
3. **Compose health checks on core services.** ratd, ratq, runner, postgres, minio all have health checks with proper `depends_on` conditions.
4. **Localhost-only port bindings for infrastructure.** Postgres and MinIO correctly bound to localhost only, preventing accidental exposure.
5. **Test compose with tmpfs.** RAM-backed storage for test databases — fast and ephemeral.
6. **Makefile as single entry point.** Well-organized with help documentation, clear sections, and containerized execution.
7. **MinIO init container pattern.** Separate init container for bucket setup with `restart: "no"`.
8. **Static Go binary with stripped symbols.** `CGO_ENABLED=0` + `-ldflags="-s -w"` produces minimal binary.
9. **Next.js standalone output.** Using `output: "standalone"` for optimal Docker image size.
10. **Smoke test script.** Comprehensive E2E test covering the full API flow with proper cleanup.

---

*Review generated 2026-02-16. File paths are relative to the repository root.*
