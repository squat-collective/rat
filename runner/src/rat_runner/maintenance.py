"""Iceberg table maintenance — snapshot expiry and orphan file removal.

Best-effort: failures are logged, never raised. Called after successful pipeline
runs to keep Iceberg tables clean without manual intervention.
"""

from __future__ import annotations

import logging
from datetime import UTC, datetime, timedelta
from typing import TYPE_CHECKING

from rat_runner.iceberg import get_catalog

if TYPE_CHECKING:
    from rat_runner.config import NessieConfig, S3Config
    from rat_runner.models import PipelineLogger

logger = logging.getLogger(__name__)


def expire_snapshots(
    table_name: str,
    max_age_days: int,
    s3_config: S3Config,
    nessie_config: NessieConfig,
) -> int:
    """Expire Iceberg snapshots older than max_age_days.

    Uses PyIceberg's manage_snapshots() to expire old snapshots.
    Returns the number of snapshots expired, or 0 on failure.
    """
    try:
        catalog = get_catalog(s3_config, nessie_config)

        # Parse table_name: "namespace.layer.name" → ("namespace", "layer.name")
        parts = table_name.split(".", 1)
        if len(parts) != 2:
            logger.warning(f"maintenance: invalid table name format: {table_name}")
            return 0

        table = catalog.load_table(table_name)
        cutoff = datetime.now(UTC) - timedelta(days=max_age_days)
        cutoff_ms = int(cutoff.timestamp() * 1000)

        snapshots_before = len(table.metadata.snapshots)
        table.manage_snapshots().expire_snapshots_older_than(cutoff_ms).commit()

        # Reload to count remaining
        table = catalog.load_table(table_name)
        snapshots_after = len(table.metadata.snapshots)
        expired = snapshots_before - snapshots_after

        if expired > 0:
            logger.info(f"maintenance: expired {expired} snapshot(s) from {table_name}")
        return max(expired, 0)

    except Exception as e:
        logger.warning(f"maintenance: failed to expire snapshots for {table_name}: {e}")
        return 0


def remove_orphan_files(
    table_name: str,
    max_age_days: int,
    s3_config: S3Config,
    nessie_config: NessieConfig,
) -> int:
    """Remove orphan data files from S3 that are no longer referenced by any snapshot.

    Compares files in the table's S3 location against files referenced in snapshots.
    Deletes files older than max_age_days that are not in any snapshot's manifest.
    Returns the number of files removed, or 0 on failure.
    """
    try:
        import boto3

        catalog = get_catalog(s3_config, nessie_config)

        table = catalog.load_table(table_name)

        # Collect all referenced data files from all snapshots
        referenced_files: set[str] = set()
        for snapshot in table.metadata.snapshots:
            try:
                scan = table.scan(snapshot_id=snapshot.snapshot_id)
                for task in scan.plan_files():
                    referenced_files.add(task.file.file_path)
            except Exception:
                pass  # Skip snapshots we can't read

        if not referenced_files:
            return 0

        # List actual S3 files in the table's data location
        location = table.metadata.location
        if not location:
            return 0

        # Parse S3 location: s3://bucket/prefix/
        bucket = s3_config.bucket
        prefix = location.replace(f"s3://{bucket}/", "").rstrip("/") + "/data/"

        s3_client = boto3.client(
            "s3",
            endpoint_url=s3_config.endpoint_url,
            aws_access_key_id=s3_config.access_key,
            aws_secret_access_key=s3_config.secret_key,
        )

        cutoff = datetime.now(UTC) - timedelta(days=max_age_days)
        orphans: list[str] = []

        paginator = s3_client.get_paginator("list_objects_v2")
        for page in paginator.paginate(Bucket=bucket, Prefix=prefix):
            for obj in page.get("Contents", []):
                key = obj["Key"]
                s3_path = f"s3://{bucket}/{key}"
                last_modified = obj.get("LastModified", datetime.now(UTC))

                if s3_path not in referenced_files and last_modified < cutoff:
                    orphans.append(key)

        if not orphans:
            return 0

        # Delete orphans in batches of 1000
        for i in range(0, len(orphans), 1000):
            batch = orphans[i : i + 1000]
            s3_client.delete_objects(
                Bucket=bucket,
                Delete={"Objects": [{"Key": k} for k in batch]},
            )

        logger.info(f"maintenance: removed {len(orphans)} orphan file(s) from {table_name}")
        return len(orphans)

    except Exception as e:
        logger.warning(f"maintenance: failed to remove orphan files for {table_name}: {e}")
        return 0


def run_maintenance(
    table_name: str,
    s3_config: S3Config,
    nessie_config: NessieConfig,
    snapshot_max_age_days: int = 7,
    orphan_max_age_days: int = 3,
    log: PipelineLogger | None = None,
) -> None:
    """Run all maintenance tasks for an Iceberg table. Best-effort — never raises.

    Args:
        table_name: Fully qualified table name (e.g., "default.silver.orders")
        s3_config: S3 connection config
        nessie_config: Nessie catalog config
        snapshot_max_age_days: Expire snapshots older than this
        orphan_max_age_days: Remove orphan files older than this
        log: Optional PipelineLogger for pipeline-level logging
    """
    try:
        if log:
            log.info(f"Running Iceberg maintenance on {table_name}")

        expired = expire_snapshots(table_name, snapshot_max_age_days, s3_config, nessie_config)
        if log and expired > 0:
            log.info(f"Expired {expired} old snapshot(s)")

        removed = remove_orphan_files(table_name, orphan_max_age_days, s3_config, nessie_config)
        if log and removed > 0:
            log.info(f"Removed {removed} orphan file(s)")

        if log:
            log.info("Iceberg maintenance complete")

    except Exception as e:
        if log:
            log.warn(f"Iceberg maintenance failed (non-fatal): {e}")
        else:
            logger.warning(f"maintenance: run_maintenance failed for {table_name}: {e}")
