"""Tests for soft-delete merge strategy."""

from __future__ import annotations

from datetime import UTC, datetime
from unittest.mock import MagicMock, patch

import pyarrow as pa
import pytest

from rat_plugin_soft_delete.strategy import SoftDeleteStrategy


# ── Protocol compliance ───────────────────────────────────────────


class TestProtocolCompliance:
    def test_implements_merge_strategy_protocol(self):
        from rat_runner.plugin_protocols import MergeStrategyProtocol

        strategy = SoftDeleteStrategy()
        assert isinstance(strategy, MergeStrategyProtocol)
        assert strategy.name == "soft_delete"


# ── Fallback behavior ────────────────────────────────────────────


class TestFallback:
    @patch("rat_plugin_soft_delete.strategy.write_iceberg", return_value=3)
    def test_falls_back_to_full_refresh_without_unique_key(self, mock_write):
        strategy = SoftDeleteStrategy()
        data = pa.table({"id": [1, 2, 3], "name": ["a", "b", "c"]})
        s3 = MagicMock()
        nessie = MagicMock()
        config = MagicMock()
        config.unique_key = ()  # empty — no unique key

        result = strategy.execute(data, "ns.silver.orders", s3, nessie, "s3://loc", config)

        assert result == 3
        mock_write.assert_called_once()

    @patch("rat_plugin_soft_delete.strategy.write_iceberg", return_value=2)
    def test_falls_back_to_full_refresh_with_config_none(self, mock_write):
        strategy = SoftDeleteStrategy()
        data = pa.table({"id": [1, 2]})
        s3 = MagicMock()
        nessie = MagicMock()

        result = strategy.execute(data, "ns.silver.orders", s3, nessie, "s3://loc", None)

        assert result == 2
        mock_write.assert_called_once()


# ── First run (no existing table) ────────────────────────────────


class TestFirstRun:
    @patch("rat_plugin_soft_delete.strategy.write_iceberg", return_value=2)
    @patch("rat_plugin_soft_delete.strategy.get_catalog")
    def test_first_run_adds_null_deleted_at(self, mock_catalog, mock_write):
        """When table doesn't exist yet, write data with _deleted_at = NULL."""
        mock_catalog.side_effect = Exception("table not found")

        strategy = SoftDeleteStrategy()
        data = pa.table({"id": [1, 2], "name": ["a", "b"]})
        s3 = MagicMock()
        nessie = MagicMock()
        config = MagicMock()
        config.unique_key = ("id",)

        result = strategy.execute(data, "ns.silver.orders", s3, nessie, "s3://loc", config)

        assert result == 2
        written_data = mock_write.call_args[0][0]
        assert "_deleted_at" in written_data.column_names
        # All _deleted_at values should be null
        assert written_data.column("_deleted_at").null_count == 2


# ── Soft-delete behavior ─────────────────────────────────────────


class TestSoftDelete:
    @patch("rat_plugin_soft_delete.strategy.write_iceberg")
    @patch("rat_plugin_soft_delete.strategy.get_catalog")
    def test_adds_deleted_at_to_missing_rows(self, mock_catalog, mock_write):
        """Rows in existing but not in incoming get _deleted_at timestamp."""
        # Existing table has rows 1, 2, 3 (all active).
        existing = pa.table({
            "id": [1, 2, 3],
            "name": ["a", "b", "c"],
            "_deleted_at": pa.array([None, None, None], type=pa.timestamp("us", tz="UTC")),
        })
        mock_table = MagicMock()
        mock_table.scan.return_value.to_arrow.return_value = existing
        mock_catalog.return_value.load_table.return_value = mock_table
        mock_write.return_value = 3

        # Incoming has only rows 1, 2 — row 3 is "deleted".
        incoming = pa.table({"id": [1, 2], "name": ["a", "b"]})

        strategy = SoftDeleteStrategy()
        s3 = MagicMock()
        nessie = MagicMock()
        config = MagicMock()
        config.unique_key = ("id",)

        result = strategy.execute(incoming, "ns.silver.orders", s3, nessie, "s3://loc", config)

        assert result == 3
        written_data = mock_write.call_args[0][0]
        assert "_deleted_at" in written_data.column_names
        assert len(written_data) == 3  # 2 incoming + 1 soft-deleted

        # Check that exactly 1 row has a non-null _deleted_at.
        deleted_col = written_data.column("_deleted_at")
        non_null_count = len(deleted_col) - deleted_col.null_count
        assert non_null_count == 1

    @patch("rat_plugin_soft_delete.strategy.write_iceberg")
    @patch("rat_plugin_soft_delete.strategy.get_catalog")
    def test_preserves_previously_deleted_rows(self, mock_catalog, mock_write):
        """Previously soft-deleted rows are preserved in the output."""
        old_ts = datetime(2024, 1, 1, tzinfo=UTC)
        existing = pa.table({
            "id": [1, 2, 3],
            "name": ["a", "b", "c"],
            "_deleted_at": pa.array(
                [None, None, old_ts], type=pa.timestamp("us", tz="UTC")
            ),
        })
        mock_table = MagicMock()
        mock_table.scan.return_value.to_arrow.return_value = existing
        mock_catalog.return_value.load_table.return_value = mock_table
        mock_write.return_value = 3

        # Incoming has rows 1 only — row 2 is newly deleted, row 3 was already deleted.
        incoming = pa.table({"id": [1], "name": ["a"]})

        strategy = SoftDeleteStrategy()
        s3 = MagicMock()
        nessie = MagicMock()
        config = MagicMock()
        config.unique_key = ("id",)

        strategy.execute(incoming, "ns.silver.orders", s3, nessie, "s3://loc", config)

        written_data = mock_write.call_args[0][0]
        assert len(written_data) == 3  # 1 incoming + 1 newly deleted + 1 previously deleted

        deleted_col = written_data.column("_deleted_at")
        non_null_count = len(deleted_col) - deleted_col.null_count
        assert non_null_count == 2  # row 2 (newly) + row 3 (previously)

    @patch("rat_plugin_soft_delete.strategy.write_iceberg")
    @patch("rat_plugin_soft_delete.strategy.get_catalog")
    def test_returns_correct_total_row_count(self, mock_catalog, mock_write):
        """Return value is the count from write_iceberg."""
        existing = pa.table({
            "id": [1, 2],
            "name": ["a", "b"],
            "_deleted_at": pa.array([None, None], type=pa.timestamp("us", tz="UTC")),
        })
        mock_table = MagicMock()
        mock_table.scan.return_value.to_arrow.return_value = existing
        mock_catalog.return_value.load_table.return_value = mock_table
        mock_write.return_value = 42

        strategy = SoftDeleteStrategy()
        incoming = pa.table({"id": [1, 2], "name": ["a", "b"]})
        s3 = MagicMock()
        nessie = MagicMock()
        config = MagicMock()
        config.unique_key = ("id",)

        result = strategy.execute(incoming, "ns.silver.orders", s3, nessie, "s3://loc", config)

        assert result == 42
