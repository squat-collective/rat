# Zero-Downtime Deployment for RAT Pro

> **Scope**: This guide covers zero-downtime deployment strategies for RAT Pro
> (multi-container deployment with plugin services). Community Edition uses a
> simpler `docker compose up -d` workflow where brief downtime is acceptable.

---

## Overview

RAT Pro deploys multiple cooperating services (ratd, ratq, runner, portal, plus
Pro plugins like auth-keycloak, enforcement, cloud-aws). A zero-downtime deploy
ensures users experience no interruption during upgrades.

### Key Constraints

- **ratd** is the single API gateway -- all traffic flows through it
- **runner** executes long-running pipelines (minutes to hours)
- **ratq** handles interactive queries (sub-second to minutes)
- **portal** serves the web IDE (stateless, easy to swap)
- **Postgres** and **MinIO** are stateful -- upgraded separately

---

## Strategy: Rolling Update with Health Gates

### 1. Service Ordering

Deploy services in this order to respect dependency chains:

```
1. Infrastructure (Postgres, MinIO, Nessie) — only if schema changes
2. ratq (stateless query sidecar)
3. runner (stateful — must drain running pipelines)
4. Pro plugins (auth-keycloak, enforcement, cloud-aws)
5. ratd (API gateway — depends on all backend services)
6. portal (stateless frontend — last, depends on ratd)
```

### 2. Health Check Gates

Every service has a healthcheck. The deployment tool must wait for the new
instance to pass its healthcheck before stopping the old one.

```yaml
# Example: rolling update of ratd
# 1. Start new ratd container on the same network
# 2. Wait for healthcheck: /ratd healthcheck (5s interval, 5 retries)
# 3. Update reverse proxy / load balancer to point to new container
# 4. Stop old ratd container with SIGTERM + 30s grace period
```

### 3. Runner Drain (Graceful Pipeline Completion)

The runner service may have active pipeline executions. Before stopping:

1. **Stop accepting new work**: Set the runner to "draining" mode via its
   gRPC health status (NOT_SERVING).
2. **Wait for active runs**: ratd's warm pool executor will stop dispatching
   to the draining runner. Allow `stop_grace_period` (default: 30s) for
   in-flight runs to checkpoint or complete.
3. **Force stop if needed**: After the grace period, SIGKILL terminates
   remaining work. Runs left in `running` state will be cleaned up by
   ratd's reaper (marks them as `failed` after the stuck-run timeout).

```yaml
# docker-compose.pro.yml override for runner
runner:
  stop_grace_period: 120s  # Allow 2 minutes for pipeline drain
  deploy:
    update_config:
      order: start-first     # Start new runner before stopping old
      failure_action: rollback
```

### 4. Portal (Stateless Swap)

The portal is a stateless Next.js app. Deploy by starting a new container,
waiting for the healthcheck, and swapping the load balancer target.

```yaml
portal:
  deploy:
    update_config:
      order: start-first
      parallelism: 1
```

---

## Docker Compose Rolling Update

For Docker Compose deployments (non-Kubernetes), use the `--no-deps` flag
to update services individually:

```bash
# Step 1: Pull new images
docker compose -f docker-compose.yml -f docker-compose.pro.yml pull

# Step 2: Update stateless services first (start-first approach)
docker compose up -d --no-deps --scale ratq=2 ratq
# Wait for new ratq to be healthy
docker compose up -d --no-deps ratq  # scales back to 1, removes old

# Step 3: Update runner with drain
docker compose up -d --no-deps runner

# Step 4: Update plugins
docker compose up -d --no-deps auth-keycloak

# Step 5: Update ratd
docker compose up -d --no-deps ratd

# Step 6: Update portal
docker compose up -d --no-deps portal
```

---

## Kubernetes Deployment

For Kubernetes deployments, use standard rolling update strategies:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ratd
spec:
  replicas: 2
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxUnavailable: 0    # Never remove a pod before replacement is ready
      maxSurge: 1           # Add one new pod at a time
  template:
    spec:
      terminationGracePeriodSeconds: 30
      containers:
        - name: ratd
          livenessProbe:
            exec:
              command: ["/ratd", "healthcheck"]
            periodSeconds: 5
          readinessProbe:
            httpGet:
              path: /health
              port: 8080
            periodSeconds: 5
```

---

## Database Migrations

Schema changes require special handling:

1. **Backward-compatible migrations only**: New columns must have defaults.
   Renaming or dropping columns requires a two-phase deploy.
2. **Run migrations before deploying new code**: ratd runs auto-migration
   on startup (`platform/internal/postgres/migrate.go`). The new schema
   must be compatible with both old and new code versions.
3. **For breaking schema changes**:
   - Phase 1: Deploy new code that reads both old and new schema
   - Phase 2: Run migration to update schema
   - Phase 3: Deploy code that only reads new schema
   - Phase 4: Drop old columns (cleanup)

---

## Rollback

If a deployment fails:

1. **Immediate**: `docker compose up -d --no-deps <service>` with the
   previous image tag
2. **Database**: If a migration was applied, run the reverse migration
   before rolling back code (migrations should be reversible)
3. **State cleanup**: ratd's reaper handles stuck runs automatically --
   no manual intervention needed for runner rollbacks

---

## Monitoring During Deploy

Watch these signals during deployment:

- **ratd health**: `GET /health` returns 200
- **Runner active runs**: Check for running pipelines before stopping
- **Error rates**: Monitor HTTP 5xx responses through the portal
- **gRPC health**: ratd polls runner and ratq health as part of its
  warm pool connectivity check

```bash
# Quick health check across all services
docker compose ps --format "table {{.Name}}\t{{.Status}}"

# Check for active runs
curl -s http://localhost:8080/api/v1/runs?status=running | jq '.total'
```
