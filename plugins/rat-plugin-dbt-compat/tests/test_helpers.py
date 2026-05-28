"""Tests for dbt-compat Jinja helpers."""

from __future__ import annotations

import os
from unittest.mock import MagicMock, patch

import pytest

from rat_plugin_dbt_compat.helpers import (
    EnvVarHelper,
    GenerateSchemaNameHelper,
    VarHelper,
)


# ── Protocol compliance ───────────────────────────────────────────


class TestProtocolCompliance:
    """All helpers must satisfy JinjaHelperProtocol (name + __call__)."""

    def test_env_var_implements_protocol(self):
        from rat_runner.plugin_protocols import JinjaHelperProtocol

        helper = EnvVarHelper()
        assert isinstance(helper, JinjaHelperProtocol)
        assert helper.name == "env_var"

    def test_var_implements_protocol(self):
        from rat_runner.plugin_protocols import JinjaHelperProtocol

        helper = VarHelper()
        assert isinstance(helper, JinjaHelperProtocol)
        assert helper.name == "var"

    def test_generate_schema_name_implements_protocol(self):
        from rat_runner.plugin_protocols import JinjaHelperProtocol

        helper = GenerateSchemaNameHelper()
        assert isinstance(helper, JinjaHelperProtocol)
        assert helper.name == "generate_schema_name"


# ── EnvVarHelper ──────────────────────────────────────────────────


class TestEnvVarHelper:
    def test_reads_existing_env_var(self):
        helper = EnvVarHelper()
        # HOME is always set
        result = helper("HOME")
        assert isinstance(result, str)
        assert len(result) > 0

    def test_raises_on_missing_env_var(self):
        helper = EnvVarHelper()
        with pytest.raises(ValueError, match="NONEXISTENT_XYZ_12345"):
            helper("NONEXISTENT_XYZ_12345")

    def test_returns_default_for_missing_env_var(self):
        helper = EnvVarHelper()
        result = helper("NONEXISTENT_XYZ_12345", "fallback")
        assert result == "fallback"

    def test_prefers_env_var_over_default(self):
        helper = EnvVarHelper()
        result = helper("HOME", "fallback")
        assert result != "fallback"

    def test_raises_on_no_arguments(self):
        helper = EnvVarHelper()
        with pytest.raises(TypeError, match="requires at least 1 argument"):
            helper()


# ── VarHelper ─────────────────────────────────────────────────────


class TestVarHelper:
    def test_returns_key_name(self):
        helper = VarHelper()
        assert helper("batch_size") == "batch_size"

    def test_raises_on_no_arguments(self):
        helper = VarHelper()
        with pytest.raises(TypeError, match="requires at least 1 argument"):
            helper()


# ── GenerateSchemaNameHelper ──────────────────────────────────────


class TestGenerateSchemaNameHelper:
    def test_returns_argument_as_is(self):
        helper = GenerateSchemaNameHelper()
        assert helper("custom") == "custom"

    def test_raises_on_no_arguments(self):
        helper = GenerateSchemaNameHelper()
        with pytest.raises(TypeError, match="requires at least 1 argument"):
            helper()


# ── Jinja template integration ────────────────────────────────────


class TestJinjaIntegration:
    """Verify helpers work inside compile_sql() via plugin_helpers param."""

    @patch("rat_runner.templating._resolve_ref", return_value="iceberg_scan('fake')")
    def test_env_var_works_in_sql_template(self, mock_ref):
        from rat_runner.templating import compile_sql

        helper = EnvVarHelper()
        raw_sql = "SELECT '{{ env_var('HOME') }}' AS home_dir"

        result = compile_sql(
            raw_sql,
            "ns",
            "silver",
            "test_pipe",
            MagicMock(),
            MagicMock(),
            plugin_helpers={"env_var": helper},
        )

        assert os.environ["HOME"] in result

    @patch("rat_runner.templating._resolve_ref", return_value="iceberg_scan('fake')")
    def test_var_works_in_sql_template(self, mock_ref):
        from rat_runner.templating import compile_sql

        helper = VarHelper()
        raw_sql = "SELECT '{{ var('batch_size') }}' AS batch"

        result = compile_sql(
            raw_sql,
            "ns",
            "silver",
            "test_pipe",
            MagicMock(),
            MagicMock(),
            plugin_helpers={"var": helper},
        )

        assert "batch_size" in result

    @patch("rat_runner.templating._resolve_ref", return_value="iceberg_scan('fake')")
    def test_builtin_ref_cannot_be_overridden(self, mock_ref):
        from rat_runner.templating import compile_sql

        raw_sql = "SELECT {{ ref('silver.other') }} AS val"

        result = compile_sql(
            raw_sql,
            "ns",
            "silver",
            "test_pipe",
            MagicMock(),
            MagicMock(),
            plugin_helpers={"ref": lambda x: "HACKED"},
        )

        assert "iceberg_scan" in result
        assert "HACKED" not in result
