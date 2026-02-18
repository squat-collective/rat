# Kubernetes Service Discovery

> **Status**: Reference documentation for deploying RAT v2 on Kubernetes.
> Docker Compose remains the primary deployment target for development and
> single-host production. This document covers the Kubernetes path for teams
> requiring horizontal scaling, high availability, or managed infrastructure.

---

## Overview

RAT v2 consists of 4 application services (ratd, ratq, runner, portal) and 3
infrastructure services (Postgres, MinIO/S3, Nessie). In Kubernetes, each
application service maps to a Deployment + Service pair. Infrastructure
services are typically backed by operators or managed cloud services.

## Service Topology

```
                    ┌──────────────┐
                    │   Ingress    │
                    │  (portal +   │
                    │   ratd API)  │
                    └──────┬───────┘
                           │
            ┌──────────────┼──────────────┐
            │              │              │
   ┌────────▼──────┐ ┌────▼─────┐ ┌──────▼──────┐
   │  portal (N)   │ │ ratd (N) │ │  ratd (N)   │
   │  Deployment   │ │ API-only │ │  + worker   │
   │  port 3000    │ │ port 8080│ │  port 8080  │
   └───────────────┘ └────┬─────┘ └──────┬──────┘
                          │              │
                  ┌───────┴──────┬───────┘
                  │              │
         ┌────────▼──────┐ ┌────▼────────┐
         │  ratq (1-N)   │ │ runner (N)  │
         │  Deployment   │ │ Deployment  │
         │  port 50051   │ │ port 50052  │
         └───────────────┘ └─────────────┘
```

## Kubernetes Service Definitions

### ratd (API + Worker)

```yaml
apiVersion: v1
kind: Service
metadata:
  name: ratd
  labels:
    app: ratd
spec:
  selector:
    app: ratd
  ports:
    - name: http
      port: 8080
      targetPort: 8080
  type: ClusterIP
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ratd
spec:
  replicas: 2
  selector:
    matchLabels:
      app: ratd
  template:
    metadata:
      labels:
        app: ratd
    spec:
      containers:
        - name: ratd
          image: rat-platform:latest
          ports:
            - containerPort: 8080
          env:
            - name: RAT_LISTEN_ADDR
              value: "0.0.0.0:8080"
            - name: DATABASE_URL
              valueFrom:
                secretKeyRef:
                  name: rat-secrets
                  key: database-url
            - name: S3_ENDPOINT
              value: "minio.rat.svc.cluster.local:9000"
            - name: RUNNER_ADDR
              # Multiple runners: comma-separated for round-robin dispatch
              value: "runner.rat.svc.cluster.local:50052"
            - name: RATQ_ADDR
              value: "ratq.rat.svc.cluster.local:50051"
            - name: NESSIE_URL
              value: "http://nessie.rat.svc.cluster.local:19120/api/v1"
          livenessProbe:
            httpGet:
              path: /health/live
              port: 8080
            initialDelaySeconds: 5
            periodSeconds: 10
          readinessProbe:
            httpGet:
              path: /health/ready
              port: 8080
            initialDelaySeconds: 10
            periodSeconds: 15
```

### Leader Election for Background Workers

RAT uses Postgres advisory locks (`pg_advisory_lock`) for leader election.
When running multiple ratd replicas, only one acquires the lock and runs the
scheduler, trigger evaluator, and reaper. If the leader dies, another replica
takes over automatically.

To run API-only replicas (no background workers):

```yaml
env:
  - name: SCHEDULER_ENABLED
    value: "false"
```

This is useful when you want dedicated API servers behind the Ingress and a
single worker replica for background tasks.

### runner

```yaml
apiVersion: v1
kind: Service
metadata:
  name: runner
spec:
  selector:
    app: runner
  ports:
    - name: grpc
      port: 50052
      targetPort: 50052
  type: ClusterIP
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: runner
spec:
  replicas: 2
  selector:
    matchLabels:
      app: runner
  template:
    metadata:
      labels:
        app: runner
    spec:
      containers:
        - name: runner
          image: rat-runner:latest
          ports:
            - containerPort: 50052
          env:
            - name: S3_ENDPOINT
              value: "minio.rat.svc.cluster.local:9000"
            - name: NESSIE_URL
              value: "http://nessie.rat.svc.cluster.local:19120/api/v1"
            - name: RUNNER_MAX_CONCURRENT
              value: "5"
          resources:
            requests:
              memory: "512Mi"
              cpu: "500m"
            limits:
              memory: "4Gi"
              cpu: "2"
```

### Multiple Runner Replicas

ratd supports multiple runner addresses for round-robin dispatch:

```yaml
# In ratd deployment:
env:
  - name: RUNNER_ADDR
    value: "runner-0.runner.rat.svc.cluster.local:50052,runner-1.runner.rat.svc.cluster.local:50052"
```

Alternatively, use a Kubernetes Service (ClusterIP) which load-balances across
all runner pods automatically. For gRPC, consider using a headless Service
with client-side load balancing, or a gRPC-aware load balancer (e.g., Envoy,
Linkerd).

### ratq (Query Service)

```yaml
apiVersion: v1
kind: Service
metadata:
  name: ratq
spec:
  selector:
    app: ratq
  ports:
    - name: grpc
      port: 50051
      targetPort: 50051
  type: ClusterIP
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ratq
spec:
  replicas: 1
  selector:
    matchLabels:
      app: ratq
  template:
    metadata:
      labels:
        app: ratq
    spec:
      containers:
        - name: ratq
          image: rat-query:latest
          ports:
            - containerPort: 50051
          env:
            - name: S3_ENDPOINT
              value: "minio.rat.svc.cluster.local:9000"
            - name: NESSIE_URL
              value: "http://nessie.rat.svc.cluster.local:19120/api/v1"
```

### portal

```yaml
apiVersion: v1
kind: Service
metadata:
  name: portal
spec:
  selector:
    app: portal
  ports:
    - name: http
      port: 3000
      targetPort: 3000
  type: ClusterIP
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: portal
spec:
  replicas: 2
  selector:
    matchLabels:
      app: portal
  template:
    metadata:
      labels:
        app: portal
    spec:
      containers:
        - name: portal
          image: rat-portal:latest
          ports:
            - containerPort: 3000
          env:
            # Internal API URL (cluster-internal, for SSR data fetching)
            - name: API_URL
              value: "http://ratd.rat.svc.cluster.local:8080"
            # Public API URL (browser-accessible, for client-side fetches)
            - name: NEXT_PUBLIC_API_URL
              value: "https://rat.example.com"
```

## DNS-Based Service Discovery

Kubernetes provides automatic DNS for Services:

| Service | DNS Name (within namespace `rat`) |
|---------|-----------------------------------|
| ratd | `ratd.rat.svc.cluster.local` |
| runner | `runner.rat.svc.cluster.local` |
| ratq | `ratq.rat.svc.cluster.local` |
| portal | `portal.rat.svc.cluster.local` |
| postgres | `postgres.rat.svc.cluster.local` |
| minio | `minio.rat.svc.cluster.local` |
| nessie | `nessie.rat.svc.cluster.local` |

No additional service discovery mechanism is needed. All RAT services resolve
each other via standard Kubernetes DNS.

## Ingress Configuration

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: rat-ingress
  annotations:
    nginx.ingress.kubernetes.io/proxy-body-size: "100m"
spec:
  rules:
    - host: rat.example.com
      http:
        paths:
          - path: /api
            pathType: Prefix
            backend:
              service:
                name: ratd
                port:
                  number: 8080
          - path: /health
            pathType: Prefix
            backend:
              service:
                name: ratd
                port:
                  number: 8080
          - path: /
            pathType: Prefix
            backend:
              service:
                name: portal
                port:
                  number: 3000
```

## Infrastructure Services

For production Kubernetes deployments, prefer managed services:

| Service | Recommended Managed Alternative |
|---------|--------------------------------|
| Postgres | Amazon RDS, Cloud SQL, Azure Database |
| MinIO | Amazon S3, Google Cloud Storage, Azure Blob |
| Nessie | Self-hosted (StatefulSet) or Dremio Cloud |

## Scaling Guidelines

| Service | Horizontal Scaling | Notes |
|---------|--------------------|-------|
| ratd | Yes (N replicas) | Advisory lock ensures single leader for background workers |
| runner | Yes (N replicas) | Each runner handles RUNNER_MAX_CONCURRENT runs |
| ratq | Limited (1-2) | Single DuckDB connection per instance; scale by namespace |
| portal | Yes (N replicas) | Stateless Next.js, scales freely |

## Health Check Endpoints

| Endpoint | Purpose | Used By |
|----------|---------|---------|
| `GET /health/live` | Process alive check | Kubernetes livenessProbe |
| `GET /health/ready` | Dependency readiness | Kubernetes readinessProbe |
| `GET /health` | Legacy (alias for live) | Docker Compose HEALTHCHECK |

## Secrets Management

Store credentials in Kubernetes Secrets and reference via `secretKeyRef`:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: rat-secrets
type: Opaque
stringData:
  database-url: "postgres://rat:secret@postgres:5432/rat?sslmode=require"
  s3-access-key: "..."
  s3-secret-key: "..."
  rat-api-key: "..."
```

For production, use an external secrets operator (e.g., External Secrets
Operator, HashiCorp Vault) instead of plaintext Secrets.
