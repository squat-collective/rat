"""gRPC server — WarehouseService implementation (ADR-024).

The servicer is warehouse-agnostic: it maps warehouse/v1 proto ↔ a duck-typed
warehouse object (the WarehouseProtocol author SDK) and frames table data as
Arrow IPC. __main__ injects the concrete IcebergNessieWarehouse. Capability-gated
RPCs that the warehouse doesn't implement raise NotImplementedError, which we
translate to gRPC UNIMPLEMENTED — matching the contract's "callers gate on the
advertised capability set" rule.
"""

from __future__ import annotations

import logging
from typing import TYPE_CHECKING, Protocol

import grpc
import pyarrow as pa

# Proto stubs (gen/ on sys.path — see __main__.py).
from warehouse.v1 import (  # type: ignore[import-untyped]
    warehouse_pb2,
    warehouse_pb2_grpc,
)

if TYPE_CHECKING:
    from collections.abc import Iterator

logger = logging.getLogger(__name__)

# common.v1.Layer enum ↔ the warehouse's string layer.
_LAYER_TO_STR = {1: "bronze", 2: "silver", 3: "gold"}
_STR_TO_LAYER = {v: k for k, v in _LAYER_TO_STR.items()}

# Capability string (author SDK) ↔ proto Capability enum.
_CAP_TO_ENUM = {
    "branching": warehouse_pb2.CAPABILITY_BRANCHING,
    "time_travel": warehouse_pb2.CAPABILITY_TIME_TRAVEL,
    "row_diff": warehouse_pb2.CAPABILITY_ROW_DIFF,
    "scd2_native": warehouse_pb2.CAPABILITY_SCD2_NATIVE,
    "partition_evolution": warehouse_pb2.CAPABILITY_PARTITION_EVOLUTION,
}


def _layer_str(layer: int) -> str:
    return _LAYER_TO_STR.get(layer, "bronze")


class _Warehouse(Protocol):
    """Structural type the servicer needs — the WarehouseProtocol author SDK."""

    name: str

    @property
    def capabilities(self) -> frozenset[str]: ...
    def list_namespaces(self) -> list[str]: ...
    def list_tables(self, namespace: str) -> list[tuple[str, str, str]]: ...
    def get_schema(self, namespace: str, layer: str, name: str, *, branch: str = "main") -> pa.Schema: ...
    def attach(self, runner_type: str, *, branch: str = "main") -> dict[str, object]: ...
    def write(self, namespace: str, layer: str, name: str, data: pa.Table, strategy: str, options: dict[str, str] | None = ..., *, branch: str = "main", location: str = "") -> int: ...


class WarehouseServicer(warehouse_pb2_grpc.WarehouseServiceServicer):
    def __init__(self, wh: _Warehouse) -> None:
        self._wh = wh

    # ── Required surface ──
    def Describe(self, request, context):  # noqa: N802
        return warehouse_pb2.DescribeResponse(
            name=self._wh.name,
            version="0.2.0b1",
            capabilities=[_CAP_TO_ENUM[c] for c in self._wh.capabilities if c in _CAP_TO_ENUM],
        )

    def ListNamespaces(self, request, context):  # noqa: N802
        return warehouse_pb2.ListNamespacesResponse(namespaces=self._wh.list_namespaces())

    def ListTables(self, request, context):  # noqa: N802
        refs = [
            warehouse_pb2.TableRef(namespace=ns, layer=_STR_TO_LAYER.get(layer, 0), name=nm)
            for ns, layer, nm in self._wh.list_tables(request.namespace)
        ]
        return warehouse_pb2.ListTablesResponse(tables=refs)

    def GetSchema(self, request, context):  # noqa: N802
        ref = request.ref
        schema = self._wh.get_schema(
            ref.namespace, _layer_str(ref.layer), ref.name, branch=request.branch or "main"
        )
        return warehouse_pb2.GetSchemaResponse(arrow_schema=schema.serialize().to_pybytes())

    def Attach(self, request, context):  # noqa: N802
        d = self._wh.attach(request.runner_type, branch=request.branch or "main")
        storage = {k: str(v) for k, v in dict(d.get("storage", {})).items()}
        options = {k: str(v) for k, v in dict(d.get("options", {})).items()}
        # The structured S3Credentials field is wired in the runner-switch slice;
        # for now storage creds ride in options under a "storage." prefix.
        merged = {**options, **{f"storage.{k}": v for k, v in storage.items()}}
        return warehouse_pb2.AttachResponse(
            descriptor=warehouse_pb2.AttachDescriptor(
                catalog_uri=str(d.get("catalog_uri", "")),
                format=str(d.get("format", "")),
                options=merged,
            )
        )

    def Write(self, request_iterator: Iterator, context):  # noqa: N802
        header = None
        batches: list[pa.RecordBatch] = []
        for msg in request_iterator:
            which = msg.WhichOneof("payload")
            if which == "header":
                header = msg.header
            elif which == "arrow_batch":
                reader = pa.ipc.open_stream(msg.arrow_batch)
                batches.extend(reader)
        if header is None:
            context.abort(grpc.StatusCode.INVALID_ARGUMENT, "Write: first message must be the header")
        ref = header.ref
        table = pa.Table.from_batches(batches) if batches else pa.table({})
        rows = self._wh.write(
            ref.namespace,
            _layer_str(ref.layer),
            ref.name,
            table,
            header.strategy,
            dict(header.options),
            branch=header.branch or "main",
        )
        return warehouse_pb2.WriteResponse(rows_written=rows)

    # ── Capability-gated ──
    def CreateBranch(self, request, context):  # noqa: N802
        return self._gated(
            context,
            lambda: warehouse_pb2.CreateBranchResponse(
                name=self._wh.create_branch(request.name, from_ref=request.from_ref or "main")
            ),
        )

    def MergeBranch(self, request, context):  # noqa: N802
        def _do():
            self._wh.merge_branch(request.from_branch, into_branch=request.into_branch or "main")
            return warehouse_pb2.MergeBranchResponse(merged=True)

        return self._gated(context, _do)

    def DeleteBranch(self, request, context):  # noqa: N802
        return self._gated(
            context,
            lambda: warehouse_pb2.DeleteBranchResponse(deleted=self._wh.delete_branch(request.name)),
        )

    def ListBranches(self, request, context):  # noqa: N802
        return self._gated(
            context, lambda: warehouse_pb2.ListBranchesResponse(branches=self._wh.list_branches())
        )

    def GetHistory(self, request, context):  # noqa: N802
        def _do():
            ref = request.ref
            snaps = self._wh.get_history(
                ref.namespace, _layer_str(ref.layer), ref.name, limit=request.limit
            )
            return warehouse_pb2.GetHistoryResponse(
                snapshots=[
                    warehouse_pb2.Snapshot(
                        id=str(s["id"]), message=str(s.get("message", "")), rows=int(s.get("rows", 0))
                    )
                    for s in snaps
                ]
            )

        return self._gated(context, _do)

    def RowDiff(self, request, context):  # noqa: N802
        ref = request.ref

        def _do():
            diff = self._wh.row_diff(
                ref.namespace, _layer_str(ref.layer), ref.name, request.from_ref, request.to_ref, limit=request.limit
            )
            return warehouse_pb2.RowDiffResponse(
                added=diff.added.serialize().to_pybytes() if diff.added else b"",
                removed=diff.removed.serialize().to_pybytes() if diff.removed else b"",
            )

        return self._gated(context, _do)

    @staticmethod
    def _gated(context, fn):
        """Run a capability-gated op; translate NotImplementedError → UNIMPLEMENTED."""
        try:
            return fn()
        except NotImplementedError as e:
            context.abort(grpc.StatusCode.UNIMPLEMENTED, str(e) or "capability not supported")
