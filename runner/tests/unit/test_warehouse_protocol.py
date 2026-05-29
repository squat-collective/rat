"""Contract tests for the rat.warehouses seam (ADR-024).

Slice 1 defines only the Protocol + entry-point group name; there is no
reference implementation or consumer migration yet. These tests pin the shape
of the contract so later slices can't silently drift it.
"""

from __future__ import annotations

import pyarrow as pa

from rat_runner.plugin_protocols import TableRef, WarehouseProtocol
from rat_runner.plugin_registry import GROUP_WAREHOUSES


class _ConformantWarehouse:
    """A minimal object implementing the full WarehouseProtocol surface."""

    @property
    def name(self) -> str:
        return "fake"

    @property
    def capabilities(self) -> frozenset[str]:
        return frozenset({"branching", "time_travel"})

    def list_namespaces(self) -> list[str]:
        return ["default"]

    def list_tables(self, namespace: str) -> list[str]:
        return ["orders"]

    def get_schema(self, ref: TableRef) -> pa.Schema:
        return pa.schema([("id", pa.int64())])

    def attach_for_runner(self, runner_type, conn, *, branch: str = "main") -> None:
        return None

    def write(self, ref, data, strategy, opts, *, branch: str = "main") -> int:
        return 0


class _PartialWarehouse:
    """Missing write() + attach_for_runner() — must NOT satisfy the Protocol."""

    @property
    def name(self) -> str:
        return "partial"

    def list_namespaces(self) -> list[str]:
        return []


def test_conformant_object_satisfies_protocol():
    assert isinstance(_ConformantWarehouse(), WarehouseProtocol)


def test_partial_object_does_not_satisfy_protocol():
    assert not isinstance(_PartialWarehouse(), WarehouseProtocol)


def test_table_ref_is_frozen_and_addressable():
    ref = TableRef(namespace="shop", layer="bronze", name="orders")
    assert (ref.namespace, ref.layer, ref.name) == ("shop", "bronze", "orders")


def test_warehouses_entry_point_group_name():
    # Stable seam name — plugins register under exactly this group.
    assert GROUP_WAREHOUSES == "rat.warehouses"
