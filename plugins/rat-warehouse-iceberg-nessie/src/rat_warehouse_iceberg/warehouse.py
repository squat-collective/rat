"""IcebergNessieWarehouse — the reference warehouse implementation (ADR-024).

Wraps the relocated Iceberg (`iceberg.py`) + Nessie (`nessie.py`) logic behind
the warehouse contract. The ConnectRPC server (`__main__.py`) adapts this object
onto warehouse/v1; the runner/ratq/consumers never import iceberg/nessie
directly anymore — they go through the service.

Capabilities advertised: branching, time_travel, row_diff (Nessie gives us
branches + snapshot history; row diff is computed via DuckDB iceberg_scan).

Strategy params (unique_key, partition_column, scd_valid_from/to) arrive in the
`options` map of write() — they used to ride on the runner's PipelineConfig
(ADR-024: the warehouse owns the write, so the strategy inputs travel with it).
"""

from __future__ import annotations

import logging
from typing import TYPE_CHECKING

from rat_warehouse_iceberg import iceberg, nessie
from rat_warehouse_iceberg.config import NessieConfig, PartitionByEntry, S3Config

if TYPE_CHECKING:
    import pyarrow as pa

logger = logging.getLogger(__name__)

# Capabilities this warehouse supports (ADR-024 capability set).
CAPABILITIES = frozenset({"branching", "time_travel", "row_diff"})


def _split_ref(namespace: str, layer: str, name: str) -> str:
    """Build the dotted Iceberg table identifier the iceberg.py functions expect."""
    return f"{namespace}.{layer}.{name}"


def _partition_by(options: dict[str, str]) -> tuple[PartitionByEntry, ...]:
    """Parse the optional `partition_by` option (``col:transform,col2:transform``)."""
    raw = options.get("partition_by", "").strip()
    if not raw:
        return ()
    entries: list[PartitionByEntry] = []
    for part in raw.split(","):
        col, _, transform = part.strip().partition(":")
        if col:
            entries.append(PartitionByEntry(column=col, transform=transform or "identity"))
    return tuple(entries)


def _unique_key(options: dict[str, str]) -> tuple[str, ...]:
    raw = options.get("unique_key", "").strip()
    return tuple(k.strip() for k in raw.split(",") if k.strip()) if raw else ()


class IcebergNessieWarehouse:
    """WarehouseProtocol + Branching/TimeTravel/RowDiff over Iceberg + Nessie.

    Construct with the S3/Nessie config the warehouse owns; the runner just sends
    a TableRef + Arrow data + strategy + options, and reads via the AttachDescriptor.
    """

    name = "iceberg-nessie"

    def __init__(self, s3_config: S3Config, nessie_config: NessieConfig) -> None:
        self._s3 = s3_config
        self._nessie = nessie_config

    @property
    def capabilities(self) -> frozenset[str]:
        return CAPABILITIES

    # ── Discovery ──
    def list_namespaces(self) -> list[str]:
        catalog = iceberg.get_catalog(self._s3, self._nessie, "main")
        return [".".join(ns) for ns in catalog.list_namespaces()]

    def list_tables(self, namespace: str) -> list[tuple[str, str, str]]:
        catalog = iceberg.get_catalog(self._s3, self._nessie, "main")
        out: list[tuple[str, str, str]] = []
        for ident in catalog.list_tables(namespace):
            # ident is (ns, layer, name) or (ns.layer, name) depending on nesting
            parts = list(ident)
            if len(parts) >= 3:
                out.append((parts[0], parts[1], parts[2]))
        return out

    def get_schema(
        self, namespace: str, layer: str, name: str, *, branch: str = "main"
    ) -> pa.Schema:
        catalog = iceberg.get_catalog(self._s3, self._nessie, branch)
        table = catalog.load_table(_split_ref(namespace, layer, name))
        return table.schema().as_arrow()

    # ── Read attach (opaque descriptor; runner turns it into a DuckDB ATTACH) ──
    def attach(self, runner_type: str, *, branch: str = "main") -> dict[str, object]:
        return {
            "catalog_uri": self._nessie.base_url,
            "format": "iceberg",
            "storage": {
                "endpoint": self._s3.endpoint,
                "access_key": self._s3.access_key,
                "secret_key": self._s3.secret_key,
                "region": self._s3.region,
                "use_ssl": str(self._s3.use_ssl).lower(),
                "branch": branch,
            },
            "options": {"runner_type": runner_type},
        }

    # ── Write (strategy dispatch; params come from `options`) ──
    def write(
        self,
        namespace: str,
        layer: str,
        name: str,
        data: pa.Table,
        strategy: str,
        options: dict[str, str] | None = None,
        *,
        branch: str = "main",
        location: str = "",
    ) -> int:
        opts = options or {}
        table_name = _split_ref(namespace, layer, name)
        loc = location or f"s3://{self._s3.bucket}/{namespace}/{layer}/{name}/"
        pby = _partition_by(opts)
        key = _unique_key(opts)

        if strategy == "incremental" and key:
            return iceberg.merge_iceberg(
                data, table_name, key, self._s3, self._nessie, loc, branch=branch, partition_by=pby
            )
        if strategy == "append_only":
            return iceberg.append_iceberg(
                data, table_name, self._s3, self._nessie, loc, branch=branch, partition_by=pby
            )
        if strategy == "delete_insert" and key:
            return iceberg.delete_insert_iceberg(
                data, table_name, key, self._s3, self._nessie, loc, branch=branch, partition_by=pby
            )
        if strategy == "scd2" and key:
            return iceberg.scd2_iceberg(
                data,
                table_name,
                key,
                self._s3,
                self._nessie,
                loc,
                valid_from_col=opts.get("scd_valid_from", "valid_from"),
                valid_to_col=opts.get("scd_valid_to", "valid_to"),
                branch=branch,
                partition_by=pby,
            )
        if strategy == "snapshot" and opts.get("partition_column"):
            return iceberg.snapshot_iceberg(
                data,
                table_name,
                opts["partition_column"],
                self._s3,
                self._nessie,
                loc,
                branch=branch,
                partition_by=pby,
            )
        # full_refresh and any strategy whose required option is absent.
        return iceberg.write_iceberg(
            data, table_name, self._s3, self._nessie, loc, branch=branch, partition_by=pby
        )

    # ── BRANCHING capability ──
    def create_branch(self, name: str, *, from_ref: str = "main") -> str:
        return nessie.create_branch(self._nessie, name, from_branch=from_ref)

    def merge_branch(self, from_branch: str, *, into_branch: str = "main") -> None:
        nessie.merge_branch(self._nessie, from_branch, target=into_branch)

    def delete_branch(self, name: str) -> bool:
        nessie.delete_branch(self._nessie, name)
        return True

    def list_branches(self) -> list[str]:
        # Nessie reference listing — relocated nessie.py exposes branch ops; a
        # full list_branches lands with the server wiring (slice continues).
        raise NotImplementedError("list_branches: wired in the server slice")

    # ── TIME_TRAVEL capability ──
    def get_history(self, namespace: str, layer: str, name: str, *, limit: int = 0):
        catalog = iceberg.get_catalog(self._s3, self._nessie, "main")
        table = catalog.load_table(_split_ref(namespace, layer, name))
        snaps = list(table.metadata.snapshots)
        if limit:
            snaps = snaps[-limit:]
        return snaps

    # ── ROW_DIFF capability ──
    def row_diff(
        self, namespace: str, layer: str, name: str, from_ref: str, to_ref: str, *, limit: int = 0
    ):
        # Row diff is computed via DuckDB iceberg_scan EXCEPT (today done in the
        # diff plugin); it moves here in the consumer-migration slice.
        raise NotImplementedError("row_diff: wired in the consumer-migration slice")
