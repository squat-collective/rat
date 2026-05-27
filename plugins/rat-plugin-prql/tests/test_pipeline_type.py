"""Tests for the PRQL pipeline type plugin."""

from __future__ import annotations

import pytest

from rat_plugin_prql.pipeline_type import PrqlPipelineType, compile_prql
from rat_runner.plugin_protocols import PipelineTypeProtocol


def test_protocol_attributes():
    """The plugin exposes the name and file_extension the registry expects."""
    plugin = PrqlPipelineType()
    assert plugin.name == "prql"
    assert plugin.file_extension == "prql"


def test_satisfies_protocol():
    """PrqlPipelineType structurally satisfies PipelineTypeProtocol."""
    assert isinstance(PrqlPipelineType(), PipelineTypeProtocol)


def test_compile_prql_produces_sql():
    """A simple PRQL query compiles to runnable SQL."""
    sql = compile_prql('from s"SELECT 1 AS n"')
    assert "SELECT" in sql.upper()


def test_compile_prql_transform():
    """A PRQL transform (filter + derive) compiles without error."""
    prql = 'from s"SELECT 1 AS amount"\nfilter amount > 0\nderive {doubled = amount * 2}'
    sql = compile_prql(prql)
    assert "SELECT" in sql.upper()


def test_compile_prql_rejects_invalid():
    """Invalid PRQL raises rather than producing garbage SQL."""
    with pytest.raises(Exception):  # noqa: B017 — prqlc raises its own error type
        compile_prql("this is not valid prql @@@")
