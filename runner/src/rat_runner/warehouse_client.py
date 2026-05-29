"""Runner-side client for the warehouse plugin (ADR-024).

The executor calls this instead of importing iceberg.py / nessie.py directly:
branch lifecycle, Arrow-streamed writes (strategy + options), read-attach, and
schema/history. The warehouse owns the storage substrate; the runner only sends
a TableRef + Arrow + a universal strategy name.
"""

from __future__ import annotations

import logging
from typing import TYPE_CHECKING

import grpc
import pyarrow as pa

# gen/ is on sys.path (see __main__.py).
from warehouse.v1 import (  # type: ignore[import-untyped]
    warehouse_pb2,
    warehouse_pb2_grpc,
)

if TYPE_CHECKING:
    from collections.abc import Iterator

logger = logging.getLogger(__name__)

_LAYER_TO_ENUM = {"bronze": 1, "silver": 2, "gold": 3}


def _table_ref(namespace: str, layer: str, name: str) -> warehouse_pb2.TableRef:
    return warehouse_pb2.TableRef(
        namespace=namespace, layer=_LAYER_TO_ENUM.get(layer, 0), name=name
    )


def _table_to_ipc(table: pa.Table) -> bytes:
    sink = pa.BufferOutputStream()
    with pa.ipc.new_stream(sink, table.schema) as writer:
        writer.write_table(table)
    return sink.getvalue().to_pybytes()


class WarehouseClient:
    """gRPC client for warehouse/v1. Inject a stub for testing, or use from_addr()."""

    def __init__(self, stub: warehouse_pb2_grpc.WarehouseServiceStub) -> None:
        self._stub = stub

    @classmethod
    def from_addr(cls, addr: str) -> WarehouseClient:
        channel = grpc.insecure_channel(addr)
        return cls(warehouse_pb2_grpc.WarehouseServiceStub(channel))

    # ── Branch lifecycle (BRANCHING) ──
    def create_branch(self, name: str, from_ref: str = "main") -> str:
        resp = self._stub.CreateBranch(
            warehouse_pb2.CreateBranchRequest(name=name, from_ref=from_ref)
        )
        return resp.name

    def merge_branch(self, from_branch: str, into_branch: str = "main") -> None:
        resp = self._stub.MergeBranch(
            warehouse_pb2.MergeBranchRequest(from_branch=from_branch, into_branch=into_branch)
        )
        if not resp.merged:
            raise RuntimeError(f"warehouse merge failed: {resp.conflict or 'unknown conflict'}")

    def delete_branch(self, name: str) -> bool:
        return self._stub.DeleteBranch(warehouse_pb2.DeleteBranchRequest(name=name)).deleted

    # ── Write (Arrow-streamed; strategy + options) ──
    def write(
        self,
        namespace: str,
        layer: str,
        name: str,
        data: pa.Table,
        strategy: str,
        options: dict[str, str] | None = None,
        branch: str = "main",
    ) -> int:
        ref = _table_ref(namespace, layer, name)

        def _stream() -> Iterator[warehouse_pb2.WriteRequest]:
            yield warehouse_pb2.WriteRequest(
                header=warehouse_pb2.WriteHeader(
                    ref=ref, strategy=strategy, branch=branch, options=options or {}
                )
            )
            yield warehouse_pb2.WriteRequest(arrow_batch=_table_to_ipc(data))

        return self._stub.Write(_stream()).rows_written

    # ── Read-attach (opaque descriptor → runner turns it into a DuckDB ATTACH) ──
    def attach(self, runner_type: str, branch: str = "main") -> warehouse_pb2.AttachDescriptor:
        return self._stub.Attach(
            warehouse_pb2.AttachRequest(runner_type=runner_type, branch=branch)
        ).descriptor

    def get_schema(self, namespace: str, layer: str, name: str, branch: str = "main") -> pa.Schema:
        resp = self._stub.GetSchema(
            warehouse_pb2.GetSchemaRequest(ref=_table_ref(namespace, layer, name), branch=branch)
        )
        return pa.ipc.read_schema(pa.py_buffer(resp.arrow_schema))
