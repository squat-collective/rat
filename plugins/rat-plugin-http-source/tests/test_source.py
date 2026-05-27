"""Tests for the HTTP source connector plugin."""

from __future__ import annotations

import json
from unittest.mock import MagicMock, patch

import pytest

from rat_plugin_http_source.source import HttpSource, extract_records
from rat_runner.plugin_protocols import SourceConnectorProtocol


def _mock_urlopen(payload: object) -> MagicMock:
    """Build a urlopen mock whose context manager yields the given JSON payload."""
    resp = MagicMock()
    resp.read.return_value = json.dumps(payload).encode()
    resp.__enter__ = lambda s: s
    resp.__exit__ = MagicMock(return_value=False)
    return MagicMock(return_value=resp)


def test_protocol_attributes():
    assert HttpSource().name == "http"


def test_satisfies_protocol():
    """HttpSource structurally satisfies SourceConnectorProtocol."""
    assert isinstance(HttpSource(), SourceConnectorProtocol)


def test_fetch_json_array():
    """A top-level JSON array becomes one Arrow row per element."""
    rows = [{"id": 1, "name": "Alice"}, {"id": 2, "name": "Bob"}]
    with patch("rat_plugin_http_source.source.urllib.request.urlopen", _mock_urlopen(rows)):
        table = HttpSource().fetch({"url": "https://example.com/api"}, s3_config=None)
    assert table.num_rows == 2
    assert table.column_names == ["id", "name"]


def test_fetch_with_record_path():
    """record_path navigates to a nested list of records."""
    payload = {"data": {"items": [{"x": 1}, {"x": 2}, {"x": 3}]}}
    with patch("rat_plugin_http_source.source.urllib.request.urlopen", _mock_urlopen(payload)):
        table = HttpSource().fetch(
            {"url": "https://example.com/api", "record_path": "data.items"},
            s3_config=None,
        )
    assert table.num_rows == 3


def test_fetch_requires_url():
    with pytest.raises(ValueError, match="url"):
        HttpSource().fetch({}, s3_config=None)


def test_extract_records_single_object():
    """A single JSON object is wrapped as a one-row list."""
    assert extract_records({"id": 1}, None) == [{"id": 1}]


def test_extract_records_bad_path():
    with pytest.raises(ValueError, match="record_path"):
        extract_records({"data": "not-a-dict"}, "data.items")


def test_extract_records_non_list():
    with pytest.raises(ValueError, match="JSON array"):
        extract_records(42, None)
