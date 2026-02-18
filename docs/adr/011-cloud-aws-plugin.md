# ADR-011: Cloud Plugin — AWS STS + ECS Fargate (v2.8)

## Status: Accepted

## Context

The community edition runs on a single host with MinIO for S3 and a warm-pool
runner (or Podman container-per-run in Pro) for pipeline execution. This works
for self-hosting but doesn't address cloud deployments where users want:

- **Real AWS S3** instead of MinIO
- **Serverless compute** (ECS Fargate) instead of local containers
- **Per-namespace credential scoping** via STS for security isolation

The plugin system (ADR-007) already defines extension points for `executor` and
a new `cloud` plugin slot. The runner already supports S3 overrides via
`SubmitRequest.s3_config` (ADR-009). This ADR adds the first cloud provider
implementation.

## Decision

### New `CloudService` proto

A new `cloud/v1/cloud.proto` defines `CloudService.GetCredentials` — separated
from `enforcement.proto` (which owns `CanAccess`). This clean boundary means
credential vending and access enforcement are independent concerns.

```protobuf
service CloudService {
  rpc GetCredentials(GetCredentialsRequest) returns (GetCredentialsResponse);
}
```

### Single `cloud-aws` plugin container

Both `CloudService` and `ExecutorService` are implemented in one Go binary.
ConnectRPC routes by service path, so both services run on the same port (50090).

```
ratd (Go)                           cloud-aws plugin (Pro)
+----------------------+            +------------------------------+
| CloudProvider        |            | CloudService                 |
|  GetCredentials()    |--ConnectRPC|  GetCredentials()  [STS]     |
|                      |            |                              |
| PluginExecutor       |            | ExecutorService              |
|  Submit()            |--ConnectRPC|  Submit()     [ECS RunTask]  |
|  GetRunStatus()      |            |  GetRunStatus() [Describe]   |
|  StreamLogs()        |            |  StreamLogs() [CW Logs]      |
|  Cancel()            |            |  Cancel()    [ECS StopTask]  |
+----------------------+            +------------------------------+
                                    AWS: STS + ECS Fargate + CloudWatch
```

### Credential flow (per run)

1. User triggers run → `HandleCreateRun`
2. ratd calls `CloudService.GetCredentials(userID, namespace)`
3. cloud-aws calls STS `AssumeRole` with inline scoped policy
4. ratd sets `run.S3Overrides = {access_key, secret_key, session_token}`
5. `PluginExecutor.Submit()` passes `s3_config` on the proto request
6. cloud-aws's ECS executor: `RunTask` with S3 env overrides
7. Runner reads `S3_SESSION_TOKEN` → DuckDB + PyIceberg + boto3 use STS creds

### STS scoping via inline policy

Each `AssumeRole` call includes an inline IAM policy restricting S3 access to
the namespace prefix:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["s3:GetObject", "s3:PutObject", "s3:DeleteObject"],
      "Resource": "arn:aws:s3:::BUCKET/NAMESPACE/*"
    },
    {
      "Effect": "Allow",
      "Action": ["s3:ListBucket"],
      "Resource": "arn:aws:s3:::BUCKET",
      "Condition": {"StringLike": {"s3:prefix": "NAMESPACE/*"}}
    }
  ]
}
```

This gives each run the minimum S3 permissions for its namespace.

### ECS Fargate executor

Pipeline runs are dispatched as ECS Fargate tasks using the same runner container
image. The executor:

- **Submit**: `ecs.RunTask` with env overrides (RUN_MODE=single, S3 creds, run params)
- **GetRunStatus**: `ecs.DescribeTasks` → maps ECS status to proto RunStatus
- **StreamLogs**: CloudWatch `GetLogEvents` (poll-based, 2s interval)
- **Cancel**: `ecs.StopTask`

### Nessie stays self-hosted

We keep Nessie as the Iceberg catalog (self-hosted on ECS or EC2). Nessie provides
git-like branching which AWS Glue lacks. The runner connects to Nessie via
`NESSIE_URL` injected by the executor.

## Consequences

### Positive

- **No AWS Glue dependency** — Nessie provides better branching semantics
- **Namespace isolation** via STS inline policies — runs can't access other namespaces' data
- **Same runner image** works locally (MinIO) and on AWS (S3) — unified execution model
- **Compose overlay** — `docker-compose.aws.yml` adds cloud-aws without modifying community stack
- **Clean separation** — cloud credential vending is independent from auth (Keycloak) and ACL

### Negative

- **In-memory run tracking** — ECS task ARN → run ID mapping is in-memory; plugin restart loses state
- **CloudWatch polling** — log streaming polls every 2s (no push-based log tailing)
- **No Lambda support** — deferred to v2.9 (Fargate covers most use cases)

### Future Work

- **v2.9**: Lambda executor for sub-second cold starts on small pipelines
- **Persistent run state**: Move run tracking to DynamoDB or Postgres instead of in-memory map
- **Cost controls**: Auto-stop idle ECS tasks, budget alerts via CloudWatch
