"""Tests for the Iceberg maintenance module."""

from __future__ import annotations

from unittest.mock import MagicMock, patch

import pytest

from rat_runner.config import NessieConfig, S3Config
from rat_runner.maintenance import expire_snapshots, remove_orphan_files, run_maintenance


@pytest.fixture
def s3_config() -> S3Config:
    return S3Config(
        endpoint="minio:9000",
        access_key="test-access-key",
        secret_key="test-secret-key",
        bucket="rat",
    )


@pytest.fixture
def nessie_config() -> NessieConfig:
    return NessieConfig(url="http://nessie:19120/api/v1")


class TestExpireSnapshots:
    @patch("rat_runner.maintenance.get_catalog")
    def test_returns_zero_on_invalid_table_name(
        self, mock_get_catalog: MagicMock, s3_config: S3Config, nessie_config: NessieConfig
    ) -> None:
        mock_get_catalog.return_value = MagicMock()
        result = expire_snapshots("invalid", 7, s3_config, nessie_config)
        assert result == 0

    @patch("rat_runner.maintenance.get_catalog")
    def test_expires_old_snapshots(
        self, mock_get_catalog: MagicMock, s3_config: S3Config, nessie_config: NessieConfig
    ) -> None:
        mock_catalog = MagicMock()
        mock_get_catalog.return_value = mock_catalog

        mock_table = MagicMock()
        mock_table.metadata.snapshots = [MagicMock(), MagicMock(), MagicMock()]
        mock_catalog.load_table.return_value = mock_table

        # After commit, reload shows fewer snapshots
        mock_table_after = MagicMock()
        mock_table_after.metadata.snapshots = [MagicMock()]
        mock_catalog.load_table.side_effect = [mock_table, mock_table_after]

        mock_manage = MagicMock()
        mock_table.manage_snapshots.return_value = mock_manage
        mock_manage.expire_snapshots_older_than.return_value = mock_manage

        result = expire_snapshots("default.silver.orders", 7, s3_config, nessie_config)
        assert result == 2
        mock_manage.commit.assert_called_once()

    @patch("rat_runner.maintenance.get_catalog")
    def test_no_snapshots_to_expire(
        self, mock_get_catalog: MagicMock, s3_config: S3Config, nessie_config: NessieConfig
    ) -> None:
        mock_catalog = MagicMock()
        mock_get_catalog.return_value = mock_catalog

        mock_table = MagicMock()
        mock_table.metadata.snapshots = [MagicMock()]
        mock_catalog.load_table.return_value = mock_table

        mock_manage = MagicMock()
        mock_table.manage_snapshots.return_value = mock_manage
        mock_manage.expire_snapshots_older_than.return_value = mock_manage

        # Same count after commit
        mock_table_after = MagicMock()
        mock_table_after.metadata.snapshots = [MagicMock()]
        mock_catalog.load_table.side_effect = [mock_table, mock_table_after]

        result = expire_snapshots("default.silver.orders", 7, s3_config, nessie_config)
        assert result == 0

    @patch("rat_runner.maintenance.get_catalog", side_effect=Exception("catalog error"))
    def test_returns_zero_on_catalog_error(
        self, mock_get_catalog: MagicMock, s3_config: S3Config, nessie_config: NessieConfig
    ) -> None:
        result = expire_snapshots("default.silver.orders", 7, s3_config, nessie_config)
        assert result == 0


class TestRemoveOrphanFiles:
    @patch("rat_runner.maintenance.get_catalog", side_effect=Exception("catalog error"))
    def test_returns_zero_on_error(
        self, mock_get_catalog: MagicMock, s3_config: S3Config, nessie_config: NessieConfig
    ) -> None:
        result = remove_orphan_files("default.silver.orders", 3, s3_config, nessie_config)
        assert result == 0


class TestRunMaintenance:
    @patch("rat_runner.maintenance.expire_snapshots", return_value=2)
    @patch("rat_runner.maintenance.remove_orphan_files", return_value=1)
    def test_runs_both_tasks(
        self,
        mock_remove: MagicMock,
        mock_expire: MagicMock,
        s3_config: S3Config,
        nessie_config: NessieConfig,
    ) -> None:
        mock_log = MagicMock()
        run_maintenance("default.silver.orders", s3_config, nessie_config, log=mock_log)

        mock_expire.assert_called_once_with("default.silver.orders", 7, s3_config, nessie_config)
        mock_remove.assert_called_once_with("default.silver.orders", 3, s3_config, nessie_config)
        assert mock_log.info.call_count >= 3  # start + expired + removed + complete

    @patch("rat_runner.maintenance.expire_snapshots", side_effect=Exception("boom"))
    def test_continues_on_failure(
        self, mock_expire: MagicMock, s3_config: S3Config, nessie_config: NessieConfig
    ) -> None:
        mock_log = MagicMock()
        # Should not raise
        run_maintenance("default.silver.orders", s3_config, nessie_config, log=mock_log)
        mock_log.warn.assert_called_once()

    @patch("rat_runner.maintenance.expire_snapshots", return_value=0)
    @patch("rat_runner.maintenance.remove_orphan_files", return_value=0)
    def test_no_log_for_zero_results(
        self,
        mock_remove: MagicMock,
        mock_expire: MagicMock,
        s3_config: S3Config,
        nessie_config: NessieConfig,
    ) -> None:
        mock_log = MagicMock()
        run_maintenance("default.silver.orders", s3_config, nessie_config, log=mock_log)

        # Should log start and complete, but not "expired 0" or "removed 0"
        info_messages = [str(call) for call in mock_log.info.call_args_list]
        assert any("Running" in m for m in info_messages)
        assert any("complete" in m for m in info_messages)
