"""Tests for arrow_ipc — Arrow IPC serialization helpers."""

from __future__ import annotations

import pyarrow as pa

from rat_query.arrow_ipc import columns_from_schema, table_to_ipc


class TestTableToIpc:
    def test_serialize_table(self):
        table = pa.table({"id": [1, 2, 3], "name": ["a", "b", "c"]})
        data = table_to_ipc(table)
        assert isinstance(data, bytes)
        assert len(data) > 0

    def test_empty_table(self):
        table = pa.table({"id": pa.array([], type=pa.int64())})
        data = table_to_ipc(table)
        assert isinstance(data, bytes)
        assert len(data) > 0

    def test_round_trip(self):
        table = pa.table({"x": [1, 2, 3], "y": ["a", "b", "c"]})
        data = table_to_ipc(table)

        reader = pa.ipc.open_stream(data)
        result = reader.read_all()

        assert result.equals(table)


class TestColumnsFromSchema:
    def test_extracts_columns(self):
        schema = pa.schema(
            [
                pa.field("id", pa.int64()),
                pa.field("name", pa.string()),
                pa.field("score", pa.float64()),
            ]
        )
        cols = columns_from_schema(schema)
        assert cols == [("id", "int64"), ("name", "string"), ("score", "double")]

    def test_empty_schema(self):
        schema = pa.schema([])
        cols = columns_from_schema(schema)
        assert cols == []
