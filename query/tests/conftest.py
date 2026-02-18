"""Shared test fixtures for the query test suite."""

from __future__ import annotations

import sys
from pathlib import Path

import pytest

from rat_query.config import NessieConfig, S3Config

# Add gen/ to sys.path for proto imports (same as __main__.py)
_gen_dir = Path(__file__).parent.parent / "src" / "rat_query" / "gen"
if str(_gen_dir) not in sys.path:
    sys.path.insert(0, str(_gen_dir))


@pytest.fixture
def s3_config() -> S3Config:
    return S3Config(
        endpoint="localhost:9000",
        access_key="test-access-key",
        secret_key="test-secret-key",
        bucket="test-bucket",
        use_ssl=False,
    )


@pytest.fixture
def nessie_config() -> NessieConfig:
    return NessieConfig(url="http://localhost:19120/api/v1")
