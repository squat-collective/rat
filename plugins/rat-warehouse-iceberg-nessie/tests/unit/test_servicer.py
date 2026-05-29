"""WarehouseService servicer tests (ADR-024).

Exercises the proto <-> warehouse mapping against a fake warehouse — no live
Nessie/Iceberg, so this is the first fully-verifiable checkpoint of the plugin.
"""

from __future__ import annotations

import pyarrow as pa
import pytest
from warehouse.v1 import warehouse_pb2

from rat_warehouse_iceberg.server import WarehouseServicer


class _FakeWarehouse:
    name = "iceberg-nessie"

    def __init__(self) -> None:
        self.written: dict | None = None
        self.created: tuple | None = None

    @property
    def capabilities(self) -> frozenset[str]:
        return frozenset({"branching", "time_travel", "row_diff"})

    def list_namespaces(self) -> list[str]:
        return ["shop", "cosmos"]

    def list_tables(self, namespace: str) -> list[tuple[str, str, str]]:
        return [(namespace, "bronze", "orders"), (namespace, "gold", "revenue")]

    def get_schema(self, namespace, layer, name, *, branch="main") -> pa.Schema:
        return pa.schema([("id", pa.int64()), ("amount", pa.float64())])

    def attach(self, runner_type, *, branch="main") -> dict:
        return {
            "catalog_uri": "http://nessie:19120/iceberg",
            "format": "iceberg",
            "storage": {"endpoint": "minio:9000", "branch": branch},
            "options": {"runner_type": runner_type},
        }

    def write(
        self, namespace, layer, name, data, strategy, options=None, *, branch="main", location=""
    ):
        self.written = {
            "ref": (namespace, layer, name),
            "rows": data.num_rows,
            "strategy": strategy,
            "options": dict(options or {}),
            "branch": branch,
        }
        return data.num_rows

    def create_branch(self, name, *, from_ref="main") -> str:
        self.created = (name, from_ref)
        return name

    def merge_branch(self, from_branch, *, into_branch="main") -> None:
        return None

    def delete_branch(self, name) -> bool:
        return True

    def list_branches(self) -> list[str]:
        raise NotImplementedError("list_branches not wired yet")


class _FakeContext:
    """Minimal grpc context: abort() raises so we can assert UNIMPLEMENTED gating."""

    class _AbortedError(Exception):
        pass

    def __init__(self) -> None:
        self.code = None

    def abort(self, code, details):
        self.code = code
        raise _FakeContext._AbortedError(details)


@pytest.fixture
def svc():
    return WarehouseServicer(_FakeWarehouse())


def test_describe_maps_name_and_capabilities(svc):
    resp = svc.Describe(warehouse_pb2.DescribeRequest(), None)
    assert resp.name == "iceberg-nessie"
    assert set(resp.capabilities) == {
        warehouse_pb2.CAPABILITY_BRANCHING,
        warehouse_pb2.CAPABILITY_TIME_TRAVEL,
        warehouse_pb2.CAPABILITY_ROW_DIFF,
    }


def test_list_namespaces(svc):
    resp = svc.ListNamespaces(warehouse_pb2.ListNamespacesRequest(), None)
    assert list(resp.namespaces) == ["shop", "cosmos"]


def test_list_tables_maps_layer_enum(svc):
    resp = svc.ListTables(warehouse_pb2.ListTablesRequest(namespace="shop"), None)
    assert resp.tables[0].namespace == "shop"
    assert resp.tables[0].layer == 1  # LAYER_BRONZE
    assert resp.tables[1].layer == 3  # LAYER_GOLD


def test_get_schema_roundtrips_arrow(svc):
    req = warehouse_pb2.GetSchemaRequest(
        ref=warehouse_pb2.TableRef(namespace="shop", layer=1, name="orders")
    )
    resp = svc.GetSchema(req, None)
    schema = pa.ipc.read_schema(pa.py_buffer(resp.arrow_schema))
    assert schema.names == ["id", "amount"]


def test_attach_maps_descriptor(svc):
    resp = svc.Attach(warehouse_pb2.AttachRequest(runner_type="sql"), None)
    assert resp.descriptor.catalog_uri == "http://nessie:19120/iceberg"
    assert resp.descriptor.format == "iceberg"
    assert resp.descriptor.options["runner_type"] == "sql"
    assert resp.descriptor.options["storage.endpoint"] == "minio:9000"


def test_write_reassembles_arrow_and_passes_strategy_options():
    wh = _FakeWarehouse()
    svc = WarehouseServicer(wh)
    table = pa.table({"id": [1, 2, 3], "amount": [1.0, 2.0, 3.0]})
    sink = pa.BufferOutputStream()
    with pa.ipc.new_stream(sink, table.schema) as w:
        w.write_table(table)
    batch_bytes = sink.getvalue().to_pybytes()

    stream = [
        warehouse_pb2.WriteRequest(
            header=warehouse_pb2.WriteHeader(
                ref=warehouse_pb2.TableRef(namespace="shop", layer=1, name="orders"),
                strategy="scd2",
                branch="run-abc",
                options={"unique_key": "id", "scd_valid_from": "vf"},
            )
        ),
        warehouse_pb2.WriteRequest(arrow_batch=batch_bytes),
    ]
    resp = svc.Write(iter(stream), None)
    assert resp.rows_written == 3
    assert wh.written == {
        "ref": ("shop", "bronze", "orders"),
        "rows": 3,
        "strategy": "scd2",
        "options": {"unique_key": "id", "scd_valid_from": "vf"},
        "branch": "run-abc",
    }


def test_create_branch(svc):
    resp = svc.CreateBranch(
        warehouse_pb2.CreateBranchRequest(name="run-xyz", from_ref="main"), None
    )
    assert resp.name == "run-xyz"


def test_unsupported_capability_aborts_unimplemented():
    import grpc

    svc = WarehouseServicer(_FakeWarehouse())
    ctx = _FakeContext()
    with pytest.raises(_FakeContext._AbortedError):
        svc.ListBranches(warehouse_pb2.ListBranchesRequest(), ctx)
    assert ctx.code == grpc.StatusCode.UNIMPLEMENTED
