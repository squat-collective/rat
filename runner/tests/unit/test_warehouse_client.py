"""Runner-side WarehouseClient tests (ADR-024).

Verifies the client maps to warehouse/v1 against a fake stub — no live warehouse,
no pyiceberg. Pins the Arrow-streamed Write framing + branch/strategy mapping the
executor relies on.
"""

from __future__ import annotations

import pyarrow as pa
import pytest
from warehouse.v1 import warehouse_pb2

from rat_runner.warehouse_client import WarehouseClient


class _FakeStub:
    def __init__(self, *, merge_ok: bool = True) -> None:
        self.calls: dict = {}
        self._merge_ok = merge_ok

    def CreateBranch(self, req):  # noqa: N802
        self.calls["create"] = (req.name, req.from_ref)
        return warehouse_pb2.CreateBranchResponse(name=req.name)

    def MergeBranch(self, req):  # noqa: N802
        self.calls["merge"] = (req.from_branch, req.into_branch)
        return warehouse_pb2.MergeBranchResponse(
            merged=self._merge_ok, conflict="" if self._merge_ok else "branch moved"
        )

    def DeleteBranch(self, req):  # noqa: N802
        self.calls["delete"] = req.name
        return warehouse_pb2.DeleteBranchResponse(deleted=True)

    def Write(self, request_iterator):  # noqa: N802
        msgs = list(request_iterator)
        header = msgs[0].header
        batches: list = []
        for m in msgs[1:]:
            batches.extend(pa.ipc.open_stream(m.arrow_batch))
        table = pa.Table.from_batches(batches)
        self.calls["write"] = {
            "ns": header.ref.namespace,
            "layer": header.ref.layer,
            "name": header.ref.name,
            "strategy": header.strategy,
            "branch": header.branch,
            "options": dict(header.options),
            "rows": table.num_rows,
        }
        return warehouse_pb2.WriteResponse(rows_written=table.num_rows)

    def Attach(self, req):  # noqa: N802
        return warehouse_pb2.AttachResponse(
            descriptor=warehouse_pb2.AttachDescriptor(catalog_uri="memory://", format="iceberg")
        )

    def GetSchema(self, req):  # noqa: N802
        return warehouse_pb2.GetSchemaResponse(
            arrow_schema=pa.schema([("id", pa.int64())]).serialize().to_pybytes()
        )


def test_create_branch_passes_from_ref():
    stub = _FakeStub()
    c = WarehouseClient(stub)
    assert c.create_branch("run-abc", from_ref="main") == "run-abc"
    assert stub.calls["create"] == ("run-abc", "main")


def test_merge_branch_raises_on_conflict():
    c = WarehouseClient(_FakeStub(merge_ok=False))
    with pytest.raises(RuntimeError, match="branch moved"):
        c.merge_branch("run-abc", into_branch="main")


def test_merge_branch_ok():
    c = WarehouseClient(_FakeStub())
    c.merge_branch("run-abc")  # no raise


def test_write_streams_arrow_and_maps_options():
    stub = _FakeStub()
    c = WarehouseClient(stub)
    table = pa.table({"id": [1, 2, 3]})
    rows = c.write(
        "shop", "bronze", "orders", table, "scd2", {"unique_key": "id"}, branch="run-abc"
    )
    assert rows == 3
    assert stub.calls["write"] == {
        "ns": "shop",
        "layer": 1,  # LAYER_BRONZE
        "name": "orders",
        "strategy": "scd2",
        "branch": "run-abc",
        "options": {"unique_key": "id"},
        "rows": 3,
    }


def test_attach_and_get_schema():
    c = WarehouseClient(_FakeStub())
    desc = c.attach("sql", branch="main")
    assert desc.format == "iceberg"
    schema = c.get_schema("shop", "bronze", "orders")
    assert schema.names == ["id"]
