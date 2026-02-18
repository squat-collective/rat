"""Tests for python_exec — Python pipeline execution via exec()."""

from __future__ import annotations

from unittest.mock import MagicMock

import pyarrow as pa
import pytest

from rat_runner.config import NessieConfig, S3Config
from rat_runner.python_exec import (
    _SandboxViolationError,
    _validate_source,
    execute_python_pipeline,
)


def _make_engine() -> MagicMock:
    engine = MagicMock()
    engine.conn = MagicMock()
    return engine


class TestExecutePythonPipeline:
    def test_returns_result_table(self, s3_config: S3Config, nessie_config: NessieConfig):
        source = "result = pa.table({'x': [1, 2, 3]})"
        engine = _make_engine()

        table = execute_python_pipeline(
            source, engine, "ns", "silver", "orders", s3_config, nessie_config
        )

        assert isinstance(table, pa.Table)
        assert len(table) == 3
        assert table.column_names == ["x"]

    def test_no_result_raises_value_error(self, s3_config: S3Config, nessie_config: NessieConfig):
        source = "x = 42  # forgot to set result"
        engine = _make_engine()

        with pytest.raises(ValueError, match="must set `result`"):
            execute_python_pipeline(
                source, engine, "ns", "silver", "orders", s3_config, nessie_config
            )

    def test_non_table_result_raises_value_error(
        self, s3_config: S3Config, nessie_config: NessieConfig
    ):
        source = "result = [1, 2, 3]  # not a pa.Table"
        engine = _make_engine()

        with pytest.raises(ValueError, match="must set `result`"):
            execute_python_pipeline(
                source, engine, "ns", "silver", "orders", s3_config, nessie_config
            )

    def test_globals_injected(self, s3_config: S3Config, nessie_config: NessieConfig):
        # Verify all expected globals are available
        # NOTE: isinstance/type are blocked in the sandbox, so use identity checks
        source = """
assert duckdb_conn is not None
assert pa is not None
assert callable(ref)
assert this == "ns.silver.orders"
assert run_started_at  # truthy string
assert is_incremental is True or is_incremental is False
result = pa.table({'ok': [True]})
"""
        engine = _make_engine()

        table = execute_python_pipeline(
            source, engine, "ns", "silver", "orders", s3_config, nessie_config
        )

        assert len(table) == 1

    def test_ref_works(self, s3_config: S3Config, nessie_config: NessieConfig):
        source = """
path = ref('bronze.events')
assert 'iceberg_scan' in path
result = pa.table({'ref_test': [path]})
"""
        engine = _make_engine()

        table = execute_python_pipeline(
            source, engine, "ns", "silver", "orders", s3_config, nessie_config
        )

        assert "iceberg_scan" in table.column("ref_test")[0].as_py()

    def test_exception_propagates(self, s3_config: S3Config, nessie_config: NessieConfig):
        source = "raise RuntimeError('pipeline error')"
        engine = _make_engine()

        with pytest.raises(RuntimeError, match="pipeline error"):
            execute_python_pipeline(
                source, engine, "ns", "silver", "orders", s3_config, nessie_config
            )

    @pytest.mark.parametrize("blocked", ["os", "subprocess", "sys", "shutil", "socket"])
    def test_blocked_imports_raise_error(
        self, s3_config: S3Config, nessie_config: NessieConfig, blocked: str
    ):
        source = f"import {blocked}\nresult = pa.table({{'x': [1]}})"
        engine = _make_engine()

        with pytest.raises(ImportError, match="not allowed"):
            execute_python_pipeline(
                source, engine, "ns", "silver", "orders", s3_config, nessie_config
            )

    def test_blocked_builtins_unavailable(self, s3_config: S3Config, nessie_config: NessieConfig):
        source = """
try:
    eval('1+1')
    result = pa.table({'ok': [False]})
except NameError:
    result = pa.table({'ok': [True]})
"""
        engine = _make_engine()
        table = execute_python_pipeline(
            source, engine, "ns", "silver", "orders", s3_config, nessie_config
        )
        assert table.column("ok")[0].as_py() is True

    def test_landing_zone_available(self, s3_config: S3Config, nessie_config: NessieConfig):
        source = """
assert callable(landing_zone)
path = landing_zone('uploads')
assert 's3://' in path
assert '/landing/uploads/' in path
result = pa.table({'lz': [path]})
"""
        engine = _make_engine()
        table = execute_python_pipeline(
            source, engine, "ns", "silver", "orders", s3_config, nessie_config
        )
        lz_path = table.column("lz")[0].as_py()
        assert lz_path == "s3://test-bucket/ns/landing/uploads/**"


class TestSandboxSecurity:
    """Tests for sandbox escape prevention."""

    def test_dunder_class_blocked(self, s3_config: S3Config, nessie_config: NessieConfig):
        source = 'x = "".__class__\nresult = pa.table({"x": [1]})'
        engine = _make_engine()
        with pytest.raises(_SandboxViolationError, match="__class__"):
            execute_python_pipeline(source, engine, "ns", "silver", "t", s3_config, nessie_config)

    def test_dunder_subclasses_blocked(self, s3_config: S3Config, nessie_config: NessieConfig):
        source = 'x = object.__subclasses__()\nresult = pa.table({"x": [1]})'
        engine = _make_engine()
        with pytest.raises(_SandboxViolationError, match="__subclasses__"):
            execute_python_pipeline(source, engine, "ns", "silver", "t", s3_config, nessie_config)

    def test_dunder_globals_blocked(self, s3_config: S3Config, nessie_config: NessieConfig):
        source = 'x = ref.__globals__\nresult = pa.table({"x": [1]})'
        engine = _make_engine()
        with pytest.raises(_SandboxViolationError, match="__globals__"):
            execute_python_pipeline(source, engine, "ns", "silver", "t", s3_config, nessie_config)

    def test_dunder_bases_blocked(self, s3_config: S3Config, nessie_config: NessieConfig):
        source = 'x = ().__class__.__bases__\nresult = pa.table({"x": [1]})'
        engine = _make_engine()
        with pytest.raises(_SandboxViolationError, match="__bases__|__class__"):
            execute_python_pipeline(source, engine, "ns", "silver", "t", s3_config, nessie_config)

    def test_dunder_mro_blocked(self, s3_config: S3Config, nessie_config: NessieConfig):
        source = 'x = str.__mro__\nresult = pa.table({"x": [1]})'
        engine = _make_engine()
        with pytest.raises(_SandboxViolationError, match="__mro__"):
            execute_python_pipeline(source, engine, "ns", "silver", "t", s3_config, nessie_config)

    def test_subscript_dunder_blocked(self, s3_config: S3Config, nessie_config: NessieConfig):
        source = 'x = {}["__class__"]\nresult = pa.table({"x": [1]})'
        engine = _make_engine()
        with pytest.raises(_SandboxViolationError, match="__class__"):
            execute_python_pipeline(source, engine, "ns", "silver", "t", s3_config, nessie_config)

    def test_string_literal_dunder_blocked(self, s3_config: S3Config, nessie_config: NessieConfig):
        source = 'x = "__globals__"\nresult = pa.table({"x": [1]})'
        engine = _make_engine()
        with pytest.raises(_SandboxViolationError, match="__globals__"):
            execute_python_pipeline(source, engine, "ns", "silver", "t", s3_config, nessie_config)

    def test_type_builtin_blocked(self, s3_config: S3Config, nessie_config: NessieConfig):
        source = 'x = type("X", (), {})\nresult = pa.table({"x": [1]})'
        engine = _make_engine()
        with pytest.raises(NameError):
            execute_python_pipeline(source, engine, "ns", "silver", "t", s3_config, nessie_config)

    def test_duckdb_copy_blocked(self, s3_config: S3Config, nessie_config: NessieConfig):
        source = """
duckdb_conn.execute("COPY (SELECT 1) TO '/tmp/pwned' (HEADER FALSE)")
result = pa.table({"x": [1]})
"""
        engine = _make_engine()
        with pytest.raises(_SandboxViolationError, match="SQL command not allowed"):
            execute_python_pipeline(source, engine, "ns", "silver", "t", s3_config, nessie_config)

    def test_duckdb_attach_blocked(self, s3_config: S3Config, nessie_config: NessieConfig):
        source = """
duckdb_conn.execute("ATTACH '/etc/passwd' AS pwned")
result = pa.table({"x": [1]})
"""
        engine = _make_engine()
        with pytest.raises(_SandboxViolationError, match="SQL command not allowed"):
            execute_python_pipeline(source, engine, "ns", "silver", "t", s3_config, nessie_config)

    def test_duckdb_safe_query_allowed(self, s3_config: S3Config, nessie_config: NessieConfig):
        source = """
duckdb_conn.execute("SELECT 1 AS x")
result = pa.table({"ok": [True]})
"""
        engine = _make_engine()
        table = execute_python_pipeline(
            source, engine, "ns", "silver", "t", s3_config, nessie_config
        )
        assert table.column("ok")[0].as_py() is True

    def test_duckdb_private_attr_blocked(self, s3_config: S3Config, nessie_config: NessieConfig):
        source = """
x = duckdb_conn._conn
result = pa.table({"x": [1]})
"""
        engine = _make_engine()
        with pytest.raises(_SandboxViolationError, match="private attribute"):
            execute_python_pipeline(source, engine, "ns", "silver", "t", s3_config, nessie_config)

    def test_import_builtins_blocked(self, s3_config: S3Config, nessie_config: NessieConfig):
        source = "import builtins\nresult = pa.table({'x': [1]})"
        engine = _make_engine()
        with pytest.raises(ImportError, match="not allowed"):
            execute_python_pipeline(source, engine, "ns", "silver", "t", s3_config, nessie_config)

    def test_import_inspect_blocked(self, s3_config: S3Config, nessie_config: NessieConfig):
        source = "import inspect\nresult = pa.table({'x': [1]})"
        engine = _make_engine()
        with pytest.raises(ImportError, match="not allowed"):
            execute_python_pipeline(source, engine, "ns", "silver", "t", s3_config, nessie_config)


class TestValidateSource:
    """Unit tests for _validate_source AST checker."""

    def test_safe_code_passes(self):
        _validate_source("x = 1 + 2\ny = [1, 2, 3]")

    def test_dunder_attribute_rejected(self):
        with pytest.raises(_SandboxViolationError, match="__class__"):
            _validate_source("x.__class__")

    def test_syntax_error_rejected(self):
        with pytest.raises(_SandboxViolationError, match="syntax error"):
            _validate_source("def f(")

    def test_dunder_dict_rejected(self):
        with pytest.raises(_SandboxViolationError, match="__dict__"):
            _validate_source("x.__dict__")

    def test_dunder_reduce_rejected(self):
        with pytest.raises(_SandboxViolationError, match="__reduce__"):
            _validate_source("x.__reduce__()")
