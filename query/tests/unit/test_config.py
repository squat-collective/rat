"""Tests for config — S3Config, DuckDBConfig, and NessieConfig.

These tests are aligned with runner/tests/unit/test_config.py for the shared
config classes (S3Config, DuckDBConfig, NessieConfig). The runner is the
canonical version; these tests verify query's copy stays in sync.
"""

from __future__ import annotations

from unittest.mock import patch

import pytest

from rat_query.config import DuckDBConfig, NessieConfig, S3Config


class TestS3Config:
    def test_defaults(self):
        config = S3Config()
        assert config.endpoint == "minio:9000"
        assert config.access_key == ""
        assert config.secret_key == ""
        assert config.bucket == "rat"
        assert config.use_ssl is False
        assert config.session_token == ""
        assert config.region == "us-east-1"

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
        assert config.session_token == ""
        assert config.region == "us-east-1"

    def test_from_env_custom(self):
        env = {
            "S3_ENDPOINT": "custom:9000",
            "S3_ACCESS_KEY": "key",
            "S3_SECRET_KEY": "secret",
            "S3_BUCKET": "mybucket",
            "S3_USE_SSL": "true",
            "S3_REGION": "eu-west-1",
        }
        with patch.dict("os.environ", env, clear=True):
            config = S3Config.from_env()
        assert config.endpoint == "custom:9000"
        assert config.access_key == "key"
        assert config.secret_key == "secret"
        assert config.bucket == "mybucket"
        assert config.use_ssl is True
        assert config.region == "eu-west-1"

    def test_session_token_default_empty(self):
        config = S3Config()
        assert config.session_token == ""

    def test_from_env_reads_session_token(self):
        env = {
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


class TestDuckDBConfig:
    def test_defaults(self):
        config = DuckDBConfig()
        assert config.memory_limit == "2GB"
        assert config.threads == 4

    def test_from_env_defaults(self):
        with patch.dict("os.environ", {}, clear=True):
            config = DuckDBConfig.from_env()
        assert config.memory_limit == "2GB"
        assert config.threads == 4

    def test_from_env_custom(self):
        env = {"DUCKDB_MEMORY_LIMIT": "4GB", "DUCKDB_THREADS": "8"}
        with patch.dict("os.environ", env, clear=True):
            config = DuckDBConfig.from_env()
        assert config.memory_limit == "4GB"
        assert config.threads == 8


class TestDuckDBConfigValidation:
    """Tests for DuckDBConfig.from_env thread value validation."""

    def test_from_env_rejects_zero_threads(self):
        env = {"DUCKDB_THREADS": "0"}
        with patch.dict("os.environ", env, clear=True):
            with pytest.raises(ValueError, match="positive integer"):
                DuckDBConfig.from_env()

    def test_from_env_rejects_negative_threads(self):
        env = {"DUCKDB_THREADS": "-2"}
        with patch.dict("os.environ", env, clear=True):
            with pytest.raises(ValueError, match="positive integer"):
                DuckDBConfig.from_env()

    def test_from_env_rejects_non_numeric_threads(self):
        env = {"DUCKDB_THREADS": "auto"}
        with patch.dict("os.environ", env, clear=True):
            with pytest.raises(ValueError, match="valid integer"):
                DuckDBConfig.from_env()

    def test_from_env_rejects_float_threads(self):
        env = {"DUCKDB_THREADS": "2.5"}
        with patch.dict("os.environ", env, clear=True):
            with pytest.raises(ValueError, match="valid integer"):
                DuckDBConfig.from_env()


class TestNessieConfig:
    def test_defaults(self):
        config = NessieConfig()
        assert config.url == "http://nessie:19120/api/v1"

    def test_from_env_default(self):
        with patch.dict("os.environ", {}, clear=True):
            config = NessieConfig.from_env()
        assert config.url == "http://nessie:19120/api/v1"

    def test_base_url_strips_api_v1(self):
        config = NessieConfig(url="http://nessie:19120/api/v1")
        assert config.base_url == "http://nessie:19120/iceberg"

    def test_base_url_strips_api_v2(self):
        config = NessieConfig(url="http://nessie:19120/api/v2")
        assert config.base_url == "http://nessie:19120/iceberg"

    def test_base_url_strips_trailing_slash(self):
        config = NessieConfig(url="http://nessie:19120/api/v1/")
        assert config.base_url == "http://nessie:19120/iceberg"

    def test_base_url_no_suffix(self):
        config = NessieConfig(url="http://nessie:19120")
        assert config.base_url == "http://nessie:19120/iceberg"

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
