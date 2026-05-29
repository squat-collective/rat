"""Configuration + small helpers for the iceberg-nessie warehouse plugin.

These are relocated verbatim from the runner (S3Config / NessieConfig /
PartitionByEntry / _to_arrow_table) so the warehouse owns its storage config and
the runner no longer needs to know about Iceberg/Nessie (ADR-024). Keep
S3Config / NessieConfig field-aligned with the runner + query copies.
"""

from __future__ import annotations

import os
from dataclasses import dataclass

import pyarrow as pa


@dataclass(frozen=True)
class S3Config:
    """S3/MinIO connection configuration."""

    endpoint: str = "minio:9000"
    access_key: str = ""
    secret_key: str = ""
    bucket: str = "rat"
    use_ssl: bool = False
    session_token: str = ""
    region: str = "us-east-1"

    @classmethod
    def from_env(cls) -> S3Config:
        """Build S3Config from environment variables.

        Raises ValueError if S3_ACCESS_KEY or S3_SECRET_KEY are not set, since
        credentials must never be hardcoded as defaults.
        """
        access_key = os.environ.get("S3_ACCESS_KEY", "")
        secret_key = os.environ.get("S3_SECRET_KEY", "")
        if not access_key or not secret_key:
            raise ValueError(
                "S3_ACCESS_KEY and S3_SECRET_KEY environment variables are required. "
                "See infra/.env.example for reference."
            )
        return cls(
            endpoint=os.environ.get("S3_ENDPOINT", "minio:9000"),
            access_key=access_key,
            secret_key=secret_key,
            bucket=os.environ.get("S3_BUCKET", "rat"),
            use_ssl=os.environ.get("S3_USE_SSL", "false").lower() == "true",
            session_token=os.environ.get("S3_SESSION_TOKEN", ""),
            region=os.environ.get("S3_REGION", "us-east-1"),
        )

    @property
    def endpoint_url(self) -> str:
        scheme = "https" if self.use_ssl else "http"
        return f"{scheme}://{self.endpoint}"

    def with_overrides(self, overrides: dict[str, str]) -> S3Config:
        """Create a new S3Config with overridden fields from a proto map."""
        if not overrides:
            return self
        return S3Config(
            endpoint=overrides.get("endpoint", self.endpoint),
            access_key=overrides.get("access_key", self.access_key),
            secret_key=overrides.get("secret_key", self.secret_key),
            bucket=overrides.get("bucket", self.bucket),
            use_ssl=overrides.get("use_ssl", str(self.use_ssl)).lower() == "true",
            session_token=overrides.get("session_token", self.session_token),
            region=overrides.get("region", self.region),
        )


@dataclass(frozen=True)
class NessieConfig:
    """Nessie catalog connection configuration."""

    url: str = "http://nessie:19120/api/v1"

    @classmethod
    def from_env(cls) -> NessieConfig:
        return cls(url=os.environ.get("NESSIE_URL", "http://nessie:19120/api/v1"))

    @property
    def _host_url(self) -> str:
        """Strip known API suffixes to get the bare Nessie host URL."""
        url = self.url.rstrip("/")
        for suffix in ("/api/v1", "/api/v2", "/iceberg"):
            if url.endswith(suffix):
                url = url[: -len(suffix)]
                break
        return url

    @property
    def base_url(self) -> str:
        """Nessie Iceberg REST catalog URI (e.g., http://nessie:19120/iceberg)."""
        return self._host_url + "/iceberg"

    @property
    def api_v2_url(self) -> str:
        """Nessie v2 REST API base URL for branch operations."""
        return self._host_url + "/api/v2"


@dataclass(frozen=True)
class PartitionByEntry:
    """A single partition field: column name + transform (e.g., day, month, identity)."""

    column: str
    transform: str = "identity"  # identity, day, month, year, hour


def _to_arrow_table(arrow_result: pa.Table | pa.RecordBatchReader) -> pa.Table:
    """Convert a DuckDB .arrow() result to a PyArrow Table.

    DuckDB 1.0+ may return a RecordBatchReader instead of a Table from .arrow().
    This helper normalises both cases to a pa.Table.
    """
    if isinstance(arrow_result, pa.RecordBatchReader):
        return arrow_result.read_all()
    return arrow_result
