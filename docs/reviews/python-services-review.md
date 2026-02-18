# Python Services Code Review — Runner & Query (ratq)

**Reviewer**: Senior Python Engineer
**Date**: 2026-02-16
**Scope**: All Python source files in `runner/` (15 modules) and `query/` (7 modules), plus tests and packaging
**Branch**: `feat/ratd-health-and-pipelines`

---

## Summary Statistics

| Metric | Runner | Query | Total |
|--------|--------|-------|-------|
| Source files | 15 | 7 | 22 |
| Test files | 16 | 6 | 22 |
| Findings | 30 | 12 | 42 |
| Critical | 2 | 1 | 3 |
| High | 8 | 2 | 10 |
| Medium | 11 | 5 | 16 |
| Low | 5 | 3 | 8 |
| Suggestion | 4 | 1 | 5 |

---

## 1. Security

### SEC-01: exec() sandbox escape via type() and metaclass chains
- **File:** `runner/src/rat_runner/python_exec.py`, lines 20-38, 106-126
- **Severity:** CRITICAL
- **What:** The Python pipeline sandbox blocks `getattr`, `setattr`, `dir`, and `eval`, but leaves `type`, `isinstance`, and `hasattr` available. An attacker can escape the sandbox via metaclass chains: `().__class__.__bases__[0].__subclasses__()`. Since `__class__` is an implicit attribute on every object and attribute access via `.` notation (as opposed to `getattr()`) is not blocked, the attacker can traverse the class hierarchy to reach `os._wrap_close` or `subprocess.Popen`.
- **Fix:** Use `RestrictedPython` or `asteval` instead of raw `exec()`. At minimum, add `type` to `_BLOCKED_BUILTINS` and implement an AST visitor that rejects attribute access to dunder attributes (`__class__`, `__bases__`, `__subclasses__`, `__globals__`, `__code__`, `__builtins__`). Consider running pipeline code in a separate subprocess with seccomp/AppArmor.

### SEC-02: SQL injection via unvalidated column names in Iceberg merge operations
- **File:** `runner/src/rat_runner/iceberg.py`, lines 163-188 (merge_iceberg), 277-285 (delete_insert_iceberg), 368-409 (scd2_iceberg), 467-474 (snapshot_iceberg)
- **Severity:** CRITICAL
- **What:** Column names from `unique_key` (a user-provided config tuple) are directly interpolated into SQL strings via f-strings: `f"d.{k} = e.{k}" for k in unique_key`. If a user sets `unique_key: ["id; DROP TABLE --"]` in their config.yaml, this becomes a SQL injection vector. Similarly, `watermark_column` in `read_watermark()` (line 514) and `partition_column` in `snapshot_iceberg()` (lines 469-470) are interpolated without validation.
- **Fix:** Add an identifier validation function and validate every column name before use:
  ```python
  def _quote_identifier(name: str) -> str:
      if not re.match(r"^[a-zA-Z_][a-zA-Z0-9_]*$", name):
          raise ValueError(f"Invalid column name: {name!r}")
      return f'"{name}"'
  ```

### SEC-03: Missing URL encoding for Nessie branch names
- **File:** `runner/src/rat_runner/nessie.py`, lines 57, 84, 98
- **Severity:** Medium
- **What:** Branch names are directly interpolated into URL paths without URL encoding. Branch names are currently derived from `run_id` (UUID-format), so the current risk is low, but if the naming convention changes, this becomes a URL injection vector.
- **Fix:** Use `urllib.parse.quote(branch_name, safe="")` for all URL path interpolation.

### SEC-04: Missing timeout on urlopen in catalog discovery
- **File:** `query/src/rat_query/catalog.py`, line 51
- **Severity:** CRITICAL
- **What:** The `urlopen()` call in `discover_tables()` does not specify a timeout. A non-responsive Nessie server will block the calling thread indefinitely, eventually exhausting the thread pool for catalog refresh.
- **Fix:** Add `timeout=10`:
  ```python
  with urlopen(req, timeout=10) as resp:
  ```

### SEC-05: metadata_location interpolated into SQL without validation
- **File:** `runner/src/rat_runner/iceberg.py`, lines 153-154
- **Severity:** Medium
- **What:** The `metadata_location` value from the PyIceberg catalog is directly interpolated into DuckDB SQL. A compromised or misconfigured Nessie server could return a crafted metadata path containing single quotes, leading to SQL injection.
- **Fix:** Escape single quotes: `safe_location = metadata_location.replace("'", "''")`

---

## 2. Code Quality

### CQ-01: Duplicated config classes across runner and query
- **File:** `runner/src/rat_runner/config.py` (lines 16-103) and `query/src/rat_query/config.py` (lines 9-75)
- **Severity:** High
- **What:** `S3Config`, `DuckDBConfig`, and `NessieConfig` are defined independently in both services with subtly different implementations. Runner's `S3Config` has `session_token` and `with_overrides()`, query's does not. Runner's `NessieConfig.base_url` returns `host + "/iceberg"` while query's returns just the host.
- **Fix:** Extract shared config classes into a common `rat_common` package.

### CQ-02: execute_pipeline() is a 270-line monolithic function
- **File:** `runner/src/rat_runner/executor.py`, lines 105-404
- **Severity:** High
- **What:** The function handles all 6 phases in a single function with deeply nested try/except blocks and duplicated success-path code (archive + maintenance appears identically at lines 351-360 and 371-380).
- **Fix:** Extract each phase into a named function. Extract the duplicated archive + maintenance block into `_post_success()`.

### CQ-03: maintenance.py duplicates catalog creation logic
- **File:** `runner/src/rat_runner/maintenance.py`, lines 31-41 and 86-96
- **Severity:** Medium
- **What:** Both `expire_snapshots()` and `remove_orphan_files()` create their own PyIceberg catalog via `load_catalog()`, duplicating `iceberg.get_catalog()`. The two implementations are slightly different (missing `s3.path-style-access`, `warehouse`, `prefix`).
- **Fix:** Reuse `iceberg.get_catalog()`.

### CQ-04: MergeStrategy should be an Enum
- **File:** `runner/src/rat_runner/models.py`, lines 69-87
- **Severity:** Medium
- **What:** `MergeStrategy` is a plain class with string constants instead of a proper `Enum`. Values are not type-safe, and typos go undetected.
- **Fix:** Convert to `StrEnum` (Python 3.11+). Update `PipelineConfig.merge_strategy` to use the `MergeStrategy` type.

### CQ-05: Duplicated DuckDB connection setup pattern
- **File:** `runner/src/rat_runner/engine.py` (lines 19-33) and `runner/src/rat_runner/iceberg.py` (lines 93-104) and `query/src/rat_query/engine.py` (lines 91-106)
- **Severity:** Medium
- **What:** S3 configuration on DuckDB connections is duplicated in three places with nearly identical code.
- **Fix:** Extract a shared `configure_duckdb_s3(conn, s3_config)` function.

### CQ-06: Broad except Exception clauses suppress useful errors
- **File:** `runner/src/rat_runner/iceberg.py`, lines 156, 272, 364, 463
- **Severity:** Medium
- **What:** Every merge operation silently falls back to loading the entire table into memory via PyIceberg on any exception. This hides genuine errors and can cause OOM for large tables.
- **Fix:** Catch only specific DuckDB/S3 exceptions. Log at warning level.

---

## 3. Type Safety

### TS-01: maintenance.py log parameter uses object | None with type: ignore
- **File:** `runner/src/rat_runner/maintenance.py`, lines 167, 181, 185, 189, 192, 196
- **Severity:** High
- **What:** The `log` parameter is typed as `object | None`, requiring `# type: ignore[union-attr]` on 6 lines.
- **Fix:** Define a `PipelineLogger` protocol in `models.py` with `info()`, `warn()`, `error()` methods. Use it instead of `object`.

### TS-02: _boto3_client return type annotation is incorrect
- **File:** `runner/src/rat_runner/config.py`, line 176
- **Severity:** Low
- **What:** Return type is `boto3.client` (a function, not a type). Should be `botocore.client.S3` or `object`.

### TS-03: server.py accesses private _lock attribute on RunState
- **File:** `runner/src/rat_runner/server.py`, line 180
- **Severity:** Medium
- **What:** `StreamLogs` directly accesses `run._lock`, breaking encapsulation.
- **Fix:** Add a public `get_logs_from(cursor)` method to `RunState`.

### TS-04: python_exec.py logger parameter typed as object | None
- **File:** `runner/src/rat_runner/python_exec.py`, line 72
- **Severity:** Low
- **What:** Same `object | None` pattern as TS-01.

---

## 4. Error Handling

### EH-01: nessie.py has no retry logic for HTTP calls
- **File:** `runner/src/rat_runner/nessie.py`, all functions
- **Severity:** High
- **What:** All Nessie REST API calls make single requests with no retry logic. Transient network failures immediately fail the pipeline.
- **Fix:** Add retry with exponential backoff for transient errors.

### EH-02: Bare except Exception: pass in executor cleanup
- **File:** `runner/src/rat_runner/executor.py`, lines 394-398
- **Severity:** Low
- **What:** Branch deletion in the finally block silently swallows all exceptions.
- **Fix:** Log the exception.

### EH-03: _evict_expired_runs iterates dict without thread safety
- **File:** `runner/src/rat_runner/server.py`, lines 81-91
- **Severity:** Medium
- **What:** `self._runs` is accessed concurrently from multiple methods without any lock.
- **Fix:** Add a lock around `_runs` access.

### EH-04: query server swallows exception details in ListTables
- **File:** `query/src/rat_query/server.py`, lines 198-204
- **Severity:** Low
- **What:** `count_rows` failures are silently caught with `pass`.
- **Fix:** Log at debug level.

---

## 5. DuckDB Usage

### DB-01: Each merge operation creates a new DuckDB connection
- **File:** `runner/src/rat_runner/iceberg.py`, lines 142, 261, 334, 353, 452
- **Severity:** High
- **What:** Five functions each create a new `duckdb.connect(":memory:")` with extension loading overhead.
- **Fix:** Accept an optional `DuckDBEngine` parameter and reuse its connection.

### DB-02: read_watermark loads entire table into memory
- **File:** `runner/src/rat_runner/iceberg.py`, lines 507-508
- **Severity:** High
- **What:** `table.scan().to_arrow()` reads the full table into memory just to compute `MAX(watermark_column)`.
- **Fix:** Use column projection: `table.scan(selected_fields=(watermark_column,)).to_arrow()` or use iceberg_scan.

### DB-03: explain_analyze uses f-string SQL formatting
- **File:** `runner/src/rat_runner/engine.py`, line 56
- **Severity:** Medium
- **What:** `f"EXPLAIN ANALYZE {sql}"` — while the SQL is already compiled/trusted, this is a code smell.
- **Fix:** Add a comment documenting that `sql` is already trusted, or validate it starts with SELECT/WITH.

### DB-04: preview_pipeline re-executes the full query 3 times
- **File:** `runner/src/rat_runner/preview.py`, lines 278-298
- **Severity:** Medium
- **What:** After executing with LIMIT, runs EXPLAIN ANALYZE on full SQL, then COUNT(*) on full SQL.
- **Fix:** Use a temp table or EXPLAIN on the limited query.

---

## 6. PyArrow / Iceberg

### IC-01: RecordBatchReader handling is repetitive
- **File:** `runner/src/rat_runner/iceberg.py`, 5 locations
- **Severity:** Medium
- **What:** The same `isinstance(arrow_result, pa.RecordBatchReader)` check is duplicated 5 times.
- **Fix:** Extract a `_to_arrow_table()` utility.

### IC-02: scd2_iceberg does not deduplicate new_data on unique_key
- **File:** `runner/src/rat_runner/iceberg.py`, lines 404-408
- **Severity:** Medium
- **What:** Unlike `merge_iceberg()`, `scd2_iceberg()` does not dedup new_data on the unique_key before inserting, potentially violating the SCD2 contract.
- **Fix:** Add deduplication before the UNION ALL.

### IC-03: Namespace splitting is non-obvious
- **File:** `runner/src/rat_runner/iceberg.py`, lines 75-77 (repeated 5 times)
- **Severity:** Low
- **What:** `table_name.rsplit(".", 1)` works correctly but the intent is fragile. Use explicit 3-part splitting.

---

## 7. gRPC Server

### GR-01: StreamLogs uses polling instead of condition variables
- **File:** `runner/src/rat_runner/server.py`, lines 178-198
- **Severity:** High
- **What:** Polls every 500ms using `time.sleep(0.5)`. Wastes CPU and adds latency.
- **Fix:** Add a `threading.Condition` to `RunState` and wait on it instead of sleeping.

### GR-02: gRPC pool size is hardcoded
- **File:** `runner/src/rat_runner/server.py`, line 374
- **Severity:** Low
- **What:** `ThreadPoolExecutor(max_workers=10)` is not configurable.
- **Fix:** Use an environment variable.

### GR-03: SubmitPipeline does not handle thread pool exhaustion
- **File:** `runner/src/rat_runner/server.py`, line 139
- **Severity:** Medium
- **What:** No backpressure when all workers are busy. Queue grows unbounded.
- **Fix:** Track active runs and return RESOURCE_EXHAUSTED when the limit is reached.

---

## 8. Testing

### TT-01: No tests for catalog.py urlopen timeout handling
- **File:** `query/tests/unit/test_catalog.py`
- **Severity:** Medium

### TT-02: No tests for query engine thread safety
- **File:** `query/tests/unit/test_engine.py`
- **Severity:** Low

### TT-03: No integration tests for either service
- **Severity:** Medium
- **Fix:** Add integration tests using testcontainers-python.

### TT-04: preview.py has unused parameters (sample_files, env)
- **File:** `runner/src/rat_runner/preview.py`, lines 71-72
- **Severity:** Low

---

## 9. Performance

### PF-01: lru_cache on _boto3_client could leak with STS credentials
- **File:** `runner/src/rat_runner/config.py`, line 175
- **Severity:** Medium

### PF-02: validate_landing_zones issues sequential S3 LIST per zone
- **File:** `runner/src/rat_runner/templating.py`, lines 180-197
- **Severity:** Low

### PF-03: ListTables calls count_rows for every table
- **File:** `query/src/rat_query/server.py`, lines 198-203
- **Severity:** Medium

---

## 10. Configuration

### CF-01: NessieConfig.base_url behaves differently between runner and query
- **File:** Both config.py files
- **Severity:** High
- **What:** Runner's returns `host + "/iceberg"`, query's returns just the host. Same property name, different semantics.

### CF-02: parse_pipeline_config does not validate merge_strategy values
- **File:** `runner/src/rat_runner/config.py`, line 124
- **Severity:** Medium

### CF-03: DuckDBConfig.from_env does not validate threads value
- **File:** Both config.py files
- **Severity:** Low

---

## 11. Templating

### TM-01: _resolve_ref silently swallows all exceptions during catalog lookup
- **File:** `runner/src/rat_runner/templating.py`, lines 166-177
- **Severity:** Medium
- **What:** The `_resolve_ref()` function catches all exceptions from the catalog lookup and falls back to a directory-based path. This hides misconfiguration and makes debugging difficult.
- **Fix:** Log the exception at warning level:
  ```python
  except Exception as e:
      logger.warning("Failed to resolve ref '%s' via catalog, using fallback: %s", table_ref, e)
  ```

### TM-02: validate_template bare function detection has edge cases
- **File:** `runner/src/rat_runner/templating.py`, lines 225-234
- **Severity:** Low
- **What:** The bare function call detection (warning when `ref()` is used outside `{{ }}`) can produce false positives for SQL comments containing `ref()` examples, and false negatives for `ref()` inside `{% %}` blocks.
- **Fix:** Improve the detection to skip SQL comments and recognize `{% %}` blocks as valid Jinja contexts.

---

## 12. Dependencies

### DP-01: grpcio-tools is a runtime dependency in query's pyproject.toml
- **File:** `query/pyproject.toml`, line 10
- **Severity:** Medium
- **What:** `grpcio-tools>=1.65` is listed under `[project.dependencies]` (runtime) instead of `[project.optional-dependencies.dev]`. This pulls in the protobuf compiler and all its dependencies into the production Docker image unnecessarily.
- **Fix:** Move to dev dependencies.

### DP-02: s3fs dependency is declared but never imported
- **File:** `runner/pyproject.toml`, line 14
- **Severity:** Low
- **What:** `s3fs>=2024.6` is listed as a runtime dependency, but no file in `runner/src/rat_runner/` imports `s3fs`.
- **Fix:** Verify whether `pyiceberg` requires `s3fs` as a transitive dependency. If not, remove from `pyproject.toml`.

### DP-03: boto3 is declared but not used in query service
- **File:** `query/pyproject.toml`, line 11
- **Severity:** Low
- **What:** `boto3>=1.35` is in query's runtime dependencies, but no file in `query/src/rat_query/` imports `boto3`.
- **Fix:** Remove if truly unused.

---

## 13. Logging

### LG-01: Compiled SQL is logged at INFO level, potentially exposing data
- **File:** `runner/src/rat_runner/executor.py`, line 238
- **Severity:** Medium
- **What:** The fully compiled SQL (with resolved S3 paths) is logged at INFO level. While S3 paths typically do not contain secrets, any Jinja variable that resolves to sensitive data would be visible in logs.
- **Fix:** Log compiled SQL at DEBUG level.

### LG-02: Inconsistent logger naming conventions
- **File:** Various
- **Severity:** Low
- **What:** Logger names are inconsistent: runner uses both `"rat.runner"` and `__name__` patterns.
- **Fix:** Standardize on `logging.getLogger(__name__)` everywhere.

---

## 14. Resource Management

### RM-01: No query timeout in QueryEngine
- **File:** `query/src/rat_query/engine.py`, lines 167-188
- **Severity:** High
- **What:** `QueryEngine.query_arrow()` executes user-provided SQL without any timeout. A malicious or poorly-written query can consume all available memory and CPU indefinitely.
- **Fix:** DuckDB supports statement timeouts:
  ```python
  def query_arrow(self, sql: str, limit: int = 1000, timeout_seconds: int = 30) -> pa.Table:
      self._conn.execute(f"SET statement_timeout = '{timeout_seconds}s'")
      try:
          # execute query...
      finally:
          self._conn.execute("SET statement_timeout = '0s'")
  ```

### RM-02: DuckDBEngine.conn property is not thread-safe
- **File:** `runner/src/rat_runner/engine.py`, lines 35-39
- **Severity:** Low
- **What:** The lazy connection creation in the `conn` property is not protected by a lock. In practice, each `DuckDBEngine` is created per-run, so concurrent access is unlikely.
- **Fix:** Add a comment documenting the single-threaded assumption.

### RM-03: Catalog refresh thread is not joined on shutdown
- **File:** `query/src/rat_query/server.py`, lines 217-220
- **Severity:** Low
- **What:** `shutdown()` sets the stop event but does not join the refresh thread. The engine is closed while the refresh thread might still be running.
- **Fix:** Join the thread before closing the engine:
  ```python
  def shutdown(self) -> None:
      self._refresh_stop.set()
      self._refresh_thread.join(timeout=5)
      self._engine.close()
  ```

---

## Prioritized Action Plan

### Immediate (before next release)
1. **SEC-04** — Add timeout to catalog `urlopen()` (1-line fix)
2. **SEC-02** — Validate column names in Iceberg merge operations
3. **SEC-01** — Harden Python exec sandbox (AST visitor or RestrictedPython)
4. **CF-02** — Validate merge_strategy values in config parsing
5. **DP-01** — Move grpcio-tools to dev dependencies

### Short-term (next sprint)
6. **CQ-02** — Break up `execute_pipeline()` into phase functions
7. **TS-01** — Define PipelineLogger protocol, eliminate type: ignore
8. **DB-02** — Fix `read_watermark()` to use column projection
9. **RM-01** — Add query timeout to QueryEngine
10. **EH-01** — Add retry logic to Nessie HTTP calls
11. **GR-01** — Replace StreamLogs polling with condition variables

### Medium-term (next milestone)
12. **CQ-01** — Extract shared config into common package
13. **CQ-04** — Convert MergeStrategy to StrEnum
14. **TT-03** — Add integration tests with testcontainers
15. **DB-01** — Reuse DuckDB connections in Iceberg merge operations
16. **CQ-05** — Unify DuckDB S3 configuration function
17. **CQ-03** — Reuse get_catalog() in maintenance.py

### Low priority (backlog)
18. **SEC-03** — URL-encode branch names
19. **SEC-05** — Escape metadata_location in SQL
20. All remaining Low/Suggestion items

---

*Review generated on 2026-02-16. All file paths are relative to the repository root.*
