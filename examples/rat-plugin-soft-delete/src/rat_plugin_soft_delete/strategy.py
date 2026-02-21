"""Soft-delete merge strategy.

Instead of physically removing rows that are no longer present in the incoming
data, this strategy marks them with a `_deleted_at` timestamp. Useful for
audit trails and regulatory compliance.

Behavior:
  1. Requires `unique_key` in config (falls back to full_refresh without it).
  2. Loads the existing table from the Iceberg catalog.
  3. Uses DuckDB to ANTI JOIN incoming vs existing to find "missing" rows.
  4. Adds `_deleted_at = current_timestamp` to missing rows.
  5. UNION ALL: incoming rows (_deleted_at = NULL) + soft-deleted rows +
     previously soft-deleted rows (preserved).
  6. Overwrites via write_iceberg().
"""

from __future__ import annotations

import logging
from datetime import UTC, datetime
from typing import TYPE_CHECKING

import duckdb
import pyarrow as pa

from rat_runner.iceberg import get_catalog, write_iceberg

if TYPE_CHECKING:
    from rat_runner.config import NessieConfig, S3Config
    from rat_runner.models import PipelineConfig

logger = logging.getLogger(__name__)


class SoftDeleteStrategy:
    """Merge strategy that soft-deletes missing rows with a _deleted_at timestamp."""

    @property
    def name(self) -> str:
        return "soft_delete"

    def execute(
        self,
        data: pa.Table,
        table_name: str,
        s3_config: S3Config,
        nessie_config: NessieConfig,
        location: str,
        config: PipelineConfig | None,
        branch: str = "main",
        conn: duckdb.DuckDBPyConnection | None = None,
    ) -> int:
        if not config or not config.unique_key:
            logger.warning(
                "soft_delete requires unique_key — falling back to full_refresh for %s",
                table_name,
            )
            return write_iceberg(
                data,
                table_name,
                s3_config,
                nessie_config,
                location,
                branch=branch,
            )

        unique_key = config.unique_key

        # Try to load existing table from catalog.
        try:
            catalog = get_catalog(s3_config, nessie_config, branch=branch)
            ns, layer, tbl = table_name.split(".")
            iceberg_table = catalog.load_table(f"{ns}.{layer}.{tbl}")
            existing = iceberg_table.scan().to_arrow()
        except Exception:
            # Table doesn't exist yet — first run, just write with _deleted_at = NULL.
            logger.info("No existing table for %s — writing initial data", table_name)
            null_col = pa.array([None] * len(data), type=pa.timestamp("us", tz="UTC"))
            result = data.append_column("_deleted_at", null_col)
            return write_iceberg(
                result,
                table_name,
                s3_config,
                nessie_config,
                location,
                branch=branch,
            )

        local_conn = conn or duckdb.connect()
        try:
            local_conn.register("incoming", data)
            local_conn.register("existing", existing)

            # Build the join condition on unique_key columns.
            join_cond = " AND ".join(f"e.{k} = i.{k}" for k in unique_key)

            # Find rows in existing that are NOT in incoming (newly deleted).
            newly_deleted = local_conn.execute(f"""
                SELECT e.*
                FROM existing e
                LEFT JOIN incoming i ON {join_cond}
                WHERE i.{unique_key[0]} IS NULL
                  AND (e._deleted_at IS NULL)
            """).fetch_arrow_table()

            # Add _deleted_at timestamp to newly deleted rows.
            now = datetime.now(UTC)
            deleted_at_col = pa.array(
                [now] * len(newly_deleted), type=pa.timestamp("us", tz="UTC")
            )
            # Drop existing _deleted_at and replace with the new timestamp.
            if "_deleted_at" in newly_deleted.column_names:
                newly_deleted = newly_deleted.drop("_deleted_at")
            newly_deleted = newly_deleted.append_column("_deleted_at", deleted_at_col)

            # Preserve previously soft-deleted rows.
            previously_deleted = local_conn.execute("""
                SELECT * FROM existing WHERE _deleted_at IS NOT NULL
            """).fetch_arrow_table()

            # Normalize _deleted_at type across all tables (DuckDB may use Etc/UTC
            # while PyArrow uses UTC — they're semantically identical but Arrow
            # treats them as different types during concat).
            ts_type = pa.timestamp("us", tz="UTC")

            null_col = pa.array([None] * len(data), type=ts_type)
            incoming_with_col = data.append_column("_deleted_at", null_col)

            tables = [incoming_with_col, newly_deleted, previously_deleted]
            unified = []
            for tbl in tables:
                if len(tbl) == 0:
                    unified.append(tbl.cast(incoming_with_col.schema))
                    continue
                col = tbl.column("_deleted_at")
                if col.type != ts_type:
                    tbl = tbl.set_column(
                        tbl.schema.get_field_index("_deleted_at"),
                        "_deleted_at",
                        col.cast(ts_type),
                    )
                unified.append(tbl)

            # UNION ALL: incoming + newly deleted + previously deleted.
            result = pa.concat_tables(unified)
        finally:
            if conn is None:
                local_conn.close()

        return write_iceberg(
            result,
            table_name,
            s3_config,
            nessie_config,
            location,
            branch=branch,
        )
