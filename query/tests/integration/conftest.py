"""Shared fixtures for query integration tests.

Integration tests run against real DuckDB with its extensions. Most tests
only need DuckDB locally (no running services). Tests that access S3 or
Nessie are gated by environment variables.

Set the following environment variables for full integration:
    S3_ENDPOINT     — MinIO endpoint (e.g., localhost:9000)
    S3_ACCESS_KEY   — MinIO access key
    S3_SECRET_KEY   — MinIO secret key
    NESSIE_URL      — Nessie API URL (e.g., http://localhost:19120/api/v1)
"""

from __future__ import annotations

import os

import pytest

from rat_query.config import DuckDBConfig, NessieConfig, S3Config
from rat_query.engine import QueryEngine

# ---------------------------------------------------------------------------
# Skip conditions
# ---------------------------------------------------------------------------

_s3_available = bool(
    os.environ.get("S3_ENDPOINT")
    and os.environ.get("S3_ACCESS_KEY")
    and os.environ.get("S3_SECRET_KEY")
)
_nessie_available = bool(os.environ.get("NESSIE_URL"))

requires_s3 = pytest.mark.skipif(
    not _s3_available,
    reason="S3_ENDPOINT, S3_ACCESS_KEY, and S3_SECRET_KEY env vars required",
)
requires_nessie = pytest.mark.skipif(
    not _nessie_available,
    reason="NESSIE_URL env var required",
)
requires_s3_and_nessie = pytest.mark.skipif(
    not (_s3_available and _nessie_available),
    reason="S3_ENDPOINT, S3_ACCESS_KEY, S3_SECRET_KEY, and NESSIE_URL env vars required",
)

# ---------------------------------------------------------------------------
# Fixtures
# ---------------------------------------------------------------------------


@pytest.fixture
def s3_config() -> S3Config:
    """Build S3Config from environment variables for integration tests."""
    return S3Config(
        endpoint=os.environ.get("S3_ENDPOINT", "localhost:9000"),
        access_key=os.environ.get("S3_ACCESS_KEY", ""),
        secret_key=os.environ.get("S3_SECRET_KEY", ""),
        bucket=os.environ.get("S3_BUCKET", "rat-integration-test"),
        use_ssl=os.environ.get("S3_USE_SSL", "false").lower() == "true",
        region=os.environ.get("S3_REGION", "us-east-1"),
    )


@pytest.fixture
def nessie_config() -> NessieConfig:
    """Build NessieConfig from environment variables for integration tests."""
    return NessieConfig(
        url=os.environ.get("NESSIE_URL", "http://localhost:19120/api/v1"),
    )


@pytest.fixture
def duckdb_config() -> DuckDBConfig:
    """Conservative DuckDB config for integration tests."""
    return DuckDBConfig(memory_limit="512MB", threads=2)


@pytest.fixture
def query_engine(s3_config: S3Config, duckdb_config: DuckDBConfig) -> QueryEngine:
    """Create a real QueryEngine with DuckDB extensions loaded.

    Yields the engine and closes it after the test.
    """
    engine = QueryEngine(s3_config, duckdb_config)
    yield engine  # type: ignore[misc]
    engine.close()
