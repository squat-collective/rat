"""Tests for iceberg — PyIceberg writer."""

from __future__ import annotations

from unittest.mock import MagicMock, patch

import pyarrow as pa
import pytest

from rat_runner.config import NessieConfig, S3Config
from rat_runner.iceberg import (
    _MAX_COMPOSITE_DELETE_ROWS,
    _build_delete_filter_composite_key,
    _build_delete_filter_single_key,
    _dedup_new_data,
    _escape_sql_string,
    _extract_composite_key_rows,
    _get_row_count,
    _try_optimized_delete_append,
    append_iceberg,
    build_partition_spec,
    delete_insert_iceberg,
    ensure_namespace,
    get_catalog,
    merge_iceberg,
    read_watermark,
    scd2_iceberg,
    snapshot_iceberg,
    write_iceberg,
)
from rat_runner.models import PartitionByEntry


def _make_optimized_mock_table(
    all_data: pa.Table,
    filtered_data: pa.Table,
) -> MagicMock:
    """Create a mock Iceberg table that supports the optimized delete+append path.

    - current_snapshot().summary["total-records"] returns len(all_data)
    - scan() with no row_filter returns all_data (full table, fallback path)
    - scan(row_filter=...) returns filtered_data (rows matching the delete filter)
    - delete() and append() are plain mocks
    """
    mock_table = MagicMock()

    def _scan_side_effect(row_filter=None, **kwargs):
        mock_scan = MagicMock()
        if row_filter is not None:
            mock_scan.to_arrow.return_value = filtered_data
        else:
            mock_scan.to_arrow.return_value = all_data
        return mock_scan

    mock_table.scan.side_effect = _scan_side_effect

    # Support _get_row_count via snapshot metadata (O(1) path).
    mock_snapshot = MagicMock()
    mock_snapshot.summary = {"total-records": str(len(all_data))}
    mock_table.current_snapshot.return_value = mock_snapshot

    mock_table.location.return_value = "s3://test-bucket/ns/silver/orders/"
    return mock_table


class TestGetCatalog:
    def test_includes_session_token_when_set(self, nessie_config: NessieConfig):
        from pyiceberg.catalog.rest import RestCatalog

        sts_config = S3Config(
            endpoint="localhost:9000",
            access_key="ak",
            secret_key="sk",
            bucket="test",
            session_token="sts-tok-123",
        )
        mock_catalog = MagicMock(spec=RestCatalog)
        with patch("rat_runner.iceberg.load_catalog", return_value=mock_catalog) as mock_load:
            get_catalog(sts_config, nessie_config)

        _, kwargs = mock_load.call_args
        assert kwargs["s3.session-token"] == "sts-tok-123"

    def test_excludes_session_token_when_empty(
        self, s3_config: S3Config, nessie_config: NessieConfig
    ):
        from pyiceberg.catalog.rest import RestCatalog

        mock_catalog = MagicMock(spec=RestCatalog)
        with patch("rat_runner.iceberg.load_catalog", return_value=mock_catalog) as mock_load:
            get_catalog(s3_config, nessie_config)

        _, kwargs = mock_load.call_args
        assert "s3.session-token" not in kwargs


class TestEnsureNamespace:
    def test_creates_namespace_hierarchy(self):
        catalog = MagicMock()
        ensure_namespace(catalog, "ns.silver")

        assert catalog.create_namespace.call_count == 2
        catalog.create_namespace.assert_any_call(("ns",))
        catalog.create_namespace.assert_any_call(("ns", "silver"))

    def test_ignores_already_exists(self):
        from pyiceberg.exceptions import NamespaceAlreadyExistsError

        catalog = MagicMock()
        catalog.create_namespace.side_effect = NamespaceAlreadyExistsError("exists")
        ensure_namespace(catalog, "ns.silver")  # should not raise

    def test_single_level_namespace(self):
        catalog = MagicMock()
        ensure_namespace(catalog, "ns")
        catalog.create_namespace.assert_called_once_with(("ns",))


class TestWriteIceberg:
    def test_creates_table_if_not_exists(self, s3_config: S3Config, nessie_config: NessieConfig):
        from pyiceberg.exceptions import NoSuchTableError

        data = pa.table({"id": [1, 2, 3], "name": ["a", "b", "c"]})
        mock_catalog = MagicMock()
        mock_table = MagicMock()
        mock_catalog.load_table.side_effect = NoSuchTableError("nope")
        mock_catalog.create_table.return_value = mock_table

        with patch("rat_runner.iceberg.get_catalog", return_value=mock_catalog):
            rows = write_iceberg(data, "ns.silver.orders", s3_config, nessie_config, "s3://b/loc/")

        assert rows == 3
        mock_catalog.create_table.assert_called_once()
        mock_table.overwrite.assert_called_once_with(data)

    def test_overwrites_existing_table(self, s3_config: S3Config, nessie_config: NessieConfig):
        data = pa.table({"id": [1, 2]})
        mock_catalog = MagicMock()
        mock_table = MagicMock()
        mock_catalog.load_table.return_value = mock_table

        with patch("rat_runner.iceberg.get_catalog", return_value=mock_catalog):
            rows = write_iceberg(data, "ns.silver.orders", s3_config, nessie_config, "s3://b/loc/")

        assert rows == 2
        mock_table.overwrite.assert_called_once_with(data)
        mock_catalog.create_table.assert_not_called()

    def test_returns_row_count(self, s3_config: S3Config, nessie_config: NessieConfig):
        data = pa.table({"x": list(range(100))})
        mock_catalog = MagicMock()
        mock_catalog.load_table.return_value = MagicMock()

        with patch("rat_runner.iceberg.get_catalog", return_value=mock_catalog):
            rows = write_iceberg(data, "ns.gold.agg", s3_config, nessie_config, "s3://b/loc/")

        assert rows == 100

    def test_ensures_namespace(self, s3_config: S3Config, nessie_config: NessieConfig):
        data = pa.table({"id": [1]})
        mock_catalog = MagicMock()
        mock_catalog.load_table.return_value = MagicMock()

        with (
            patch("rat_runner.iceberg.get_catalog", return_value=mock_catalog),
            patch("rat_runner.iceberg.ensure_namespace") as mock_ensure,
        ):
            write_iceberg(data, "ns.silver.orders", s3_config, nessie_config, "s3://b/loc/")

        mock_ensure.assert_called_once_with(mock_catalog, "ns.silver")

    def test_passes_branch_to_catalog(self, s3_config: S3Config, nessie_config: NessieConfig):
        data = pa.table({"id": [1]})

        with (
            patch("rat_runner.iceberg.get_catalog") as mock_get_catalog,
            patch("rat_runner.iceberg.ensure_namespace"),
        ):
            mock_catalog = MagicMock()
            mock_catalog.load_table.return_value = MagicMock()
            mock_get_catalog.return_value = mock_catalog

            write_iceberg(
                data,
                "ns.silver.orders",
                s3_config,
                nessie_config,
                "s3://b/loc/",
                branch="run-r1",
            )

        mock_get_catalog.assert_called_once_with(s3_config, nessie_config, branch="run-r1")


class TestMergeIceberg:
    def _mock_table_with_data(self, data: pa.Table) -> MagicMock:
        """Create a mock Iceberg table that provides data via scan() fallback.

        Used for the full-rewrite (fallback) path, where scan() is only called
        without row_filter in the DuckDB fallback code.
        """
        mock_table = MagicMock()
        mock_scan = MagicMock()
        mock_scan.to_arrow.return_value = data
        mock_table.scan.return_value = mock_scan
        mock_table.location.return_value = "s3://test-bucket/ns/silver/orders/"
        return mock_table

    def test_merge_optimized_single_key(self, s3_config: S3Config, nessie_config: NessieConfig):
        """Single-column key uses optimized delete+append (no full rewrite)."""
        existing = pa.table({"id": [1, 2, 3], "value": ["a", "b", "c"]})
        # Rows matching delete filter: id IN (2, 4) -> only id=2 is in existing
        filtered = pa.table({"id": [2], "value": ["b"]})
        new_data = pa.table({"id": [2, 4], "value": ["b_updated", "d"]})

        mock_table = _make_optimized_mock_table(existing, filtered)
        mock_catalog = MagicMock()
        mock_catalog.load_table.return_value = mock_table

        with (
            patch("rat_runner.iceberg.get_catalog", return_value=mock_catalog),
            patch("rat_runner.iceberg.ensure_namespace"),
        ):
            rows = merge_iceberg(
                new_data,
                "ns.silver.orders",
                ["id"],
                s3_config,
                nessie_config,
                "s3://b/loc/",
            )

        # existing=3, deleted=1 (id=2), appended=2 (id=2,4) -> 3 - 1 + 2 = 4
        assert rows == 4
        mock_table.delete.assert_called_once()
        mock_table.append.assert_called_once()
        # Overwrite should NOT be called (optimized path)
        mock_table.overwrite.assert_not_called()

    def test_merge_optimized_deduplicates_new_data(
        self, s3_config: S3Config, nessie_config: NessieConfig
    ):
        """Single-column key: new_data is deduped before delete+append (last wins)."""
        existing = pa.table({"id": [1], "value": ["orig"]})
        # After dedup, new_data has 1 row: id=2, value="second"
        # Filtered: id=2 not in existing, so 0 matches
        filtered = pa.table(
            {"id": pa.array([], type=pa.int64()), "value": pa.array([], type=pa.string())}
        )
        new_data = pa.table({"id": [2, 2], "value": ["first", "second"]})

        mock_table = _make_optimized_mock_table(existing, filtered)
        mock_catalog = MagicMock()
        mock_catalog.load_table.return_value = mock_table

        with (
            patch("rat_runner.iceberg.get_catalog", return_value=mock_catalog),
            patch("rat_runner.iceberg.ensure_namespace"),
        ):
            rows = merge_iceberg(
                new_data,
                "ns.silver.orders",
                ["id"],
                s3_config,
                nessie_config,
                "s3://b/loc/",
            )

        # existing=1, deleted=0, appended=1 (deduped) -> 1 - 0 + 1 = 2
        assert rows == 2
        mock_table.delete.assert_called_once()
        mock_table.append.assert_called_once()
        # Verify deduped data was appended (last row wins)
        appended = mock_table.append.call_args[0][0]
        assert len(appended) == 1
        assert appended.column("value")[0].as_py() == "second"

    def test_merge_composite_key_uses_optimized_path(
        self, s3_config: S3Config, nessie_config: NessieConfig
    ):
        """Composite unique keys with small row count use optimized delete+append."""
        existing = pa.table({"id": [1, 2], "region": ["us", "eu"], "value": ["a", "b"]})
        # Rows matching composite filter: (id=2, region=eu) -> 1 existing row
        filtered = pa.table({"id": [2], "region": ["eu"], "value": ["b"]})
        new_data = pa.table({"id": [2], "region": ["eu"], "value": ["b_updated"]})

        mock_table = _make_optimized_mock_table(existing, filtered)
        mock_catalog = MagicMock()
        mock_catalog.load_table.return_value = mock_table

        with (
            patch("rat_runner.iceberg.get_catalog", return_value=mock_catalog),
            patch("rat_runner.iceberg.ensure_namespace"),
        ):
            rows = merge_iceberg(
                new_data,
                "ns.silver.orders",
                ["id", "region"],
                s3_config,
                nessie_config,
                "s3://b/loc/",
            )

        # existing=2, deleted=1 (id=2/eu), appended=1 (updated) -> 2 - 1 + 1 = 2
        assert rows == 2
        mock_table.delete.assert_called_once()
        mock_table.append.assert_called_once()
        # Overwrite should NOT be called (optimized path)
        mock_table.overwrite.assert_not_called()

    def test_merge_optimized_failure_falls_back_to_full_rewrite(
        self, s3_config: S3Config, nessie_config: NessieConfig
    ):
        """If the optimized path raises, fall back to full rewrite."""
        existing = pa.table({"id": [1, 2, 3], "value": ["a", "b", "c"]})
        new_data = pa.table({"id": [2, 4], "value": ["b_updated", "d"]})

        mock_table = self._mock_table_with_data(existing)
        mock_catalog = MagicMock()
        mock_catalog.load_table.return_value = mock_table

        with (
            patch("rat_runner.iceberg.get_catalog", return_value=mock_catalog),
            patch("rat_runner.iceberg.ensure_namespace"),
            patch("rat_runner.iceberg._try_optimized_delete_append", return_value=None),
        ):
            rows = merge_iceberg(
                new_data,
                "ns.silver.orders",
                ["id"],
                s3_config,
                nessie_config,
                "s3://b/loc/",
            )

        # Full rewrite: ANTI JOIN + UNION ALL -> 4 rows
        assert rows == 4
        mock_table.overwrite.assert_called_once()
        merged = mock_table.overwrite.call_args[0][0]
        merged_ids = sorted(merged.column("id").to_pylist())
        assert merged_ids == [1, 2, 3, 4]

    def test_merge_falls_back_to_write_on_missing_table(
        self, s3_config: S3Config, nessie_config: NessieConfig
    ):
        from pyiceberg.exceptions import NoSuchTableError

        new_data = pa.table({"id": [1, 2], "value": ["a", "b"]})
        mock_catalog = MagicMock()
        mock_catalog.load_table.side_effect = NoSuchTableError("nope")

        with (
            patch("rat_runner.iceberg.get_catalog", return_value=mock_catalog),
            patch("rat_runner.iceberg.write_iceberg", return_value=2) as mock_write,
        ):
            rows = merge_iceberg(
                new_data,
                "ns.silver.orders",
                ["id"],
                s3_config,
                nessie_config,
                "s3://b/loc/",
            )

        assert rows == 2
        mock_write.assert_called_once()


class TestReadWatermark:
    def test_returns_max_value(self, s3_config: S3Config, nessie_config: NessieConfig):
        # Only the watermark column is returned (column projection).
        projected = pa.table({"ts": ["2024-01-01", "2024-03-01", "2024-02-01"]})

        mock_catalog = MagicMock()
        mock_table = MagicMock()
        mock_scan = MagicMock()
        mock_scan.to_arrow.return_value = projected
        mock_table.scan.return_value = mock_scan
        mock_catalog.load_table.return_value = mock_table

        with patch("rat_runner.iceberg.get_catalog", return_value=mock_catalog):
            result = read_watermark("ns.silver.orders", "ts", s3_config, nessie_config)

        assert result == "2024-03-01"
        # Verify column projection: only the watermark column is requested.
        mock_table.scan.assert_called_once_with(selected_fields=("ts",))

    def test_returns_none_on_missing_table(self, s3_config: S3Config, nessie_config: NessieConfig):
        from pyiceberg.exceptions import NoSuchTableError

        mock_catalog = MagicMock()
        mock_catalog.load_table.side_effect = NoSuchTableError("nope")

        with patch("rat_runner.iceberg.get_catalog", return_value=mock_catalog):
            result = read_watermark("ns.silver.orders", "ts", s3_config, nessie_config)

        assert result is None

    def test_returns_none_on_empty_table(self, s3_config: S3Config, nessie_config: NessieConfig):
        # Only the watermark column is returned (column projection).
        projected = pa.table({"ts": pa.array([], type=pa.string())})

        mock_catalog = MagicMock()
        mock_table = MagicMock()
        mock_scan = MagicMock()
        mock_scan.to_arrow.return_value = projected
        mock_table.scan.return_value = mock_scan
        mock_catalog.load_table.return_value = mock_table

        with patch("rat_runner.iceberg.get_catalog", return_value=mock_catalog):
            result = read_watermark("ns.silver.orders", "ts", s3_config, nessie_config)

        assert result is None
        mock_table.scan.assert_called_once_with(selected_fields=("ts",))


class TestAppendIceberg:
    def test_appends_to_existing_table(self, s3_config: S3Config, nessie_config: NessieConfig):
        data = pa.table({"id": [4, 5], "value": ["d", "e"]})
        mock_catalog = MagicMock()
        mock_table = MagicMock()
        mock_catalog.load_table.return_value = mock_table

        with (
            patch("rat_runner.iceberg.get_catalog", return_value=mock_catalog),
            patch("rat_runner.iceberg.ensure_namespace"),
        ):
            rows = append_iceberg(data, "ns.silver.events", s3_config, nessie_config, "s3://b/loc/")

        assert rows == 2
        mock_table.append.assert_called_once_with(data)

    def test_creates_table_if_missing(self, s3_config: S3Config, nessie_config: NessieConfig):
        from pyiceberg.exceptions import NoSuchTableError

        data = pa.table({"id": [1, 2]})
        mock_catalog = MagicMock()
        mock_catalog.load_table.side_effect = NoSuchTableError("nope")

        with (
            patch("rat_runner.iceberg.get_catalog", return_value=mock_catalog),
            patch("rat_runner.iceberg.ensure_namespace"),
            patch("rat_runner.iceberg.write_iceberg", return_value=2) as mock_write,
        ):
            rows = append_iceberg(data, "ns.silver.events", s3_config, nessie_config, "s3://b/loc/")

        assert rows == 2
        mock_write.assert_called_once()

    def test_returns_row_count(self, s3_config: S3Config, nessie_config: NessieConfig):
        data = pa.table({"x": list(range(50))})
        mock_catalog = MagicMock()
        mock_catalog.load_table.return_value = MagicMock()

        with (
            patch("rat_runner.iceberg.get_catalog", return_value=mock_catalog),
            patch("rat_runner.iceberg.ensure_namespace"),
        ):
            rows = append_iceberg(data, "ns.silver.logs", s3_config, nessie_config, "s3://b/loc/")

        assert rows == 50


class TestDeleteInsertIceberg:
    def _mock_table_with_data(self, data: pa.Table) -> MagicMock:
        """Used for the full-rewrite (fallback) path."""
        mock_table = MagicMock()
        mock_scan = MagicMock()
        mock_scan.to_arrow.return_value = data
        mock_table.scan.return_value = mock_scan
        return mock_table

    def test_optimized_single_key_no_dedup(self, s3_config: S3Config, nessie_config: NessieConfig):
        """Single-column key uses optimized delete+append without deduplication."""
        existing = pa.table({"id": [1, 2, 3], "value": ["a", "b", "c"]})
        # Rows matching: id IN (2) -> 1 existing row
        filtered = pa.table({"id": [2], "value": ["b"]})
        new_data = pa.table({"id": [2, 2], "value": ["b1", "b2"]})

        mock_table = _make_optimized_mock_table(existing, filtered)
        mock_catalog = MagicMock()
        mock_catalog.load_table.return_value = mock_table

        with (
            patch("rat_runner.iceberg.get_catalog", return_value=mock_catalog),
            patch("rat_runner.iceberg.ensure_namespace"),
        ):
            rows = delete_insert_iceberg(
                new_data,
                "ns.silver.orders",
                ["id"],
                s3_config,
                nessie_config,
                "s3://b/loc/",
            )

        # existing=3, deleted=1 (id=2), appended=2 (id=2,2 NO dedup) -> 3 - 1 + 2 = 4
        assert rows == 4
        mock_table.delete.assert_called_once()
        mock_table.append.assert_called_once()
        mock_table.overwrite.assert_not_called()
        # Verify both duplicate rows were appended (no dedup in delete_insert)
        appended = mock_table.append.call_args[0][0]
        assert len(appended) == 2

    def test_composite_key_uses_optimized_path(
        self, s3_config: S3Config, nessie_config: NessieConfig
    ):
        """Composite unique keys with small row count use optimized delete+append."""
        existing = pa.table(
            {"id": [1, 2, 3], "region": ["us", "eu", "us"], "value": ["a", "b", "c"]}
        )
        # Rows matching composite filter: (id=2, region=eu) -> 1 existing row
        filtered = pa.table({"id": [2], "region": ["eu"], "value": ["b"]})
        new_data = pa.table({"id": [2, 2], "region": ["eu", "eu"], "value": ["b1", "b2"]})

        mock_table = _make_optimized_mock_table(existing, filtered)
        mock_catalog = MagicMock()
        mock_catalog.load_table.return_value = mock_table

        with (
            patch("rat_runner.iceberg.get_catalog", return_value=mock_catalog),
            patch("rat_runner.iceberg.ensure_namespace"),
        ):
            rows = delete_insert_iceberg(
                new_data,
                "ns.silver.orders",
                ["id", "region"],
                s3_config,
                nessie_config,
                "s3://b/loc/",
            )

        # existing=3, deleted=1 (id=2/eu), appended=2 (NO dedup) -> 3 - 1 + 2 = 4
        assert rows == 4
        mock_table.delete.assert_called_once()
        mock_table.append.assert_called_once()
        # Overwrite should NOT be called (optimized path)
        mock_table.overwrite.assert_not_called()
        # Verify both duplicate rows were appended (no dedup in delete_insert)
        appended = mock_table.append.call_args[0][0]
        assert len(appended) == 2

    def test_optimized_failure_falls_back_to_full_rewrite(
        self, s3_config: S3Config, nessie_config: NessieConfig
    ):
        """If the optimized path returns None, fall back to full rewrite."""
        existing = pa.table({"id": [1, 2], "value": ["a", "b"]})
        new_data = pa.table({"id": [2], "value": ["b_updated"]})

        mock_table = self._mock_table_with_data(existing)
        mock_catalog = MagicMock()
        mock_catalog.load_table.return_value = mock_table

        with (
            patch("rat_runner.iceberg.get_catalog", return_value=mock_catalog),
            patch("rat_runner.iceberg.ensure_namespace"),
            patch("rat_runner.iceberg._try_optimized_delete_append", return_value=None),
        ):
            rows = delete_insert_iceberg(
                new_data,
                "ns.silver.orders",
                ["id"],
                s3_config,
                nessie_config,
                "s3://b/loc/",
            )

        assert rows == 2
        mock_table.overwrite.assert_called_once()

    def test_falls_back_on_missing_table(self, s3_config: S3Config, nessie_config: NessieConfig):
        from pyiceberg.exceptions import NoSuchTableError

        new_data = pa.table({"id": [1], "value": ["a"]})
        mock_catalog = MagicMock()
        mock_catalog.load_table.side_effect = NoSuchTableError("nope")

        with (
            patch("rat_runner.iceberg.get_catalog", return_value=mock_catalog),
            patch("rat_runner.iceberg.write_iceberg", return_value=1) as mock_write,
        ):
            rows = delete_insert_iceberg(
                new_data,
                "ns.silver.orders",
                ["id"],
                s3_config,
                nessie_config,
                "s3://b/loc/",
            )

        assert rows == 1
        mock_write.assert_called_once()


class TestScd2Iceberg:
    def _mock_table_with_data(self, data: pa.Table) -> MagicMock:
        mock_table = MagicMock()
        mock_scan = MagicMock()
        mock_scan.to_arrow.return_value = data
        mock_table.scan.return_value = mock_scan
        mock_table.schema.return_value = MagicMock(names=lambda: list(data.schema.names))
        # Override schema() to return something with .names()
        mock_schema = MagicMock()
        mock_schema.names.return_value = list(data.schema.names)
        mock_table.schema.return_value = mock_schema
        return mock_table

    def test_first_run_adds_scd_columns(self, s3_config: S3Config, nessie_config: NessieConfig):
        from pyiceberg.exceptions import NoSuchTableError

        new_data = pa.table({"id": [1, 2], "name": ["alice", "bob"]})
        mock_catalog = MagicMock()
        mock_catalog.load_table.side_effect = NoSuchTableError("nope")

        with (
            patch("rat_runner.iceberg.get_catalog", return_value=mock_catalog),
            patch("rat_runner.iceberg.ensure_namespace"),
            patch("rat_runner.iceberg.write_iceberg", return_value=2) as mock_write,
        ):
            rows = scd2_iceberg(
                new_data,
                "ns.silver.customers",
                ["id"],
                s3_config,
                nessie_config,
                "s3://b/loc/",
            )

        assert rows == 2
        # Verify write_iceberg was called with data that has SCD columns
        written_data = mock_write.call_args[0][0]
        assert "valid_from" in written_data.column_names
        assert "valid_to" in written_data.column_names

    def test_closes_existing_opens_new(self, s3_config: S3Config, nessie_config: NessieConfig):
        import pyarrow as pa

        existing = pa.table(
            {
                "id": [1, 2],
                "name": ["alice", "bob"],
                "valid_from": pa.array(
                    ["2024-01-01 00:00:00", "2024-01-01 00:00:00"], type=pa.string()
                ).cast(pa.timestamp("us")),
                "valid_to": pa.array([None, None], type=pa.timestamp("us")),
            }
        )
        new_data = pa.table({"id": [1], "name": ["alice_updated"]})

        mock_catalog = MagicMock()
        mock_table = self._mock_table_with_data(existing)
        mock_catalog.load_table.return_value = mock_table

        with (
            patch("rat_runner.iceberg.get_catalog", return_value=mock_catalog),
            patch("rat_runner.iceberg.ensure_namespace"),
        ):
            rows = scd2_iceberg(
                new_data,
                "ns.silver.customers",
                ["id"],
                s3_config,
                nessie_config,
                "s3://b/loc/",
            )

        # id=1 (closed) + id=2 (kept open) + id=1 new (opened) = 3 rows
        assert rows == 3

    def test_custom_column_names(self, s3_config: S3Config, nessie_config: NessieConfig):
        from pyiceberg.exceptions import NoSuchTableError

        new_data = pa.table({"id": [1]})
        mock_catalog = MagicMock()
        mock_catalog.load_table.side_effect = NoSuchTableError("nope")

        with (
            patch("rat_runner.iceberg.get_catalog", return_value=mock_catalog),
            patch("rat_runner.iceberg.ensure_namespace"),
            patch("rat_runner.iceberg.write_iceberg", return_value=1) as mock_write,
        ):
            scd2_iceberg(
                new_data,
                "ns.silver.t",
                ["id"],
                s3_config,
                nessie_config,
                "s3://b/loc/",
                valid_from_col="start_ts",
                valid_to_col="end_ts",
            )

        written_data = mock_write.call_args[0][0]
        assert "start_ts" in written_data.column_names
        assert "end_ts" in written_data.column_names


class TestSnapshotIceberg:
    def _mock_table_with_data(self, data: pa.Table) -> MagicMock:
        """Used for the full-rewrite (fallback) path."""
        mock_table = MagicMock()
        mock_scan = MagicMock()
        mock_scan.to_arrow.return_value = data
        mock_table.scan.return_value = mock_scan
        return mock_table

    def test_optimized_replaces_touched_partitions(
        self, s3_config: S3Config, nessie_config: NessieConfig
    ):
        """Optimized path: delete rows matching partition values + append new data."""
        existing = pa.table(
            {
                "date": ["2024-01", "2024-01", "2024-02", "2024-03"],
                "value": [1, 2, 3, 4],
            }
        )
        # Rows matching: date IN ("2024-01") -> 2 existing rows
        filtered = pa.table({"date": ["2024-01", "2024-01"], "value": [1, 2]})
        new_data = pa.table({"date": ["2024-01"], "value": [99]})

        mock_table = _make_optimized_mock_table(existing, filtered)
        mock_catalog = MagicMock()
        mock_catalog.load_table.return_value = mock_table

        with (
            patch("rat_runner.iceberg.get_catalog", return_value=mock_catalog),
            patch("rat_runner.iceberg.ensure_namespace"),
        ):
            rows = snapshot_iceberg(
                new_data,
                "ns.silver.metrics",
                "date",
                s3_config,
                nessie_config,
                "s3://b/loc/",
            )

        # existing=4, deleted=2 (2024-01), appended=1 (2024-01 new) -> 4 - 2 + 1 = 3
        assert rows == 3
        mock_table.delete.assert_called_once()
        mock_table.append.assert_called_once()
        mock_table.overwrite.assert_not_called()

    def test_optimized_keeps_untouched_partitions(
        self, s3_config: S3Config, nessie_config: NessieConfig
    ):
        """Optimized path with new partition value: no existing rows deleted."""
        existing = pa.table(
            {
                "date": ["2024-01", "2024-02"],
                "value": [1, 2],
            }
        )
        # Rows matching: date IN ("2024-03") -> 0 rows (new partition)
        filtered = pa.table(
            {
                "date": pa.array([], type=pa.string()),
                "value": pa.array([], type=pa.int64()),
            }
        )
        new_data = pa.table({"date": ["2024-03"], "value": [3]})

        mock_table = _make_optimized_mock_table(existing, filtered)
        mock_catalog = MagicMock()
        mock_catalog.load_table.return_value = mock_table

        with (
            patch("rat_runner.iceberg.get_catalog", return_value=mock_catalog),
            patch("rat_runner.iceberg.ensure_namespace"),
        ):
            rows = snapshot_iceberg(
                new_data,
                "ns.silver.metrics",
                "date",
                s3_config,
                nessie_config,
                "s3://b/loc/",
            )

        # existing=2, deleted=0, appended=1 -> 2 - 0 + 1 = 3
        assert rows == 3
        mock_table.delete.assert_called_once()
        mock_table.append.assert_called_once()

    def test_optimized_failure_falls_back_to_full_rewrite(
        self, s3_config: S3Config, nessie_config: NessieConfig
    ):
        """If optimized path fails, fall back to DuckDB partition filter + overwrite."""
        existing = pa.table(
            {
                "date": ["2024-01", "2024-01", "2024-02", "2024-03"],
                "value": [1, 2, 3, 4],
            }
        )
        new_data = pa.table({"date": ["2024-01"], "value": [99]})

        mock_table = self._mock_table_with_data(existing)
        mock_catalog = MagicMock()
        mock_catalog.load_table.return_value = mock_table

        with (
            patch("rat_runner.iceberg.get_catalog", return_value=mock_catalog),
            patch("rat_runner.iceberg.ensure_namespace"),
            patch("rat_runner.iceberg._try_optimized_delete_append", return_value=None),
        ):
            rows = snapshot_iceberg(
                new_data,
                "ns.silver.metrics",
                "date",
                s3_config,
                nessie_config,
                "s3://b/loc/",
            )

        # Full rewrite: 2024-02 (1) + 2024-03 (1) + 2024-01 (1 new) = 3
        assert rows == 3
        mock_table.overwrite.assert_called_once()
        merged = mock_table.overwrite.call_args[0][0]
        dates = sorted(merged.column("date").to_pylist())
        assert dates == ["2024-01", "2024-02", "2024-03"]

    def test_falls_back_on_missing_table(self, s3_config: S3Config, nessie_config: NessieConfig):
        from pyiceberg.exceptions import NoSuchTableError

        new_data = pa.table({"date": ["2024-01"], "value": [1]})
        mock_catalog = MagicMock()
        mock_catalog.load_table.side_effect = NoSuchTableError("nope")

        with (
            patch("rat_runner.iceberg.get_catalog", return_value=mock_catalog),
            patch("rat_runner.iceberg.write_iceberg", return_value=1) as mock_write,
        ):
            rows = snapshot_iceberg(
                new_data,
                "ns.silver.metrics",
                "date",
                s3_config,
                nessie_config,
                "s3://b/loc/",
            )

        assert rows == 1
        mock_write.assert_called_once()


class TestBuildPartitionSpec:
    def test_single_identity_partition(self):
        """Identity transform should use the source field's ID."""
        from pyiceberg.transforms import IdentityTransform

        schema = pa.schema([("region", pa.string()), ("value", pa.int64())])
        entries = (PartitionByEntry(column="region", transform="identity"),)

        spec = build_partition_spec(schema, entries)

        assert len(spec.fields) == 1
        field = spec.fields[0]
        assert field.name == "region"
        assert isinstance(field.transform, IdentityTransform)
        # source_id should be 1 (first column, 1-indexed)
        assert field.source_id == 1

    def test_day_transform(self):
        """Day transform on a date column."""
        from pyiceberg.transforms import DayTransform

        schema = pa.schema([("created_date", pa.date32()), ("id", pa.int64())])
        entries = (PartitionByEntry(column="created_date", transform="day"),)

        spec = build_partition_spec(schema, entries)

        assert len(spec.fields) == 1
        field = spec.fields[0]
        assert field.name == "created_date_day"
        assert isinstance(field.transform, DayTransform)
        assert field.source_id == 1

    def test_multiple_partition_fields(self):
        """Multiple partition entries produce a spec with multiple fields."""
        schema = pa.schema(
            [
                ("created_date", pa.date32()),
                ("region", pa.string()),
                ("value", pa.int64()),
            ]
        )
        entries = (
            PartitionByEntry(column="created_date", transform="month"),
            PartitionByEntry(column="region", transform="identity"),
        )

        spec = build_partition_spec(schema, entries)

        assert len(spec.fields) == 2
        assert spec.fields[0].name == "created_date_month"
        assert spec.fields[0].source_id == 1  # created_date is column 0 -> ID 1
        assert spec.fields[1].name == "region"
        assert spec.fields[1].source_id == 2  # region is column 1 -> ID 2

    def test_all_supported_transforms(self):
        """All supported transforms (identity, day, month, year, hour) should work."""
        from pyiceberg.transforms import (
            DayTransform,
            HourTransform,
            IdentityTransform,
            MonthTransform,
            YearTransform,
        )

        schema = pa.schema([("ts", pa.timestamp("us"))])
        transforms = {
            "identity": IdentityTransform,
            "day": DayTransform,
            "month": MonthTransform,
            "year": YearTransform,
            "hour": HourTransform,
        }

        for transform_name, transform_cls in transforms.items():
            entries = (PartitionByEntry(column="ts", transform=transform_name),)
            spec = build_partition_spec(schema, entries)
            assert isinstance(spec.fields[0].transform, transform_cls)

    def test_empty_partition_by_returns_empty_spec(self):
        """Empty partition_by list returns an unpartitioned spec."""
        schema = pa.schema([("id", pa.int64())])
        spec = build_partition_spec(schema, ())

        assert len(spec.fields) == 0

    def test_unknown_column_raises_value_error(self):
        """Referencing a column not in the schema should raise ValueError."""
        schema = pa.schema([("id", pa.int64())])
        entries = (PartitionByEntry(column="nonexistent", transform="identity"),)

        with pytest.raises(ValueError, match="not found in table schema"):
            build_partition_spec(schema, entries)

    def test_unsupported_transform_raises_value_error(self):
        """An unsupported transform name should raise ValueError."""
        schema = pa.schema([("id", pa.int64())])
        entries = (PartitionByEntry(column="id", transform="bucket[16]"),)

        with pytest.raises(ValueError, match="Unsupported partition transform"):
            build_partition_spec(schema, entries)


class TestWriteIcebergWithPartitions:
    def test_creates_table_with_partition_spec(
        self, s3_config: S3Config, nessie_config: NessieConfig
    ):
        """When partition_by is provided and table doesn't exist, create_table should receive a partition_spec."""
        from pyiceberg.exceptions import NoSuchTableError

        data = pa.table({"id": [1, 2], "region": ["us", "eu"]})
        mock_catalog = MagicMock()
        mock_table = MagicMock()
        mock_catalog.load_table.side_effect = NoSuchTableError("nope")
        mock_catalog.create_table.return_value = mock_table
        partition_by = (PartitionByEntry(column="region", transform="identity"),)

        with patch("rat_runner.iceberg.get_catalog", return_value=mock_catalog):
            rows = write_iceberg(
                data,
                "ns.silver.orders",
                s3_config,
                nessie_config,
                "s3://b/loc/",
                partition_by=partition_by,
            )

        assert rows == 2
        call_kwargs = mock_catalog.create_table.call_args[1]
        assert "partition_spec" in call_kwargs
        spec = call_kwargs["partition_spec"]
        assert len(spec.fields) == 1
        assert spec.fields[0].name == "region"

    def test_creates_table_without_partition_spec_when_none(
        self, s3_config: S3Config, nessie_config: NessieConfig
    ):
        """When partition_by is None, create_table should NOT receive partition_spec."""
        from pyiceberg.exceptions import NoSuchTableError

        data = pa.table({"id": [1]})
        mock_catalog = MagicMock()
        mock_table = MagicMock()
        mock_catalog.load_table.side_effect = NoSuchTableError("nope")
        mock_catalog.create_table.return_value = mock_table

        with patch("rat_runner.iceberg.get_catalog", return_value=mock_catalog):
            write_iceberg(data, "ns.silver.t", s3_config, nessie_config, "s3://b/loc/")

        call_kwargs = mock_catalog.create_table.call_args[1]
        assert "partition_spec" not in call_kwargs

    def test_partition_spec_ignored_for_existing_table(
        self, s3_config: S3Config, nessie_config: NessieConfig
    ):
        """When table already exists, partition_by is ignored (no table creation)."""
        data = pa.table({"id": [1], "region": ["us"]})
        mock_catalog = MagicMock()
        mock_table = MagicMock()
        mock_catalog.load_table.return_value = mock_table
        partition_by = (PartitionByEntry(column="region", transform="identity"),)

        with patch("rat_runner.iceberg.get_catalog", return_value=mock_catalog):
            rows = write_iceberg(
                data,
                "ns.silver.orders",
                s3_config,
                nessie_config,
                "s3://b/loc/",
                partition_by=partition_by,
            )

        assert rows == 1
        mock_catalog.create_table.assert_not_called()
        mock_table.overwrite.assert_called_once_with(data)


class TestEscapeSqlString:
    def test_escapes_single_quote(self):
        assert _escape_sql_string("it's a path") == "it''s a path"

    def test_escapes_multiple_single_quotes(self):
        assert _escape_sql_string("a'b'c") == "a''b''c"

    def test_no_change_without_quotes(self):
        assert (
            _escape_sql_string("s3://bucket/path/metadata.json") == "s3://bucket/path/metadata.json"
        )

    def test_metadata_location_with_single_quote(self):
        """Simulate a crafted metadata_location containing a single quote."""
        location = "s3://bucket/ns/table/metadata/v1.metadata.json'; DROP TABLE x; --"
        escaped = _escape_sql_string(location)
        assert escaped == "s3://bucket/ns/table/metadata/v1.metadata.json''; DROP TABLE x; --"
        assert "'" not in escaped.replace("''", "")


class TestBuildDeleteFilterSingleKey:
    """Tests for _build_delete_filter_single_key helper."""

    def test_builds_in_filter_from_pyarrow_array(self):
        """PyArrow array values are converted to an In() filter."""
        values = pa.array([1, 2, 3])
        result = _build_delete_filter_single_key("id", values)

        from pyiceberg.expressions import In

        assert isinstance(result, In)

    def test_builds_in_filter_from_chunked_array(self):
        """ChunkedArray values are also supported."""
        table = pa.table({"id": [10, 20, 30]})
        chunked = table.column("id")
        result = _build_delete_filter_single_key("id", chunked)

        from pyiceberg.expressions import In

        assert isinstance(result, In)

    def test_builds_in_filter_from_python_list(self):
        """Plain Python list values are also supported."""
        result = _build_delete_filter_single_key("name", ["alice", "bob"])

        from pyiceberg.expressions import In

        assert isinstance(result, In)

    def test_deduplicates_values(self):
        """Duplicate values in the input are deduplicated."""
        values = pa.array([1, 1, 2, 2, 3])
        result = _build_delete_filter_single_key("id", values)

        from pyiceberg.expressions import In

        assert isinstance(result, In)


class TestExtractCompositeKeyRows:
    """Tests for _extract_composite_key_rows helper."""

    def test_extracts_deduplicated_tuples(self):
        """Duplicate key combinations are removed, preserving first-seen order."""
        data = pa.table({"id": [1, 2, 1, 3], "region": ["us", "eu", "us", "us"]})
        result = _extract_composite_key_rows(data, ["id", "region"])

        assert result == [(1, "us"), (2, "eu"), (3, "us")]

    def test_preserves_none_values(self):
        """NULL values are preserved as None in the output tuples."""
        data = pa.table(
            {
                "id": pa.array([1, 2, None], type=pa.int64()),
                "region": pa.array(["us", None, "eu"], type=pa.string()),
            }
        )
        result = _extract_composite_key_rows(data, ["id", "region"])

        assert result == [(1, "us"), (2, None), (None, "eu")]

    def test_empty_table_returns_empty_list(self):
        """Empty table returns empty list."""
        data = pa.table(
            {
                "id": pa.array([], type=pa.int64()),
                "region": pa.array([], type=pa.string()),
            }
        )
        result = _extract_composite_key_rows(data, ["id", "region"])

        assert result == []

    def test_all_duplicates_returns_single_tuple(self):
        """When all rows have the same key, returns a single tuple."""
        data = pa.table({"id": [1, 1, 1], "region": ["us", "us", "us"], "value": ["a", "b", "c"]})
        result = _extract_composite_key_rows(data, ["id", "region"])

        assert result == [(1, "us")]


class TestBuildDeleteFilterCompositeKey:
    """Tests for _build_delete_filter_composite_key helper."""

    def test_multi_row_builds_or_of_ands(self):
        """Multiple rows produce Or(And(...), And(...))."""
        from pyiceberg.expressions import Or

        result = _build_delete_filter_composite_key(
            ["id", "region"],
            [(1, "us"), (2, "eu")],
        )
        assert isinstance(result, Or)

    def test_single_row_builds_and_without_or(self):
        """Single row produces And(...) directly (no Or wrapper)."""
        from pyiceberg.expressions import And

        result = _build_delete_filter_composite_key(
            ["id", "region"],
            [(1, "us")],
        )
        assert isinstance(result, And)

    def test_null_values_use_is_null(self):
        """NULL values in key columns produce IsNull() predicates."""
        from pyiceberg.expressions import And, IsNull

        result = _build_delete_filter_composite_key(
            ["id", "region"],
            [(1, None)],
        )
        assert isinstance(result, And)
        # The second predicate should be IsNull for the None value.
        # And stores children in .left and .right for binary, or nested for variadic.
        # Just verify it's an And (the filter builder handles None correctly).

    def test_empty_input_raises_value_error(self):
        """Empty key_value_rows raises ValueError."""
        with pytest.raises(ValueError, match="must not be empty"):
            _build_delete_filter_composite_key(["id", "region"], [])

    def test_three_rows_builds_correct_or(self):
        """Three rows produce Or(And(...), And(...), And(...))."""
        from pyiceberg.expressions import Or

        result = _build_delete_filter_composite_key(
            ["id", "region"],
            [(1, "us"), (2, "eu"), (3, "ap")],
        )
        assert isinstance(result, Or)


class TestGetRowCount:
    """Tests for _get_row_count helper."""

    def test_uses_snapshot_metadata(self):
        """Returns count from snapshot summary when available."""
        mock_table = MagicMock()
        mock_snapshot = MagicMock()
        mock_snapshot.summary = {"total-records": "42"}
        mock_table.current_snapshot.return_value = mock_snapshot

        result = _get_row_count(mock_table)

        assert result == 42
        # Should NOT call scan() — metadata path is O(1).
        mock_table.scan.assert_not_called()

    def test_falls_back_when_summary_missing(self):
        """Falls back to full scan when snapshot has no summary."""
        mock_table = MagicMock()
        mock_snapshot = MagicMock()
        mock_snapshot.summary = None
        mock_table.current_snapshot.return_value = mock_snapshot

        fallback_data = pa.table({"id": [1, 2, 3]})
        mock_scan = MagicMock()
        mock_scan.to_arrow.return_value = fallback_data
        mock_table.scan.return_value = mock_scan

        result = _get_row_count(mock_table)

        assert result == 3
        mock_table.scan.assert_called_once()

    def test_falls_back_when_no_snapshot(self):
        """Falls back to full scan when current_snapshot() returns None."""
        mock_table = MagicMock()
        mock_table.current_snapshot.return_value = None

        fallback_data = pa.table({"id": [1, 2]})
        mock_scan = MagicMock()
        mock_scan.to_arrow.return_value = fallback_data
        mock_table.scan.return_value = mock_scan

        result = _get_row_count(mock_table)

        assert result == 2

    def test_falls_back_on_exception(self):
        """Falls back to full scan when current_snapshot() raises."""
        mock_table = MagicMock()
        mock_table.current_snapshot.side_effect = RuntimeError("metadata error")

        fallback_data = pa.table({"id": [1]})
        mock_scan = MagicMock()
        mock_scan.to_arrow.return_value = fallback_data
        mock_table.scan.return_value = mock_scan

        result = _get_row_count(mock_table)

        assert result == 1

    def test_falls_back_when_total_records_missing(self):
        """Falls back when summary exists but lacks 'total-records' key."""
        mock_table = MagicMock()
        mock_snapshot = MagicMock()
        mock_snapshot.summary = {"some-other-key": "value"}
        mock_table.current_snapshot.return_value = mock_snapshot

        fallback_data = pa.table({"id": [1, 2, 3, 4]})
        mock_scan = MagicMock()
        mock_scan.to_arrow.return_value = fallback_data
        mock_table.scan.return_value = mock_scan

        result = _get_row_count(mock_table)

        assert result == 4


class TestTryOptimizedDeleteAppendComposite:
    """Tests for _try_optimized_delete_append with composite keys."""

    def test_composite_within_threshold_deletes_and_appends(self):
        """Composite key with rows <= threshold uses optimized path."""
        existing = pa.table(
            {"id": [1, 2, 3], "region": ["us", "eu", "ap"], "value": ["a", "b", "c"]}
        )
        filtered = pa.table({"id": [1, 2], "region": ["us", "eu"], "value": ["a", "b"]})
        mock_table = _make_optimized_mock_table(existing, filtered)
        new_data = pa.table({"id": [1, 2], "region": ["us", "eu"], "value": ["a2", "b2"]})

        result = _try_optimized_delete_append(mock_table, new_data, ["id", "region"])

        assert result == 3  # 3 - 2 + 2
        mock_table.delete.assert_called_once()
        mock_table.append.assert_called_once()

    def test_composite_exceeds_threshold_returns_none(self):
        """Composite key with rows > threshold returns None (fall back)."""
        # Create data with more unique rows than the threshold.
        n = _MAX_COMPOSITE_DELETE_ROWS + 1
        data = pa.table({"id": list(range(n)), "region": ["us"] * n, "value": ["x"] * n})
        mock_table = MagicMock()

        result = _try_optimized_delete_append(mock_table, data, ["id", "region"])

        assert result is None
        mock_table.delete.assert_not_called()

    def test_composite_missing_column_returns_none(self):
        """Returns None if any key column is missing from new_data."""
        mock_table = MagicMock()
        data = pa.table({"id": [1], "value": ["a"]})

        result = _try_optimized_delete_append(mock_table, data, ["id", "region"])

        assert result is None
        mock_table.delete.assert_not_called()

    def test_composite_with_null_keys(self):
        """Composite keys with NULL values use IsNull() in the filter."""
        existing = pa.table(
            {
                "id": pa.array([1, 2], type=pa.int64()),
                "region": pa.array(["us", None], type=pa.string()),
                "value": ["a", "b"],
            }
        )
        filtered = pa.table(
            {
                "id": pa.array([2], type=pa.int64()),
                "region": pa.array([None], type=pa.string()),
                "value": ["b"],
            }
        )
        mock_table = _make_optimized_mock_table(existing, filtered)
        new_data = pa.table(
            {
                "id": pa.array([2], type=pa.int64()),
                "region": pa.array([None], type=pa.string()),
                "value": ["b_new"],
            }
        )

        result = _try_optimized_delete_append(mock_table, new_data, ["id", "region"])

        assert result == 2  # 2 - 1 + 1
        mock_table.delete.assert_called_once()
        mock_table.append.assert_called_once()

    def test_composite_empty_new_data_returns_none(self):
        """Empty new_data returns None (no rows to extract keys from)."""
        mock_table = MagicMock()
        data = pa.table(
            {
                "id": pa.array([], type=pa.int64()),
                "region": pa.array([], type=pa.string()),
            }
        )

        result = _try_optimized_delete_append(mock_table, data, ["id", "region"])

        assert result is None
        mock_table.delete.assert_not_called()

    def test_composite_delete_failure_returns_none(self):
        """If table.delete() fails for composite keys, returns None (fallback)."""
        existing = pa.table({"id": [1], "region": ["us"], "value": ["a"]})
        filtered = pa.table(
            {
                "id": pa.array([], type=pa.int64()),
                "region": pa.array([], type=pa.string()),
                "value": pa.array([], type=pa.string()),
            }
        )
        mock_table = _make_optimized_mock_table(existing, filtered)
        mock_table.delete.side_effect = RuntimeError("PyIceberg internal error")
        new_data = pa.table({"id": [1], "region": ["us"], "value": ["a_new"]})

        result = _try_optimized_delete_append(mock_table, new_data, ["id", "region"])

        assert result is None


class TestDedupNewData:
    """Tests for _dedup_new_data helper."""

    def test_deduplicates_on_single_key(self):
        """Keeps last row per key (by position) when there are duplicates."""
        data = pa.table({"id": [1, 2, 2, 3], "value": ["a", "first", "second", "c"]})
        result = _dedup_new_data(data, ("id",))

        assert len(result) == 3
        result_dict = {
            result.column("id")[i].as_py(): result.column("value")[i].as_py()
            for i in range(len(result))
        }
        assert result_dict[2] == "second"  # last row wins

    def test_no_duplicates_returns_all_rows(self):
        """When there are no duplicates, all rows are returned."""
        data = pa.table({"id": [1, 2, 3], "value": ["a", "b", "c"]})
        result = _dedup_new_data(data, ("id",))

        assert len(result) == 3

    def test_deduplicates_on_composite_key(self):
        """Composite key deduplication keeps last row per key combo."""
        data = pa.table(
            {
                "id": [1, 1, 1],
                "region": ["us", "us", "eu"],
                "value": ["first", "second", "third"],
            }
        )
        result = _dedup_new_data(data, ("id", "region"))

        assert len(result) == 2  # (1, "us") and (1, "eu")

    def test_uses_provided_connection(self):
        """When a DuckDB connection is provided, it uses it (no auto-close)."""
        import duckdb

        conn = duckdb.connect(":memory:")
        data = pa.table({"id": [1, 1], "value": ["a", "b"]})
        result = _dedup_new_data(data, ("id",), conn=conn)

        assert len(result) == 1
        # Connection should still be usable after (not closed)
        conn.execute("SELECT 1").fetchone()
        conn.close()


class TestTryOptimizedDeleteAppend:
    """Tests for _try_optimized_delete_append helper."""

    def test_composite_key_within_threshold_uses_optimized_path(self):
        """Composite keys with small row count use optimized delete+append."""
        existing = pa.table({"id": [1, 2], "region": ["us", "eu"], "value": ["a", "b"]})
        filtered = pa.table({"id": [1], "region": ["us"], "value": ["a"]})
        mock_table = _make_optimized_mock_table(existing, filtered)
        new_data = pa.table({"id": [1], "region": ["us"], "value": ["a_new"]})

        result = _try_optimized_delete_append(mock_table, new_data, ["id", "region"])

        assert result == 2  # 2 - 1 + 1
        mock_table.delete.assert_called_once()
        mock_table.append.assert_called_once()

    def test_returns_none_for_missing_key_column(self):
        """If the key column is not in new_data, returns None."""
        mock_table = MagicMock()
        data = pa.table({"value": ["a"]})

        result = _try_optimized_delete_append(mock_table, data, ["id"])

        assert result is None

    def test_single_key_calls_delete_then_append(self):
        """Single-column key: calls table.delete(filter) then table.append(data)."""
        existing = pa.table({"id": [1, 2, 3], "value": ["a", "b", "c"]})
        filtered = pa.table({"id": [2], "value": ["b"]})
        mock_table = _make_optimized_mock_table(existing, filtered)
        new_data = pa.table({"id": [2, 4], "value": ["b_new", "d"]})

        result = _try_optimized_delete_append(mock_table, new_data, ["id"])

        assert result == 4  # 3 - 1 + 2
        mock_table.delete.assert_called_once()
        mock_table.append.assert_called_once()
        # Verify appended data is the new_data
        appended = mock_table.append.call_args[0][0]
        assert len(appended) == 2

    def test_returns_none_on_exception(self):
        """If delete() or append() raises, returns None (fallback)."""
        existing = pa.table({"id": [1], "value": ["a"]})
        filtered = pa.table(
            {"id": pa.array([], type=pa.int64()), "value": pa.array([], type=pa.string())}
        )
        mock_table = _make_optimized_mock_table(existing, filtered)
        mock_table.delete.side_effect = RuntimeError("PyIceberg internal error")
        new_data = pa.table({"id": [1], "value": ["a_new"]})

        result = _try_optimized_delete_append(mock_table, new_data, ["id"])

        assert result is None

    def test_no_matching_rows_still_appends(self):
        """When no existing rows match the key, delete is a no-op but append still runs."""
        existing = pa.table({"id": [1, 2], "value": ["a", "b"]})
        filtered = pa.table(
            {"id": pa.array([], type=pa.int64()), "value": pa.array([], type=pa.string())}
        )
        mock_table = _make_optimized_mock_table(existing, filtered)
        new_data = pa.table({"id": [3], "value": ["c"]})

        result = _try_optimized_delete_append(mock_table, new_data, ["id"])

        assert result == 3  # 2 - 0 + 1
        mock_table.delete.assert_called_once()
        mock_table.append.assert_called_once()
