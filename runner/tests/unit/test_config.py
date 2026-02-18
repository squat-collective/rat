"""Tests for config — S3Config, NessieConfig, parse_pipeline_config, read_s3_text."""

from __future__ import annotations

import logging
import time
from unittest.mock import MagicMock, patch

import pytest
from botocore.exceptions import ClientError

from rat_runner.config import (
    _BOTO3_CLIENT_TTL_SECONDS,
    NessieConfig,
    S3Config,
    _boto3_client,
    _boto3_client_cache,
    _boto3_client_cache_clear,
    list_s3_keys,
    merge_configs,
    move_s3_keys,
    parse_pipeline_config,
    read_s3_text,
    validate_pipeline_config,
)
from rat_runner.models import MergeStrategy, PipelineConfig


class TestS3Config:
    def test_defaults(self):
        config = S3Config()
        assert config.endpoint == "minio:9000"
        assert config.access_key == ""
        assert config.secret_key == ""
        assert config.bucket == "rat"
        assert config.use_ssl is False

    def test_endpoint_url_http(self):
        config = S3Config(endpoint="minio:9000", use_ssl=False)
        assert config.endpoint_url == "http://minio:9000"

    def test_endpoint_url_https(self):
        config = S3Config(endpoint="s3.amazonaws.com", use_ssl=True)
        assert config.endpoint_url == "https://s3.amazonaws.com"

    def test_from_env_raises_without_credentials(self):
        with patch.dict("os.environ", {}, clear=True):
            with pytest.raises(ValueError, match="S3_ACCESS_KEY and S3_SECRET_KEY"):
                S3Config.from_env()

    def test_from_env_raises_without_secret_key(self):
        with patch.dict("os.environ", {"S3_ACCESS_KEY": "key"}, clear=True):
            with pytest.raises(ValueError, match="S3_ACCESS_KEY and S3_SECRET_KEY"):
                S3Config.from_env()

    def test_from_env_raises_without_access_key(self):
        with patch.dict("os.environ", {"S3_SECRET_KEY": "secret"}, clear=True):
            with pytest.raises(ValueError, match="S3_ACCESS_KEY and S3_SECRET_KEY"):
                S3Config.from_env()

    def test_from_env_defaults(self):
        env = {"S3_ACCESS_KEY": "ak", "S3_SECRET_KEY": "sk"}
        with patch.dict("os.environ", env, clear=True):
            config = S3Config.from_env()
        assert config.endpoint == "minio:9000"
        assert config.bucket == "rat"
        assert config.access_key == "ak"
        assert config.secret_key == "sk"

    def test_from_env_custom(self):
        env = {
            "S3_ENDPOINT": "custom:9000",
            "S3_ACCESS_KEY": "key",
            "S3_SECRET_KEY": "secret",
            "S3_BUCKET": "mybucket",
            "S3_USE_SSL": "true",
        }
        with patch.dict("os.environ", env, clear=True):
            config = S3Config.from_env()
        assert config.endpoint == "custom:9000"
        assert config.access_key == "key"
        assert config.secret_key == "secret"
        assert config.bucket == "mybucket"
        assert config.use_ssl is True

    def test_with_overrides_full(self):
        config = S3Config()
        overridden = config.with_overrides(
            {
                "endpoint": "sts:9000",
                "access_key": "AKIA...",
                "secret_key": "SECRET...",
                "bucket": "other",
                "use_ssl": "true",
            }
        )
        assert overridden.endpoint == "sts:9000"
        assert overridden.access_key == "AKIA..."
        assert overridden.secret_key == "SECRET..."
        assert overridden.bucket == "other"
        assert overridden.use_ssl is True

    def test_with_overrides_empty_returns_self(self):
        config = S3Config()
        result = config.with_overrides({})
        assert result is config

    def test_with_overrides_partial(self):
        config = S3Config(endpoint="orig:9000", access_key="orig_key")
        overridden = config.with_overrides({"access_key": "new_key"})
        assert overridden.endpoint == "orig:9000"
        assert overridden.access_key == "new_key"

    def test_session_token_default_empty(self):
        config = S3Config()
        assert config.session_token == ""

    def test_from_env_reads_session_token(self):
        env = {
            "S3_ENDPOINT": "minio:9000",
            "S3_ACCESS_KEY": "test-key",
            "S3_SECRET_KEY": "test-secret",
            "S3_SESSION_TOKEN": "sts-token-abc",
        }
        with patch.dict("os.environ", env, clear=True):
            config = S3Config.from_env()
        assert config.session_token == "sts-token-abc"

    def test_from_env_session_token_empty_when_not_set(self):
        env = {
            "S3_ACCESS_KEY": "test-key",
            "S3_SECRET_KEY": "test-secret",
        }
        with patch.dict("os.environ", env, clear=True):
            config = S3Config.from_env()
        assert config.session_token == ""

    def test_with_overrides_session_token(self):
        config = S3Config()
        overridden = config.with_overrides({"session_token": "sts-xyz"})
        assert overridden.session_token == "sts-xyz"

    def test_with_overrides_preserves_session_token(self):
        config = S3Config(session_token="existing-token")
        overridden = config.with_overrides({"access_key": "new_key"})
        assert overridden.session_token == "existing-token"


class TestBoto3Client:
    def test_no_session_token(self):
        config = S3Config(endpoint="minio:9000", access_key="ak", secret_key="sk")
        with patch("rat_runner.config.boto3.client") as mock_client:
            _boto3_client(config)
        mock_client.assert_called_once_with(
            "s3",
            endpoint_url="http://minio:9000",
            aws_access_key_id="ak",
            aws_secret_access_key="sk",
        )

    def test_with_session_token(self):
        config = S3Config(
            endpoint="minio:9000", access_key="ak", secret_key="sk", session_token="tok"
        )
        with patch("rat_runner.config.boto3.client") as mock_client:
            _boto3_client(config)
        mock_client.assert_called_once_with(
            "s3",
            endpoint_url="http://minio:9000",
            aws_access_key_id="ak",
            aws_secret_access_key="sk",
            aws_session_token="tok",
        )

    def test_returns_cached_client_within_ttl(self):
        config = S3Config(endpoint="minio:9000", access_key="ak", secret_key="sk")
        with patch("rat_runner.config.boto3.client") as mock_client:
            client1 = _boto3_client(config)
            client2 = _boto3_client(config)
        assert client1 is client2
        # boto3.client should only be called once — second call returns cached
        mock_client.assert_called_once()

    def test_creates_new_client_after_ttl_expires(self):
        config = S3Config(endpoint="minio:9000", access_key="ak", secret_key="sk")
        with patch("rat_runner.config.boto3.client") as mock_client:
            mock_client.side_effect = [MagicMock(name="client1"), MagicMock(name="client2")]

            client1 = _boto3_client(config)

            # Simulate TTL expiry by backdating the cached timestamp
            _boto3_client_cache[config] = (
                _boto3_client_cache[config][0],
                time.monotonic() - _BOTO3_CLIENT_TTL_SECONDS - 1,
            )

            client2 = _boto3_client(config)

        assert client1 is not client2
        assert mock_client.call_count == 2

    def test_ttl_is_45_minutes(self):
        assert _BOTO3_CLIENT_TTL_SECONDS == 45 * 60

    def test_cache_clear_removes_all_entries(self):
        config = S3Config(endpoint="minio:9000", access_key="ak", secret_key="sk")
        with patch("rat_runner.config.boto3.client"):
            _boto3_client(config)
        assert len(_boto3_client_cache) == 1
        _boto3_client_cache_clear()
        assert len(_boto3_client_cache) == 0

    def test_different_configs_cached_separately(self):
        config1 = S3Config(endpoint="minio:9000", access_key="ak1", secret_key="sk1")
        config2 = S3Config(endpoint="minio:9000", access_key="ak2", secret_key="sk2")
        with patch("rat_runner.config.boto3.client") as mock_client:
            mock_client.side_effect = [MagicMock(name="client1"), MagicMock(name="client2")]
            client1 = _boto3_client(config1)
            client2 = _boto3_client(config2)
        assert client1 is not client2
        assert mock_client.call_count == 2
        assert len(_boto3_client_cache) == 2


class TestNessieConfig:
    def test_base_url_strips_api_v1(self):
        config = NessieConfig(url="http://nessie:19120/api/v1")
        assert config.base_url == "http://nessie:19120/iceberg"

    def test_base_url_strips_api_v2(self):
        config = NessieConfig(url="http://nessie:19120/api/v2")
        assert config.base_url == "http://nessie:19120/iceberg"

    def test_base_url_no_suffix(self):
        config = NessieConfig(url="http://nessie:19120")
        assert config.base_url == "http://nessie:19120/iceberg"

    def test_base_url_strips_trailing_slash(self):
        config = NessieConfig(url="http://nessie:19120/api/v1/")
        assert config.base_url == "http://nessie:19120/iceberg"

    def test_from_env_default(self):
        with patch.dict("os.environ", {}, clear=True):
            config = NessieConfig.from_env()
        assert config.url == "http://nessie:19120/api/v1"

    def test_api_v2_url(self):
        config = NessieConfig(url="http://nessie:19120/api/v1")
        assert config.api_v2_url == "http://nessie:19120/api/v2"

    def test_api_v2_url_no_suffix(self):
        config = NessieConfig(url="http://nessie:19120")
        assert config.api_v2_url == "http://nessie:19120/api/v2"

    def test_api_v2_url_from_iceberg_suffix(self):
        config = NessieConfig(url="http://nessie:19120/iceberg")
        assert config.api_v2_url == "http://nessie:19120/api/v2"
        assert config.base_url == "http://nessie:19120/iceberg"


class TestDuckDBConfig:
    def test_defaults(self):
        from rat_runner.config import DuckDBConfig

        config = DuckDBConfig()
        assert config.memory_limit == "2GB"
        assert config.threads == 4

    def test_from_env_defaults(self):
        from rat_runner.config import DuckDBConfig

        with patch.dict("os.environ", {}, clear=True):
            config = DuckDBConfig.from_env()
        assert config.memory_limit == "2GB"
        assert config.threads == 4

    def test_from_env_custom(self):
        from rat_runner.config import DuckDBConfig

        env = {"DUCKDB_MEMORY_LIMIT": "4GB", "DUCKDB_THREADS": "8"}
        with patch.dict("os.environ", env, clear=True):
            config = DuckDBConfig.from_env()
        assert config.memory_limit == "4GB"
        assert config.threads == 8

    def test_from_env_rejects_zero_threads(self):
        from rat_runner.config import DuckDBConfig

        env = {"DUCKDB_THREADS": "0"}
        with patch.dict("os.environ", env, clear=True):
            with pytest.raises(ValueError, match="positive integer"):
                DuckDBConfig.from_env()

    def test_from_env_rejects_negative_threads(self):
        from rat_runner.config import DuckDBConfig

        env = {"DUCKDB_THREADS": "-2"}
        with patch.dict("os.environ", env, clear=True):
            with pytest.raises(ValueError, match="positive integer"):
                DuckDBConfig.from_env()

    def test_from_env_rejects_non_numeric_threads(self):
        from rat_runner.config import DuckDBConfig

        env = {"DUCKDB_THREADS": "auto"}
        with patch.dict("os.environ", env, clear=True):
            with pytest.raises(ValueError, match="valid integer"):
                DuckDBConfig.from_env()

    def test_from_env_rejects_float_threads(self):
        from rat_runner.config import DuckDBConfig

        env = {"DUCKDB_THREADS": "2.5"}
        with patch.dict("os.environ", env, clear=True):
            with pytest.raises(ValueError, match="valid integer"):
                DuckDBConfig.from_env()


class TestParsePipelineConfig:
    def test_minimal_yaml(self):
        config = parse_pipeline_config("description: My pipeline")
        assert config.description == "My pipeline"
        assert config.materialized == "table"

    def test_full_fields(self):
        yaml_str = """
description: Order cleanup
materialized: table
unique_key:
  - order_id
  - product_id
merge_strategy: incremental
"""
        config = parse_pipeline_config(yaml_str)
        assert config.description == "Order cleanup"
        assert config.materialized == "table"
        assert config.unique_key == ("order_id", "product_id")
        assert config.merge_strategy == "incremental"

    def test_unique_key_as_csv_string(self):
        config = parse_pipeline_config("unique_key: order_id, product_id")
        assert config.unique_key == ("order_id", "product_id")

    def test_invalid_yaml_returns_defaults(self):
        config = parse_pipeline_config("just a string")
        assert config.description == ""
        assert config.unique_key == ()

    def test_watermark_column(self):
        config = parse_pipeline_config("watermark_column: updated_at")
        assert config.watermark_column == "updated_at"

    def test_watermark_column_default_empty(self):
        config = parse_pipeline_config("description: test")
        assert config.watermark_column == ""

    def test_partition_column(self):
        config = parse_pipeline_config("partition_column: date")
        assert config.partition_column == "date"

    def test_scd_columns(self):
        yaml_str = """
scd_valid_from: start_ts
scd_valid_to: end_ts
"""
        config = parse_pipeline_config(yaml_str)
        assert config.scd_valid_from == "start_ts"
        assert config.scd_valid_to == "end_ts"

    def test_scd_columns_default(self):
        config = parse_pipeline_config("description: test")
        assert config.scd_valid_from == "valid_from"
        assert config.scd_valid_to == "valid_to"

    def test_new_strategies_parsed(self):
        for strategy in ("append_only", "delete_insert", "scd2", "snapshot"):
            config = parse_pipeline_config(f"merge_strategy: {strategy}")
            assert config.merge_strategy == strategy

    def test_partition_by_single_entry(self):
        yaml_str = """
partition_by:
  - column: created_date
    transform: day
"""
        config = parse_pipeline_config(yaml_str)
        assert len(config.partition_by) == 1
        assert config.partition_by[0].column == "created_date"
        assert config.partition_by[0].transform == "day"

    def test_partition_by_multiple_entries(self):
        yaml_str = """
partition_by:
  - column: created_date
    transform: month
  - column: region
    transform: identity
"""
        config = parse_pipeline_config(yaml_str)
        assert len(config.partition_by) == 2
        assert config.partition_by[0].column == "created_date"
        assert config.partition_by[0].transform == "month"
        assert config.partition_by[1].column == "region"
        assert config.partition_by[1].transform == "identity"

    def test_partition_by_defaults_to_identity(self):
        yaml_str = """
partition_by:
  - column: region
"""
        config = parse_pipeline_config(yaml_str)
        assert len(config.partition_by) == 1
        assert config.partition_by[0].transform == "identity"

    def test_partition_by_empty_list(self):
        yaml_str = """
partition_by: []
"""
        config = parse_pipeline_config(yaml_str)
        assert config.partition_by == ()

    def test_partition_by_not_set(self):
        config = parse_pipeline_config("description: test")
        assert config.partition_by == ()

    def test_partition_by_all_transforms(self):
        yaml_str = """
partition_by:
  - column: ts
    transform: identity
  - column: ts
    transform: day
  - column: ts
    transform: month
  - column: ts
    transform: year
  - column: ts
    transform: hour
"""
        config = parse_pipeline_config(yaml_str)
        assert len(config.partition_by) == 5
        transforms = [e.transform for e in config.partition_by]
        assert transforms == ["identity", "day", "month", "year", "hour"]


class TestValidatePartitionBy:
    """Tests for partition_by validation in validate_pipeline_config."""

    def test_valid_partition_by_accepted(self):
        data = {"partition_by": [{"column": "date", "transform": "day"}]}
        validate_pipeline_config(data)  # should not raise

    def test_partition_by_not_a_list_raises(self):
        data = {"partition_by": "date"}
        with pytest.raises(ValueError, match="partition_by must be a list"):
            validate_pipeline_config(data)

    def test_partition_by_entry_not_a_dict_raises(self):
        data = {"partition_by": ["date"]}
        with pytest.raises(ValueError, match="partition_by\\[0\\] must be a mapping"):
            validate_pipeline_config(data)

    def test_partition_by_entry_missing_column_raises(self):
        data = {"partition_by": [{"transform": "day"}]}
        with pytest.raises(ValueError, match="partition_by\\[0\\] is missing required 'column'"):
            validate_pipeline_config(data)

    def test_partition_by_invalid_transform_raises(self):
        data = {"partition_by": [{"column": "date", "transform": "bucket[16]"}]}
        with pytest.raises(ValueError, match="Invalid partition transform 'bucket\\[16\\]'"):
            validate_pipeline_config(data)

    def test_partition_by_default_transform_is_valid(self):
        """An entry without transform should default to identity and pass validation."""
        data = {"partition_by": [{"column": "region"}]}
        validate_pipeline_config(data)  # should not raise

    def test_partition_by_none_is_ok(self):
        """Omitting partition_by should not raise."""
        validate_pipeline_config({"description": "no partitions"})


class TestValidatePipelineConfig:
    """Tests for validate_pipeline_config — unknown key warnings and value validation."""

    def test_known_keys_no_warnings(self, caplog):
        """All known keys should pass without warnings."""
        data = {
            "description": "test",
            "materialized": "table",
            "unique_key": ["id"],
            "merge_strategy": "full_refresh",
            "watermark_column": "updated_at",
            "archive_landing_zones": True,
            "partition_column": "date",
            "partition_by": [{"column": "date", "transform": "day"}],
            "scd_valid_from": "start_ts",
            "scd_valid_to": "end_ts",
        }
        with caplog.at_level(logging.WARNING, logger="rat_runner.config"):
            validate_pipeline_config(data)
        assert len(caplog.records) == 0

    def test_unknown_key_warns(self, caplog):
        """Unknown keys should produce a warning, not an error."""
        data = {"description": "test", "typo_key": "value"}
        with caplog.at_level(logging.WARNING, logger="rat_runner.config"):
            validate_pipeline_config(data)
        assert len(caplog.records) == 1
        assert "Unknown pipeline config key 'typo_key'" in caplog.records[0].message

    def test_multiple_unknown_keys_warn_sorted(self, caplog):
        """Multiple unknown keys should each produce a warning, in sorted order."""
        data = {"zebra": 1, "aardvark": 2, "description": "ok"}
        with caplog.at_level(logging.WARNING, logger="rat_runner.config"):
            validate_pipeline_config(data)
        assert len(caplog.records) == 2
        assert "aardvark" in caplog.records[0].message
        assert "zebra" in caplog.records[1].message

    def test_invalid_merge_strategy_raises(self):
        """An invalid merge_strategy should raise ValueError immediately."""
        data = {"merge_strategy": "upsert"}
        with pytest.raises(ValueError, match="Invalid merge_strategy 'upsert'"):
            validate_pipeline_config(data)

    def test_all_valid_merge_strategies_accepted(self):
        """Every strategy in MergeStrategy should be accepted."""
        for strategy in MergeStrategy:
            validate_pipeline_config({"merge_strategy": strategy})

    def test_merge_strategy_not_present_is_ok(self):
        """Omitting merge_strategy should not raise."""
        validate_pipeline_config({"description": "no strategy"})

    def test_invalid_materialized_raises(self):
        """An invalid materialized value should raise ValueError."""
        data = {"materialized": "snapshot"}
        with pytest.raises(ValueError, match="Invalid materialized 'snapshot'"):
            validate_pipeline_config(data)

    def test_valid_materialized_table(self):
        """materialized='table' should be accepted."""
        validate_pipeline_config({"materialized": "table"})

    def test_valid_materialized_view(self):
        """materialized='view' should be accepted."""
        validate_pipeline_config({"materialized": "view"})

    def test_materialized_not_present_is_ok(self):
        """Omitting materialized should not raise."""
        validate_pipeline_config({"description": "no materialized"})

    def test_unknown_key_and_invalid_strategy_raises_before_warning(self):
        """Invalid merge_strategy raises even if unknown keys are also present."""
        data = {"merge_strategy": "bogus", "unknown_key": "val"}
        with pytest.raises(ValueError, match="Invalid merge_strategy 'bogus'"):
            validate_pipeline_config(data)

    def test_parse_pipeline_config_rejects_invalid_strategy(self):
        """parse_pipeline_config should reject invalid merge_strategy via validation."""
        with pytest.raises(ValueError, match="Invalid merge_strategy"):
            parse_pipeline_config("merge_strategy: upsert")

    def test_parse_pipeline_config_warns_on_unknown_key(self, caplog):
        """parse_pipeline_config should warn on unknown keys but still parse."""
        yaml_str = """
description: My pipeline
unknown_option: true
"""
        with caplog.at_level(logging.WARNING, logger="rat_runner.config"):
            config = parse_pipeline_config(yaml_str)
        assert config.description == "My pipeline"
        assert len(caplog.records) == 1
        assert "unknown_option" in caplog.records[0].message

    def test_parse_pipeline_config_rejects_invalid_materialized(self):
        """parse_pipeline_config should reject invalid materialized values."""
        with pytest.raises(ValueError, match="Invalid materialized"):
            parse_pipeline_config("materialized: ephemeral")

    def test_empty_dict_is_valid(self):
        """An empty config dict should pass validation."""
        validate_pipeline_config({})


class TestMergeConfigs:
    def test_annotations_win_over_base(self):
        base = PipelineConfig(merge_strategy="full_refresh", description="base")
        result = merge_configs(base, {"merge_strategy": "incremental"})
        assert result.merge_strategy == "incremental"
        assert result.description == "base"

    def test_base_only(self):
        base = PipelineConfig(merge_strategy="scd2", unique_key=("id",))
        result = merge_configs(base, {})
        assert result is base

    def test_annotations_only(self):
        result = merge_configs(None, {"merge_strategy": "append_only"})
        assert result.merge_strategy == "append_only"

    def test_both_none(self):
        result = merge_configs(None, {})
        assert result == PipelineConfig()

    def test_unique_key_from_annotation(self):
        base = PipelineConfig(unique_key=("old_key",))
        result = merge_configs(base, {"unique_key": "id, email"})
        assert result.unique_key == ("id", "email")

    def test_unique_key_fallback_to_base(self):
        base = PipelineConfig(unique_key=("id",))
        result = merge_configs(base, {"description": "override desc"})
        assert result.unique_key == ("id",)
        assert result.description == "override desc"

    def test_new_fields_from_annotations(self):
        result = merge_configs(
            None,
            {
                "partition_column": "date",
                "scd_valid_from": "start_ts",
                "scd_valid_to": "end_ts",
            },
        )
        assert result.partition_column == "date"
        assert result.scd_valid_from == "start_ts"
        assert result.scd_valid_to == "end_ts"

    def test_archive_landing_zones_annotation(self):
        base = PipelineConfig(archive_landing_zones=False)
        result = merge_configs(base, {"archive_landing_zones": "true"})
        assert result.archive_landing_zones is True


class TestReadS3Text:
    def test_reads_file(self, s3_config: S3Config):
        mock_body = MagicMock()
        mock_body.read.return_value = b"SELECT 1"
        mock_client = MagicMock()
        mock_client.get_object.return_value = {"Body": mock_body}

        with patch("rat_runner.config.boto3.client", return_value=mock_client):
            result = read_s3_text(s3_config, "path/to/file.sql")

        assert result == "SELECT 1"
        mock_client.get_object.assert_called_once_with(Bucket="test-bucket", Key="path/to/file.sql")

    def test_returns_none_on_no_such_key(self, s3_config: S3Config):
        mock_client = MagicMock()
        mock_client.get_object.side_effect = ClientError(
            {"Error": {"Code": "NoSuchKey", "Message": "Not found"}}, "GetObject"
        )

        with patch("rat_runner.config.boto3.client", return_value=mock_client):
            result = read_s3_text(s3_config, "missing.sql")

        assert result is None

    def test_raises_on_other_errors(self, s3_config: S3Config):
        mock_client = MagicMock()
        mock_client.get_object.side_effect = ClientError(
            {"Error": {"Code": "AccessDenied", "Message": "Forbidden"}}, "GetObject"
        )

        with patch("rat_runner.config.boto3.client", return_value=mock_client):
            try:
                read_s3_text(s3_config, "forbidden.sql")
                assert False, "Should have raised"
            except ClientError as e:
                assert e.response["Error"]["Code"] == "AccessDenied"


class TestListS3Keys:
    def test_lists_keys_with_prefix(self, s3_config: S3Config):
        mock_client = MagicMock()
        mock_paginator = MagicMock()
        mock_paginator.paginate.return_value = [
            {
                "Contents": [
                    {"Key": "ns/tests/quality/test1.sql"},
                    {"Key": "ns/tests/quality/test2.sql"},
                ]
            }
        ]
        mock_client.get_paginator.return_value = mock_paginator

        with patch("rat_runner.config.boto3.client", return_value=mock_client):
            keys = list_s3_keys(s3_config, "ns/tests/quality/")

        assert keys == ["ns/tests/quality/test1.sql", "ns/tests/quality/test2.sql"]

    def test_filters_by_suffix(self, s3_config: S3Config):
        mock_client = MagicMock()
        mock_paginator = MagicMock()
        mock_paginator.paginate.return_value = [
            {
                "Contents": [
                    {"Key": "ns/tests/quality/test1.sql"},
                    {"Key": "ns/tests/quality/README.md"},
                ]
            }
        ]
        mock_client.get_paginator.return_value = mock_paginator

        with patch("rat_runner.config.boto3.client", return_value=mock_client):
            keys = list_s3_keys(s3_config, "ns/tests/quality/", suffix=".sql")

        assert keys == ["ns/tests/quality/test1.sql"]

    def test_empty_when_no_contents(self, s3_config: S3Config):
        mock_client = MagicMock()
        mock_paginator = MagicMock()
        mock_paginator.paginate.return_value = [{}]  # no Contents key
        mock_client.get_paginator.return_value = mock_paginator

        with patch("rat_runner.config.boto3.client", return_value=mock_client):
            keys = list_s3_keys(s3_config, "ns/empty/")

        assert keys == []


class TestMoveS3Keys:
    def test_copies_and_deletes(self, s3_config: S3Config):
        mock_client = MagicMock()
        src_keys = [
            "myns/landing/orders/file1.csv",
            "myns/landing/orders/file2.csv",
        ]

        with patch("rat_runner.config.boto3.client", return_value=mock_client):
            move_s3_keys(
                s3_config,
                src_keys,
                "myns/landing/orders/",
                "myns/landing/orders/_processed/",
            )

        # Verify copy_object called for each key
        assert mock_client.copy_object.call_count == 2
        mock_client.copy_object.assert_any_call(
            Bucket="test-bucket",
            CopySource={"Bucket": "test-bucket", "Key": "myns/landing/orders/file1.csv"},
            Key="myns/landing/orders/_processed/file1.csv",
        )
        mock_client.copy_object.assert_any_call(
            Bucket="test-bucket",
            CopySource={"Bucket": "test-bucket", "Key": "myns/landing/orders/file2.csv"},
            Key="myns/landing/orders/_processed/file2.csv",
        )

        # Verify delete_objects called once with all keys
        mock_client.delete_objects.assert_called_once_with(
            Bucket="test-bucket",
            Delete={
                "Objects": [
                    {"Key": "myns/landing/orders/file1.csv"},
                    {"Key": "myns/landing/orders/file2.csv"},
                ]
            },
        )

    def test_empty_keys_is_noop(self, s3_config: S3Config):
        mock_client = MagicMock()

        with patch("rat_runner.config.boto3.client", return_value=mock_client):
            move_s3_keys(s3_config, [], "src/", "dest/")

        mock_client.copy_object.assert_not_called()
        mock_client.delete_objects.assert_not_called()
