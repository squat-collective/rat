"""Configuration management — S3 and Nessie connection config.

NOTE: S3Config, DuckDBConfig, and NessieConfig are intentionally aligned with
the runner's versions in runner/src/rat_runner/config.py. When modifying these
classes, keep both files in sync. A shared package was considered but deferred
to avoid pyproject.toml and Docker packaging complexity. See task P6-01.
"""

from __future__ import annotations

import os
from dataclasses import dataclass


@dataclass(frozen=True)
class S3Config:
    """S3/MinIO connection configuration.

    Aligned with runner/src/rat_runner/config.py — keep fields, defaults,
    and env var names identical across both services.
    """

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

        Raises ValueError if S3_ACCESS_KEY or S3_SECRET_KEY are not set,
        since credentials must never be hardcoded as defaults.
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


@dataclass(frozen=True)
class DuckDBConfig:
    """DuckDB resource limits.

    Aligned with runner/src/rat_runner/config.py — keep fields, defaults,
    and env var names identical across both services.
    """

    memory_limit: str = "2GB"
    threads: int = 4

    @classmethod
    def from_env(cls) -> DuckDBConfig:
        raw_threads = os.environ.get("DUCKDB_THREADS", "4")
        try:
            threads = int(raw_threads)
        except ValueError:
            raise ValueError(
                f"DUCKDB_THREADS must be a valid integer, got {raw_threads!r}"
            ) from None
        if threads < 1:
            raise ValueError(f"DUCKDB_THREADS must be a positive integer, got {threads}")
        return cls(
            memory_limit=os.environ.get("DUCKDB_MEMORY_LIMIT", "2GB"),
            threads=threads,
        )


@dataclass(frozen=True)
class NessieConfig:
    """Nessie catalog connection configuration.

    Aligned with runner/src/rat_runner/config.py — keep property semantics
    identical across both services. The runner is the canonical version.
    """

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
        """Nessie v2 REST API base URL for table discovery."""
        return self._host_url + "/api/v2"
