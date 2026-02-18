"""Arrow IPC serialization helpers."""

from __future__ import annotations

import pyarrow as pa


def table_to_ipc(table: pa.Table) -> bytes:
    """Serialize a PyArrow Table to Arrow IPC stream format."""
    sink = pa.BufferOutputStream()
    writer = pa.ipc.new_stream(sink, table.schema)
    writer.write_table(table)
    writer.close()
    return sink.getvalue().to_pybytes()


def columns_from_schema(schema: pa.Schema) -> list[tuple[str, str]]:
    """Extract (name, type_string) pairs from a PyArrow schema."""
    return [(field.name, str(field.type)) for field in schema]
