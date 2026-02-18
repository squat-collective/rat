"""Integration tests for Iceberg operations — write, merge, append via Nessie catalog.

These tests require running S3 (MinIO) and Nessie services. They are skipped
automatically when the required environment variables are not set.

Set the following env vars to enable:
    S3_ENDPOINT, S3_ACCESS_KEY, S3_SECRET_KEY, NESSIE_URL
"""

from __future__ import annotations

import os

import pyarrow as pa
import pytest

from rat_runner.config import NessieConfig, S3Config
from rat_runner.iceberg import (
    append_iceberg,
    build_partition_spec,
    get_catalog,
    merge_iceberg,
    read_watermark,
    write_iceberg,
)
from rat_runner.nessie import create_branch, delete_branch

# Check availability at import time — these match the conftest skip conditions.
_s3_available = bool(
    os.environ.get("S3_ENDPOINT")
    and os.environ.get("S3_ACCESS_KEY")
    and os.environ.get("S3_SECRET_KEY")
)
_nessie_available = bool(os.environ.get("NESSIE_URL"))
_has_s3_and_nessie = _s3_available and _nessie_available


@pytest.mark.skipif(
    not _has_s3_and_nessie,
    reason="S3 and Nessie services required (set S3_ENDPOINT, S3_ACCESS_KEY, S3_SECRET_KEY, NESSIE_URL)",
)
class TestIcebergWrite:
    """Test writing data to Iceberg tables via the Nessie catalog."""

    def test_write_creates_table_and_stores_data(
        self,
        s3_config: S3Config,
        nessie_config: NessieConfig,
        test_namespace: str,
    ) -> None:
        """write_iceberg should create a new Iceberg table and write data."""
        data = pa.table(
            {
                "id": pa.array([1, 2, 3], type=pa.int64()),
                "name": pa.array(["alice", "bob", "charlie"], type=pa.string()),
            }
        )
        table_name = f"{test_namespace}.bronze.write_test"
        location = f"s3://{s3_config.bucket}/{test_namespace}/bronze/write_test"

        rows = write_iceberg(
            data,
            table_name,
            s3_config,
            nessie_config,
            location,
        )
        assert rows == 3

        # Verify data is readable via the catalog
        catalog = get_catalog(s3_config, nessie_config)
        table = catalog.load_table(table_name)
        result = table.scan().to_arrow()
        assert len(result) == 3
        assert set(result.column_names) == {"id", "name"}

    def test_write_overwrites_existing_table(
        self,
        s3_config: S3Config,
        nessie_config: NessieConfig,
        test_namespace: str,
    ) -> None:
        """write_iceberg on an existing table should overwrite all data."""
        table_name = f"{test_namespace}.bronze.overwrite_test"
        location = f"s3://{s3_config.bucket}/{test_namespace}/bronze/overwrite_test"

        # First write
        data_v1 = pa.table({"id": [1, 2], "value": ["a", "b"]})
        write_iceberg(data_v1, table_name, s3_config, nessie_config, location)

        # Overwrite with different data
        data_v2 = pa.table({"id": [10, 20, 30], "value": ["x", "y", "z"]})
        rows = write_iceberg(data_v2, table_name, s3_config, nessie_config, location)
        assert rows == 3

        # Verify only v2 data remains
        catalog = get_catalog(s3_config, nessie_config)
        table = catalog.load_table(table_name)
        result = table.scan().to_arrow()
        assert len(result) == 3
        assert sorted(result.column("id").to_pylist()) == [10, 20, 30]


@pytest.mark.skipif(
    not _has_s3_and_nessie,
    reason="S3 and Nessie services required",
)
class TestIcebergMerge:
    """Test merge (upsert) operations on Iceberg tables."""

    def test_merge_inserts_on_first_run(
        self,
        s3_config: S3Config,
        nessie_config: NessieConfig,
        test_namespace: str,
    ) -> None:
        """merge_iceberg on a non-existent table should create it with all data."""
        data = pa.table(
            {
                "id": pa.array([1, 2, 3], type=pa.int64()),
                "name": pa.array(["alice", "bob", "charlie"], type=pa.string()),
            }
        )
        table_name = f"{test_namespace}.silver.merge_first_run"
        location = f"s3://{s3_config.bucket}/{test_namespace}/silver/merge_first_run"

        rows = merge_iceberg(
            data,
            table_name,
            ["id"],
            s3_config,
            nessie_config,
            location,
        )
        assert rows == 3

    def test_merge_updates_existing_rows(
        self,
        s3_config: S3Config,
        nessie_config: NessieConfig,
        test_namespace: str,
    ) -> None:
        """merge_iceberg should update existing rows matching the unique key."""
        table_name = f"{test_namespace}.silver.merge_update"
        location = f"s3://{s3_config.bucket}/{test_namespace}/silver/merge_update"

        # Initial data
        data_v1 = pa.table(
            {
                "id": pa.array([1, 2, 3], type=pa.int64()),
                "name": pa.array(["alice", "bob", "charlie"], type=pa.string()),
            }
        )
        write_iceberg(data_v1, table_name, s3_config, nessie_config, location)

        # Merge with updated + new rows
        data_v2 = pa.table(
            {
                "id": pa.array([2, 4], type=pa.int64()),
                "name": pa.array(["bob_updated", "dave"], type=pa.string()),
            }
        )
        rows = merge_iceberg(
            data_v2,
            table_name,
            ["id"],
            s3_config,
            nessie_config,
            location,
        )

        # Should have 4 rows: alice(1), bob_updated(2), charlie(3), dave(4)
        assert rows == 4

        catalog = get_catalog(s3_config, nessie_config)
        table = catalog.load_table(table_name)
        result = table.scan().to_arrow()
        assert len(result) == 4
        # Verify bob was updated
        names = sorted(result.column("name").to_pylist())
        assert "bob_updated" in names
        assert "dave" in names
        assert "alice" in names


@pytest.mark.skipif(
    not _has_s3_and_nessie,
    reason="S3 and Nessie services required",
)
class TestIcebergAppend:
    """Test append-only operations on Iceberg tables."""

    def test_append_creates_table_on_first_run(
        self,
        s3_config: S3Config,
        nessie_config: NessieConfig,
        test_namespace: str,
    ) -> None:
        """append_iceberg on a non-existent table should create it."""
        data = pa.table({"ts": ["2024-01-01"], "event": ["start"]})
        table_name = f"{test_namespace}.bronze.append_first"
        location = f"s3://{s3_config.bucket}/{test_namespace}/bronze/append_first"

        rows = append_iceberg(data, table_name, s3_config, nessie_config, location)
        assert rows == 1

    def test_append_adds_rows_without_overwriting(
        self,
        s3_config: S3Config,
        nessie_config: NessieConfig,
        test_namespace: str,
    ) -> None:
        """append_iceberg should add new rows without removing existing ones."""
        table_name = f"{test_namespace}.bronze.append_multi"
        location = f"s3://{s3_config.bucket}/{test_namespace}/bronze/append_multi"

        batch1 = pa.table({"id": [1, 2], "val": ["a", "b"]})
        append_iceberg(batch1, table_name, s3_config, nessie_config, location)

        batch2 = pa.table({"id": [3, 4], "val": ["c", "d"]})
        append_iceberg(batch2, table_name, s3_config, nessie_config, location)

        catalog = get_catalog(s3_config, nessie_config)
        table = catalog.load_table(table_name)
        result = table.scan().to_arrow()
        assert len(result) == 4
        assert sorted(result.column("id").to_pylist()) == [1, 2, 3, 4]


@pytest.mark.skipif(
    not _has_s3_and_nessie,
    reason="S3 and Nessie services required",
)
class TestIcebergWatermark:
    """Test watermark reading from Iceberg tables."""

    def test_read_watermark_returns_none_for_missing_table(
        self,
        s3_config: S3Config,
        nessie_config: NessieConfig,
        test_namespace: str,
    ) -> None:
        """read_watermark should return None for a non-existent table."""
        result = read_watermark(
            f"{test_namespace}.bronze.no_such_table",
            "updated_at",
            s3_config,
            nessie_config,
        )
        assert result is None

    def test_read_watermark_returns_max_value(
        self,
        s3_config: S3Config,
        nessie_config: NessieConfig,
        test_namespace: str,
    ) -> None:
        """read_watermark should return the max value of the watermark column."""
        table_name = f"{test_namespace}.silver.watermark_test"
        location = f"s3://{s3_config.bucket}/{test_namespace}/silver/watermark_test"

        data = pa.table(
            {
                "id": pa.array([1, 2, 3], type=pa.int64()),
                "updated_at": pa.array(
                    ["2024-01-01", "2024-06-15", "2024-03-10"],
                    type=pa.string(),
                ),
            }
        )
        write_iceberg(data, table_name, s3_config, nessie_config, location)

        result = read_watermark(
            table_name,
            "updated_at",
            s3_config,
            nessie_config,
        )
        assert result is not None
        assert result == "2024-06-15"


@pytest.mark.skipif(
    not _has_s3_and_nessie,
    reason="S3 and Nessie services required",
)
class TestNessieBranching:
    """Test Nessie branch operations for pipeline isolation."""

    def test_create_and_delete_branch(
        self,
        nessie_config: NessieConfig,
        s3_config: S3Config,
    ) -> None:
        """Should be able to create and delete ephemeral branches."""
        branch_name = "inttest-branch-lifecycle"

        branch_hash = create_branch(nessie_config, branch_name)
        assert isinstance(branch_hash, str)
        assert len(branch_hash) > 0

        # Delete should not raise
        delete_branch(nessie_config, branch_name)

    def test_create_branch_is_idempotent(
        self,
        nessie_config: NessieConfig,
        s3_config: S3Config,
    ) -> None:
        """Creating a branch that already exists should return its hash."""
        branch_name = "inttest-branch-idempotent"
        try:
            hash1 = create_branch(nessie_config, branch_name)
            hash2 = create_branch(nessie_config, branch_name)
            # Both should succeed (409 handled gracefully)
            assert isinstance(hash1, str)
            assert isinstance(hash2, str)
        finally:
            delete_branch(nessie_config, branch_name)

    def test_delete_nonexistent_branch_is_silent(
        self,
        nessie_config: NessieConfig,
    ) -> None:
        """Deleting a branch that does not exist should not raise."""
        delete_branch(nessie_config, "inttest-nonexistent-branch")

    def test_write_on_ephemeral_branch(
        self,
        s3_config: S3Config,
        nessie_config: NessieConfig,
        test_namespace: str,
    ) -> None:
        """Data written on an ephemeral branch should be isolated from main."""
        branch_name = f"inttest-{test_namespace}-ephemeral"
        try:
            create_branch(nessie_config, branch_name)

            # Write on branch
            data = pa.table({"id": [1, 2], "name": ["a", "b"]})
            table_name = f"{test_namespace}.bronze.branch_write_test"
            location = f"s3://{s3_config.bucket}/{test_namespace}/bronze/branch_write_test"

            write_iceberg(
                data,
                table_name,
                s3_config,
                nessie_config,
                location,
                branch=branch_name,
            )

            # Verify data is visible on the branch
            branch_catalog = get_catalog(s3_config, nessie_config, branch=branch_name)
            table = branch_catalog.load_table(table_name)
            result = table.scan().to_arrow()
            assert len(result) == 2
        finally:
            delete_branch(nessie_config, branch_name)


class TestBuildPartitionSpec:
    """Test partition spec construction from config entries (no services needed)."""

    def test_empty_partition_by_returns_unpartitioned(self) -> None:
        schema = pa.schema([("id", pa.int64()), ("name", pa.string())])
        spec = build_partition_spec(schema, [])
        assert len(spec.fields) == 0

    def test_identity_partition(self) -> None:
        from rat_runner.models import PartitionByEntry

        schema = pa.schema([("id", pa.int64()), ("region", pa.string())])
        entries = [PartitionByEntry(column="region", transform="identity")]
        spec = build_partition_spec(schema, entries)
        assert len(spec.fields) == 1
        assert spec.fields[0].name == "region"

    def test_invalid_column_raises_error(self) -> None:
        from rat_runner.models import PartitionByEntry

        schema = pa.schema([("id", pa.int64())])
        entries = [PartitionByEntry(column="nonexistent", transform="identity")]
        with pytest.raises(ValueError, match="not found in table schema"):
            build_partition_spec(schema, entries)

    def test_invalid_transform_raises_error(self) -> None:
        from rat_runner.models import PartitionByEntry

        schema = pa.schema([("id", pa.int64())])
        entries = [PartitionByEntry(column="id", transform="invalid_transform")]
        with pytest.raises(ValueError, match="Unsupported partition transform"):
            build_partition_spec(schema, entries)
