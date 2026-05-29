"""Contract tests for the rat.warehouses seam (ADR-024).

Slice 1 defines only the author-SDK Protocols + the warehouse/v1 wire contract;
there is no reference implementation or consumer migration yet. These tests pin
the shape of the contract — the required surface and the opt-in capability
Protocols — so later slices can't silently drift it.
"""

from __future__ import annotations

import pyarrow as pa

from rat_runner.plugin_protocols import (
    AttachDescriptor,
    BranchingWarehouse,
    RowDiff,
    RowDiffWarehouse,
    Snapshot,
    TableRef,
    TimeTravelWarehouse,
    WarehouseProtocol,
)
from rat_runner.plugin_registry import GROUP_WAREHOUSES


class _BaseWarehouse:
    """Implements only the required WarehouseProtocol surface."""

    @property
    def name(self) -> str:
        return "fake"

    @property
    def capabilities(self) -> frozenset[str]:
        return frozenset()

    def list_namespaces(self) -> list[str]:
        return ["default"]

    def list_tables(self, namespace: str) -> list[TableRef]:
        return [TableRef(namespace, "bronze", "orders")]

    def get_schema(self, ref: TableRef, *, branch: str = "main") -> pa.Schema:
        return pa.schema([("id", pa.int64())])

    def attach(self, runner_type: str, *, branch: str = "main") -> AttachDescriptor:
        return AttachDescriptor(catalog_uri="memory://", format="iceberg")

    def write(self, ref, data, strategy, options=None, *, branch: str = "main") -> int:
        return 0


class _BranchingWarehouse(_BaseWarehouse):
    """Required surface + the branching capability."""

    @property
    def capabilities(self) -> frozenset[str]:
        return frozenset({"branching"})

    def create_branch(self, name: str, *, from_ref: str = "main") -> str:
        return name

    def merge_branch(self, from_branch: str, *, into_branch: str = "main") -> None:
        return None

    def delete_branch(self, name: str) -> bool:
        return True

    def list_branches(self) -> list[str]:
        return ["main"]


class _Partial:
    """Missing write() + attach() — must NOT satisfy the base Protocol."""

    @property
    def name(self) -> str:
        return "partial"

    def list_namespaces(self) -> list[str]:
        return []


def test_base_warehouse_satisfies_required_protocol():
    assert isinstance(_BaseWarehouse(), WarehouseProtocol)


def test_partial_object_does_not_satisfy_protocol():
    assert not isinstance(_Partial(), WarehouseProtocol)


def test_base_warehouse_does_not_claim_optional_capabilities():
    wh = _BaseWarehouse()
    assert not isinstance(wh, BranchingWarehouse)
    assert not isinstance(wh, TimeTravelWarehouse)
    assert not isinstance(wh, RowDiffWarehouse)


def test_branching_warehouse_satisfies_both_required_and_optional():
    wh = _BranchingWarehouse()
    assert isinstance(wh, WarehouseProtocol)
    assert isinstance(wh, BranchingWarehouse)
    # And capability advertisement is consistent with the implemented Protocol.
    assert "branching" in wh.capabilities


def test_attach_returns_opaque_descriptor():
    desc = _BaseWarehouse().attach("sql")
    assert isinstance(desc, AttachDescriptor)
    assert desc.catalog_uri == "memory://"
    assert desc.format == "iceberg"
    assert desc.storage == {} and desc.options == {}


def test_table_ref_is_addressable():
    ref = TableRef(namespace="shop", layer="bronze", name="orders")
    assert (ref.namespace, ref.layer, ref.name) == ("shop", "bronze", "orders")


def test_capability_value_dataclasses_exist():
    # Snapshot / RowDiff are the value types the optional ops return.
    snap = Snapshot(id="abc", committed_at="2026-05-29T00:00:00Z", message="init", rows=1)
    assert snap.id == "abc"
    empty = pa.table({"id": pa.array([], type=pa.int64())})
    diff = RowDiff(added=empty, removed=empty)
    assert diff.added.num_rows == 0


def test_warehouses_entry_point_group_name():
    assert GROUP_WAREHOUSES == "rat.warehouses"
