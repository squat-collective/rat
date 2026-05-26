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
    metadata_location: str = ""  # full Iceberg metadata.json path, for iceberg_scan()


class NessieCatalog:
    """Discovers Iceberg tables from Nessie and registers them as DuckDB views.

    Uses Nessie v2 REST API (GET /api/v2/trees/main/entries) to enumerate every
    namespace and every Iceberg table, then creates DuckDB views via
    iceberg_scan() — snapshot-aware reads pointed at each table's current
    metadata.json.

    Multi-namespace by default. Earlier versions scanned a single namespace
    (defaulting to "default"); pipelines created under any other namespace
    (e.g. by the demo-loader plugin) were invisible to /api/v1/tables and to
    the portal Explorer. The catalog now lists every NAMESPACE entry from
    Nessie on each refresh and registers tables for all of them.

    Optimization: tracks the Nessie main-branch commit hash. If unchanged
    since the last refresh, the whole pass is skipped. When something has
    changed, only tables whose metadata_location moved are re-registered
    (CREATE OR REPLACE VIEW avoids in-flight query failures).
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
        # Per-namespace state — earlier this was a flat list/dict and a
        # multi-namespace iteration silently dropped each previous namespace's
        # views as "stale". Keying by namespace fixes that.
        self._tables_by_ns: dict[str, list[TableEntry]] = {}
        self._paths_by_ns: dict[str, dict[tuple[str, str], str]] = {}
        self._lock = threading.Lock()
        self._last_commit_hash: str = ""

    def _get_nessie_commit_hash(self) -> str:
        """Fetch the current commit hash of the Nessie main branch.

        Used to detect whether the catalog has changed since last refresh.
        Returns an empty string if the request fails (caller should proceed
        with a full refresh as a fallback).
        """
        try:
            url = f"{self._nessie_config.api_v2_url}/trees/main"
            req = Request(url, headers={"Accept": "application/json"})
            with urlopen(req, timeout=5) as resp:  # noqa: S310
                data = json.loads(resp.read().decode())
            return data.get("hash", "")
        except Exception:
            return ""

    def discover_namespaces(self) -> list[str]:
        """Enumerate top-level namespaces from Nessie.

        A top-level namespace is a NAMESPACE entry whose `name.elements` has
        length 1 (e.g. `cosmos`, `shop`). Nested namespaces like
        `cosmos.bronze` show up as length-2 entries and are ignored here —
        those are RAT's layer namespaces, not tenants.
        """
        url = f"{self._nessie_config.api_v2_url}/trees/main/entries"
        req = Request(url, headers={"Accept": "application/json"})
        with urlopen(req, timeout=10) as resp:  # noqa: S310
            data = json.loads(resp.read().decode())

        out: list[str] = []
        seen: set[str] = set()
        for entry in data.get("entries", []):
            if entry.get("type") != "NAMESPACE":
                continue
            elements = entry.get("name", {}).get("elements", [])
            if len(elements) != 1:
                continue
            ns = elements[0]
            if ns not in seen:
                seen.add(ns)
                out.append(ns)
        return out

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
                    metadata_location=meta_loc,
                )
            )

        return entries

    def register_tables(self, namespace: str) -> None:
        """Discover + register tables for a single namespace.

        Kept for back-compat (server init still uses it). Performs the
        Nessie-hash short-circuit before doing any work.
        """
        current_hash = self._get_nessie_commit_hash()
        if current_hash and current_hash == self._last_commit_hash:
            logger.debug(
                "Nessie commit hash unchanged (%s), skipping view re-registration",
                current_hash[:8],
            )
            return
        self._register_namespace(namespace)
        if current_hash:
            self._last_commit_hash = current_hash

    def register_all_tables(
        self, extra_namespaces: list[str] | None = None
    ) -> None:
        """Discover every namespace from Nessie + any extras, register each.

        One Nessie-hash short-circuit covers all namespaces — if nothing in
        the catalog changed since the last refresh, the whole pass is a no-op.
        """
        current_hash = self._get_nessie_commit_hash()
        if current_hash and current_hash == self._last_commit_hash:
            return

        namespaces = list(self.discover_namespaces())
        if extra_namespaces:
            for ns in extra_namespaces:
                if ns not in namespaces:
                    namespaces.append(ns)

        for ns in namespaces:
            try:
                self._register_namespace(ns)
            except Exception:
                logger.exception("Failed to register namespace '%s'", ns)

        if current_hash:
            self._last_commit_hash = current_hash

    def _register_namespace(self, namespace: str) -> None:
        """Per-namespace registration. Does not check the Nessie hash —
        callers either decided to refresh or are forcing a single namespace.
        """
        tables = self.discover_tables(namespace)

        old_paths = self._paths_by_ns.get(namespace, {})
        new_keys = {(t.layer, t.name) for t in tables}
        old_keys = set(old_paths.keys())

        new_paths: dict[tuple[str, str], str] = {}
        registered = 0
        skipped = 0
        for t in tables:
            key = (t.layer, t.name)
            if not t.metadata_location:
                logger.warning(
                    "Table %s.%s.%s has no metadataLocation — skipping registration",
                    t.namespace,
                    t.layer,
                    t.name,
                )
                continue
            new_paths[key] = t.metadata_location

            # Only re-register when the metadata.json changed (new snapshot)
            # or the table is new (not in the old path map).
            if old_paths.get(key) == t.metadata_location:
                skipped += 1
                continue

            self._engine.register_view(
                t.layer, t.name, t.metadata_location, namespace=namespace
            )
            registered += 1

        # Drop only stale views (removed from Nessie since last refresh).
        for layer, name in old_keys - new_keys:
            self._engine.drop_view(layer, name, namespace=namespace)

        with self._lock:
            self._tables_by_ns[namespace] = tables
        self._paths_by_ns[namespace] = new_paths

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
        self,
        stop_event: threading.Event,
        interval: float = 30.0,
        extra_namespaces: list[str] | None = None,
    ) -> None:
        """Background loop: re-discover namespaces + re-register tables every
        interval seconds. New namespaces (e.g. created by the demo-loader)
        are picked up automatically.
        """
        while not stop_event.wait(timeout=interval):
            try:
                self.register_all_tables(extra_namespaces=extra_namespaces)
            except Exception:
                logger.exception("Failed to refresh catalog")

    def get_tables(self, namespace: str = "", layer_filter: str = "") -> list[TableEntry]:
        """Return cached table list, optionally filtered by namespace + layer.

        With `namespace=""`, returns tables across every namespace the
        catalog has registered.
        """
        with self._lock:
            if namespace:
                tables = list(self._tables_by_ns.get(namespace, []))
            else:
                tables = [t for nsl in self._tables_by_ns.values() for t in nsl]
        if layer_filter:
            tables = [t for t in tables if t.layer == layer_filter]
        return tables
