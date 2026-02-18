# Entity Relationship Diagram

> Complete DAG of all RAT v2 entities, their relationships, and data flows.

## Entity Relationship Diagram

```mermaid
erDiagram
    %% ═══════════════════════════════════════════
    %% CORE PLATFORM (Postgres)
    %% ═══════════════════════════════════════════

    Namespace {
        uuid id PK
        varchar name UK "unique slug"
        text description
        varchar created_by "null in Community"
        timestamptz created_at
    }

    Pipeline {
        uuid id PK
        varchar namespace FK "→ namespaces.name"
        varchar layer "bronze | silver | gold"
        varchar name
        varchar type "sql | python"
        varchar s3_path "S3 prefix for code"
        text description
        varchar owner "null in Community"
        jsonb published_versions "file → S3 version ID"
        boolean draft_dirty "draft ≠ published"
        int max_versions "default 50"
        timestamptz published_at
        timestamptz deleted_at "soft delete"
    }

    PipelineVersion {
        uuid id PK
        uuid pipeline_id FK "→ pipelines.id"
        int version_number
        text message "publish commit message"
        jsonb published_versions "snapshot of versions"
        timestamptz created_at
    }

    Run {
        uuid id PK
        uuid pipeline_id FK "→ pipelines.id"
        varchar status "pending → running → success | failed | cancelled"
        varchar trigger "manual | schedule:cron | trigger:lz_upload:ns/zone"
        timestamptz started_at
        timestamptz finished_at
        int duration_ms
        bigint rows_written
        text error
        varchar logs_s3_path "full logs in S3"
        jsonb logs "recent logs (JSONB)"
        jsonb phase_profiles "execution phase timings"
        timestamptz created_at
    }

    Schedule {
        uuid id PK
        uuid pipeline_id FK "→ pipelines.id"
        varchar cron_expr "5-field cron"
        boolean enabled
        uuid last_run_id FK "→ runs.id"
        timestamptz last_run_at
        timestamptz next_run_at
    }

    PipelineTrigger {
        uuid id PK
        uuid pipeline_id FK "→ pipelines.id"
        varchar type "landing_zone_upload"
        jsonb config "namespace + zone_name"
        boolean enabled
        int cooldown_seconds
        uuid last_run_id FK "→ runs.id"
        timestamptz last_triggered_at
    }

    %% ═══════════════════════════════════════════
    %% QUALITY TESTING
    %% ═══════════════════════════════════════════

    QualityTest {
        uuid id PK
        uuid pipeline_id FK "→ pipelines.id"
        varchar name
        text description "from @description annotation"
        varchar severity "error | warn"
        varchar s3_path "test SQL in S3"
        string tags "from @tags annotation"
        string remediation "from @remediation annotation"
        timestamptz created_at
    }

    QualityResult {
        uuid id PK
        uuid test_id FK "→ quality_tests.id"
        uuid run_id FK "→ runs.id"
        varchar status "passed | failed | warned | error"
        numeric value "violation count"
        int duration_ms
        timestamptz ran_at
    }

    %% ═══════════════════════════════════════════
    %% LANDING ZONES
    %% ═══════════════════════════════════════════

    LandingZone {
        uuid id PK
        varchar namespace FK "→ namespaces.name"
        varchar name
        text description
        varchar owner
        text expected_schema "schema documentation"
        timestamptz created_at
    }

    LandingFile {
        uuid id PK
        uuid zone_id FK "→ landing_zones.id"
        varchar filename
        varchar s3_path
        bigint size_bytes
        varchar content_type
        timestamptz uploaded_at
    }

    %% ═══════════════════════════════════════════
    %% TABLE DOCUMENTATION (metadata overlay)
    %% ═══════════════════════════════════════════

    TableMetadata {
        uuid id PK
        varchar namespace "logical ref (not FK)"
        varchar layer "bronze | silver | gold"
        varchar name
        text description
        varchar owner
        jsonb column_descriptions "col → desc map"
        timestamptz updated_at
    }

    %% ═══════════════════════════════════════════
    %% ICEBERG DATA LAYER (Nessie + S3)
    %% ═══════════════════════════════════════════

    IcebergTable {
        string namespace "from Nessie catalog"
        string layer "branch prefix"
        string name "table identifier"
        bigint row_count "from Iceberg metadata"
        bigint size_bytes "total Parquet size"
    }

    IcebergColumn {
        string name
        string type "INTEGER | VARCHAR | TIMESTAMP | ..."
        boolean nullable
    }

    %% ═══════════════════════════════════════════
    %% PRO ONLY (reserved, not yet implemented)
    %% ═══════════════════════════════════════════

    Plugin {
        varchar name PK
        varchar slot "auth | sharing | executor | cloud"
        varchar image "container image"
        varchar status "healthy | degraded | down"
        jsonb config
    }

    AuditLog {
        uuid id PK
        varchar user_id
        varchar action "pipeline.create | run.trigger | ..."
        varchar resource
        text detail
        timestamptz created_at
    }

    %% ═══════════════════════════════════════════
    %% RELATIONSHIPS
    %% ═══════════════════════════════════════════

    Namespace ||--o{ Pipeline : "contains"
    Namespace ||--o{ LandingZone : "contains"

    Pipeline ||--o{ Run : "executes"
    Pipeline ||--o{ Schedule : "scheduled by"
    Pipeline ||--o{ PipelineVersion : "versioned as"
    Pipeline ||--o{ PipelineTrigger : "triggered by"
    Pipeline ||--o{ QualityTest : "validated by"

    Run ||--o{ QualityResult : "produces"
    QualityTest ||--o{ QualityResult : "evaluated in"

    Schedule ||--o| Run : "last run"
    PipelineTrigger ||--o| Run : "last run"

    LandingZone ||--o{ LandingFile : "receives"
    PipelineTrigger }o--|| LandingZone : "watches (via JSONB)"

    IcebergTable ||--o{ IcebergColumn : "has columns"
    TableMetadata ||--o| IcebergTable : "documents"
```

## Data Flow DAG

```mermaid
flowchart TB
    subgraph USER["User Actions"]
        direction LR
        UPLOAD["Upload File"]
        MANUAL["Manual Run"]
        PUBLISH["Publish Pipeline"]
        EDIT_DOCS["Edit Documentation"]
    end

    subgraph TRIGGERS["Trigger Layer"]
        direction LR
        CRON["Schedule\n(cron evaluator, 30s tick)"]
        LZ_TRIGGER["Landing Zone Trigger\n(file upload event)"]
    end

    subgraph EXECUTION["Execution Layer"]
        direction TB
        CREATE_RUN["Create Run\n(status: pending)"]
        EXECUTOR["WarmPool Executor\n(ConnectRPC → Runner)"]

        subgraph RUNNER["Runner (Python)"]
            direction TB
            BRANCH["1. Create Nessie Branch"]
            COMPILE["2. Compile SQL (Jinja)"]
            WRITE["3. DuckDB Write → Iceberg"]
            QUALITY["4. Run Quality Tests"]
            MERGE["5. Merge Branch → main"]
        end
    end

    subgraph STORAGE["Storage Layer"]
        direction LR
        PG[("Postgres\n(metadata)")]
        S3[("MinIO / S3\n(files + data)")]
        NESSIE[("Nessie\n(Iceberg catalog)")]
    end

    subgraph DATA["Data Layer"]
        direction LR
        BRONZE["Bronze Tables\n(raw ingestion)"]
        SILVER["Silver Tables\n(cleaned / joined)"]
        GOLD["Gold Tables\n(aggregated / reporting)"]
    end

    %% User triggers
    UPLOAD --> LZ_TRIGGER
    MANUAL --> CREATE_RUN
    PUBLISH --> PG

    %% Automated triggers
    CRON -- "evaluates cron_expr" --> CREATE_RUN
    LZ_TRIGGER -- "cooldown check" --> CREATE_RUN

    %% Execution flow
    CREATE_RUN -- "status: pending" --> PG
    CREATE_RUN --> EXECUTOR
    EXECUTOR -- "gRPC SubmitPipeline" --> BRANCH

    BRANCH -- "Nessie REST API" --> NESSIE
    BRANCH --> COMPILE
    COMPILE -- "ref() → iceberg_scan()" --> WRITE
    WRITE -- "PyIceberg + Parquet" --> S3
    WRITE --> QUALITY
    QUALITY -- "0 violations = pass" --> MERGE
    MERGE -- "Nessie merge" --> NESSIE

    %% Quality can block
    QUALITY -. "severity:error + fail\n→ run fails" .-> PG

    %% Results flow
    MERGE -- "status: success" --> PG
    WRITE --> BRONZE
    WRITE --> SILVER
    WRITE --> GOLD

    %% Medallion flow
    BRONZE -- "ref('bronze.X')" --> SILVER
    SILVER -- "ref('silver.X')" --> GOLD

    %% Documentation (standalone)
    EDIT_DOCS --> PG

    %% Styling
    classDef user fill:#1a1a2e,stroke:#00ff41,color:#00ff41
    classDef trigger fill:#1a1a2e,stroke:#e5c07b,color:#e5c07b
    classDef exec fill:#1a1a2e,stroke:#61afef,color:#61afef
    classDef storage fill:#16213e,stroke:#c678dd,color:#c678dd
    classDef data fill:#1a1a2e,stroke:#98c379,color:#98c379

    class UPLOAD,MANUAL,PUBLISH,EDIT_DOCS user
    class CRON,LZ_TRIGGER trigger
    class CREATE_RUN,EXECUTOR,BRANCH,COMPILE,WRITE,QUALITY,MERGE exec
    class PG,S3,NESSIE storage
    class BRONZE,SILVER,GOLD data
```

## Pipeline Lifecycle

```mermaid
stateDiagram-v2
    [*] --> Draft: create pipeline

    Draft --> Draft: edit code (S3)
    Draft --> Published: publish (snapshot versions)

    Published --> Draft: edit code → draft_dirty=true
    Published --> Published: rollback to version N
    Published --> Runnable: schedule / trigger / manual

    Runnable --> Pending: create run
    Pending --> Running: executor dispatches to runner
    Running --> Success: all phases complete + quality pass
    Running --> Failed: SQL error / quality error-severity fail
    Running --> Cancelled: user cancels

    state Running {
        [*] --> BranchCreate
        BranchCreate --> SQLCompile
        SQLCompile --> DuckDBWrite
        DuckDBWrite --> QualityTests
        QualityTests --> NessieMerge: all pass / warn only
        QualityTests --> [*]: error-severity fail
        NessieMerge --> [*]
    }

    Success --> [*]
    Failed --> [*]
    Cancelled --> [*]
```

## Entity Counts & Storage Summary

| Entity | Storage | Cardinality | Notes |
|--------|---------|-------------|-------|
| **Namespace** | Postgres | 1 (Community) / N (Pro) | Top-level grouping |
| **Pipeline** | Postgres + S3 | Many per namespace | Code in S3, metadata in PG |
| **PipelineVersion** | Postgres | Many per pipeline | Immutable version snapshots |
| **Run** | Postgres + S3 | Many per pipeline | Logs in S3, status in PG |
| **Schedule** | Postgres | 0-N per pipeline | Cron-based triggers |
| **PipelineTrigger** | Postgres | 0-N per pipeline | Event-driven (LZ uploads) |
| **QualityTest** | Postgres + S3 | 0-N per pipeline | SQL in S3, metadata in PG |
| **QualityResult** | Postgres | 1 per test per run | Updated after each run |
| **LandingZone** | Postgres | Many per namespace | File drop areas |
| **LandingFile** | Postgres + S3 | Many per zone | Content in S3 |
| **TableMetadata** | Postgres | 0-1 per Iceberg table | User-maintained docs |
| **Iceberg Table** | Nessie + S3 | Dynamic | Created by pipeline runs |
| **Plugin** | Postgres | 0-N (Pro only) | Plugin registry |
| **AuditLog** | Postgres | Append-only (Pro) | Action log |

## Key Design Patterns

1. **Dual Storage** — Metadata in Postgres, data/code/logs in S3, catalog in Nessie
2. **Medallion Architecture** — Bronze → Silver → Gold via `ref()` dependency chains
3. **Git-like Versioning** — Nessie branches for atomic writes, pipeline versions for rollback
4. **Event-driven Triggers** — Landing zone uploads fire pipeline runs via JSONB config matching
5. **Quality Gates** — Tests with `severity: error` block pipeline completion on failure
6. **Soft References** — Triggers → LandingZones via JSONB, TableMetadata → IcebergTable via name
