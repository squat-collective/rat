"""Nessie-based catalog discovery + DuckDB view registration."""

from __future__ import annotations

import json
import logging
import threading
from dataclasses import dataclass
from typing import TYPE_CHECKING
from urllib.request import Request, urlopen

if TYPE_CHECKING:
    from rat_query.config import NessieConfig, S3Config
    from rat_query.engine import QueryEngine

logger = logging.getLogger(__name__)


@dataclass(frozen=True)
class TableEntry:
    """Lightweight table reference for listing."""

    namespace: str
    layer: str
    name: str
    s3_base_path: str = ""  # table root in S3 (derived from metadataLocation)


class NessieCatalog:
    """Discovers Iceberg tables from Nessie and registers them as DuckDB views.

    Uses Nessie v2 REST API (GET /api/v2/trees/main/entries) to list all
    Iceberg tables, then creates DuckDB views via read_parquet() glob patterns.

    Optimization: tracks the Nessie commit hash from the last refresh and the
    per-table s3_base_path. If the commit hash hasn't changed, skips
    re-registration entirely. If only some tables changed, only re-registers
    those whose s3_base_path differs (indicating a new Iceberg snapshot).
    """

    def __init__(
        self,
        nessie_config: NessieConfig,
        s3_config: S3Config,
        engine: QueryEngine,
    ) -> None:
        self._nessie_config = nessie_config
        self._s3_config = s3_config
        self._engine = engine
        self._tables: list[TableEntry] = []
        self._lock = threading.Lock()
        # Track Nessie commit hash + per-table paths to skip redundant re-registration.
        self._last_commit_hash: str = ""
        self._table_paths: dict[tuple[str, str], str] = {}  # (layer, name) -> s3_base_path

    def _get_nessie_commit_hash(self) -> str:
        """Fetch the current commit hash of the Nessie main branch.

        Used to detect whether the catalog has changed since last refresh.
        Returns an empty string if the request fails (caller should proceed
        with full refresh as a fallback).
        """
        try:
            url = f"{self._nessie_config.api_v2_url}/trees/main"
            req = Request(url, headers={"Accept": "application/json"})
            with urlopen(req, timeout=5) as resp:  # noqa: S310
                data = json.loads(resp.read().decode())
            return data.get("hash", "")
        except Exception:
            return ""

    def discover_tables(self, namespace: str) -> list[TableEntry]:
        """Call Nessie v2 REST API to list all Iceberg tables in a namespace."""
        url = f"{self._nessie_config.api_v2_url}/trees/main/entries?content=true"
        req = Request(url, headers={"Accept": "application/json"})
        with urlopen(req, timeout=10) as resp:  # noqa: S310
            data = json.loads(resp.read().decode())

        entries: list[TableEntry] = []
        for entry in data.get("entries", []):
            if entry.get("type") != "ICEBERG_TABLE":
                continue
            # Nessie entry key: elements are namespace parts
            # Convention: [namespace, layer, table_name]
            elements = entry.get("name", {}).get("elements", [])
            if len(elements) < 3:
                continue
            ns = elements[0]
            layer = elements[1]
            table_name = elements[2]
            if ns != namespace:
                continue
            if layer not in ("bronze", "silver", "gold"):
                continue

            # Derive S3 base path from metadataLocation (strip /metadata/*.json)
            s3_base = ""
            content = entry.get("content", {})
            meta_loc = content.get("metadataLocation", "")
            if "/metadata/" in meta_loc:
                s3_base = meta_loc[: meta_loc.index("/metadata/")]

            entries.append(
                TableEntry(
                    namespace=ns,
                    layer=layer,
                    name=table_name,
                    s3_base_path=s3_base,
                )
            )

        return entries

    def register_tables(self, namespace: str) -> None:
        """Discover tables from Nessie and register as DuckDB views.

        Optimization: compares the Nessie main branch commit hash against the
        last known hash. If unchanged, skips the entire re-registration. If
        changed, only re-registers tables whose s3_base_path differs (indicating
        a new Iceberg metadata snapshot). This avoids redundant CREATE OR REPLACE
        VIEW calls that trigger DuckDB catalog locks.

        Uses CREATE OR REPLACE VIEW (in register_view) to avoid dropping
        schemas — this prevents in-flight queries from failing during refresh.
        Stale views (removed from Nessie) are dropped individually.
        """
        # Check commit hash first — skip entirely if Nessie state is unchanged.
        current_hash = self._get_nessie_commit_hash()
        if current_hash and current_hash == self._last_commit_hash:
            logger.debug(
                "Nessie commit hash unchanged (%s), skipping view re-registration", current_hash[:8]
            )
            return

        tables = self.discover_tables(namespace)
        bucket = self._s3_config.bucket

        # Build set of current (layer, name) for stale detection.
        new_keys = {(t.layer, t.name) for t in tables}
        with self._lock:
            old_keys = {(t.layer, t.name) for t in self._tables}

        # Build new path map to detect which tables actually changed.
        new_paths: dict[tuple[str, str], str] = {}
        registered = 0
        skipped = 0
        for t in tables:
            s3_path = t.s3_base_path or f"s3://{bucket}/{t.namespace}/{t.layer}/{t.name}"
            key = (t.layer, t.name)
            new_paths[key] = s3_path

            # Only re-register if the s3_base_path changed (new snapshot) or
            # the table is new (not in the old path map).
            if self._table_paths.get(key) == s3_path:
                skipped += 1
                continue

            self._engine.register_view(t.layer, t.name, s3_path, namespace=namespace)
            registered += 1

        # Drop only stale views (removed from Nessie since last refresh).
        for layer, name in old_keys - new_keys:
            self._engine.drop_view(layer, name, namespace=namespace)

        with self._lock:
            self._tables = tables
        self._table_paths = new_paths
        if current_hash:
            self._last_commit_hash = current_hash

        if skipped > 0:
            logger.info(
                "Registered %d tables for namespace '%s' (%d unchanged, skipped)",
                registered,
                namespace,
                skipped,
            )
        else:
            logger.info("Registered %d tables for namespace '%s'", registered, namespace)

    def refresh_loop(
        self, namespace: str, stop_event: threading.Event, interval: float = 30.0
    ) -> None:
        """Background loop: re-discover + re-register tables every interval seconds."""
        while not stop_event.wait(timeout=interval):
            try:
                self.register_tables(namespace)
            except Exception:
                logger.exception("Failed to refresh catalog")

    def get_tables(self, namespace: str, layer_filter: str = "") -> list[TableEntry]:
        """Return cached table list, optionally filtered by layer."""
        with self._lock:
            tables = list(self._tables)
        if layer_filter:
            tables = [t for t in tables if t.layer == layer_filter]
        return [t for t in tables if t.namespace == namespace]
