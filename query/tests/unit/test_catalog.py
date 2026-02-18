"""Tests for catalog — Nessie discovery + DuckDB view registration."""

from __future__ import annotations

import json
import threading
import urllib.error
from unittest.mock import MagicMock, patch

import pytest

from rat_query.catalog import NessieCatalog, TableEntry
from rat_query.config import NessieConfig, S3Config


def _make_nessie_response(entries: list[dict]) -> bytes:
    """Create a mock Nessie v2 entries response."""
    return json.dumps({"entries": entries}).encode()


def _iceberg_entry(elements: list[str], metadata_location: str = "") -> dict:
    """Create a Nessie ICEBERG_TABLE entry."""
    entry: dict = {"type": "ICEBERG_TABLE", "name": {"elements": elements}}
    if metadata_location:
        entry["content"] = {
            "type": "ICEBERG_TABLE",
            "metadataLocation": metadata_location,
        }
    return entry


class TestNessieCatalog:
    def test_discover_tables(self, s3_config: S3Config, nessie_config: NessieConfig):
        mock_response = _make_nessie_response(
            [
                _iceberg_entry(
                    ["default", "bronze", "raw_events"],
                    "s3://test-bucket/default/bronze/raw_events_abc123/metadata/00000.metadata.json",
                ),
                _iceberg_entry(
                    ["default", "silver", "orders"],
                    "s3://test-bucket/default/silver/orders_def456/metadata/00000.metadata.json",
                ),
                _iceberg_entry(
                    ["default", "gold", "revenue"],
                    "s3://test-bucket/default/gold/revenue_ghi789/metadata/00000.metadata.json",
                ),
            ]
        )

        engine = MagicMock()
        catalog = NessieCatalog(nessie_config, s3_config, engine)

        with patch("rat_query.catalog.urlopen") as mock_urlopen:
            mock_resp = MagicMock()
            mock_resp.read.return_value = mock_response
            mock_resp.__enter__ = lambda s: s
            mock_resp.__exit__ = MagicMock(return_value=False)
            mock_urlopen.return_value = mock_resp

            tables = catalog.discover_tables("default")

        assert len(tables) == 3
        assert tables[0].namespace == "default"
        assert tables[0].layer == "bronze"
        assert tables[0].name == "raw_events"
        assert tables[0].s3_base_path == "s3://test-bucket/default/bronze/raw_events_abc123"
        assert tables[1].s3_base_path == "s3://test-bucket/default/silver/orders_def456"
        assert tables[2].s3_base_path == "s3://test-bucket/default/gold/revenue_ghi789"

    def test_discover_tables_filters_by_namespace(
        self, s3_config: S3Config, nessie_config: NessieConfig
    ):
        mock_response = _make_nessie_response(
            [
                _iceberg_entry(["default", "silver", "orders"]),
                _iceberg_entry(["other", "silver", "products"]),
            ]
        )

        engine = MagicMock()
        catalog = NessieCatalog(nessie_config, s3_config, engine)

        with patch("rat_query.catalog.urlopen") as mock_urlopen:
            mock_resp = MagicMock()
            mock_resp.read.return_value = mock_response
            mock_resp.__enter__ = lambda s: s
            mock_resp.__exit__ = MagicMock(return_value=False)
            mock_urlopen.return_value = mock_resp

            tables = catalog.discover_tables("default")

        assert len(tables) == 1
        assert tables[0].name == "orders"

    def test_discover_tables_skips_non_iceberg(
        self, s3_config: S3Config, nessie_config: NessieConfig
    ):
        mock_response = _make_nessie_response(
            [
                {"type": "NAMESPACE", "name": {"elements": ["default"]}},
                _iceberg_entry(["default", "silver", "orders"]),
            ]
        )

        engine = MagicMock()
        catalog = NessieCatalog(nessie_config, s3_config, engine)

        with patch("rat_query.catalog.urlopen") as mock_urlopen:
            mock_resp = MagicMock()
            mock_resp.read.return_value = mock_response
            mock_resp.__enter__ = lambda s: s
            mock_resp.__exit__ = MagicMock(return_value=False)
            mock_urlopen.return_value = mock_resp

            tables = catalog.discover_tables("default")

        assert len(tables) == 1

    def test_discover_tables_skips_invalid_layer(
        self, s3_config: S3Config, nessie_config: NessieConfig
    ):
        mock_response = _make_nessie_response(
            [
                _iceberg_entry(["default", "staging", "temp"]),
            ]
        )

        engine = MagicMock()
        catalog = NessieCatalog(nessie_config, s3_config, engine)

        with patch("rat_query.catalog.urlopen") as mock_urlopen:
            mock_resp = MagicMock()
            mock_resp.read.return_value = mock_response
            mock_resp.__enter__ = lambda s: s
            mock_resp.__exit__ = MagicMock(return_value=False)
            mock_urlopen.return_value = mock_resp

            tables = catalog.discover_tables("default")

        assert len(tables) == 0

    def test_register_tables_creates_views(self, s3_config: S3Config, nessie_config: NessieConfig):
        engine = MagicMock()
        catalog = NessieCatalog(nessie_config, s3_config, engine)

        with patch.object(catalog, "discover_tables") as mock_discover:
            mock_discover.return_value = [
                TableEntry(
                    namespace="default",
                    layer="silver",
                    name="orders",
                    s3_base_path="s3://test-bucket/default/silver/orders_abc",
                ),
                TableEntry(
                    namespace="default",
                    layer="gold",
                    name="revenue",
                    s3_base_path="s3://test-bucket/default/gold/revenue_def",
                ),
            ]
            catalog.register_tables("default")

        # No drop_all_views — uses CREATE OR REPLACE VIEW instead.
        engine.drop_all_views.assert_not_called()
        assert engine.register_view.call_count == 2
        engine.register_view.assert_any_call(
            "silver",
            "orders",
            "s3://test-bucket/default/silver/orders_abc",
            namespace="default",
        )
        engine.register_view.assert_any_call(
            "gold",
            "revenue",
            "s3://test-bucket/default/gold/revenue_def",
            namespace="default",
        )

    def test_register_tables_drops_stale_views(
        self, s3_config: S3Config, nessie_config: NessieConfig
    ):
        engine = MagicMock()
        catalog = NessieCatalog(nessie_config, s3_config, engine)

        # First registration: 2 tables.
        with patch.object(catalog, "discover_tables") as mock_discover:
            mock_discover.return_value = [
                TableEntry(
                    namespace="default",
                    layer="silver",
                    name="orders",
                    s3_base_path="s3://test-bucket/default/silver/orders_abc",
                ),
                TableEntry(
                    namespace="default",
                    layer="gold",
                    name="revenue",
                    s3_base_path="s3://test-bucket/default/gold/revenue_def",
                ),
            ]
            catalog.register_tables("default")

        engine.reset_mock()

        # Second registration: only 1 table — "revenue" was removed.
        with patch.object(catalog, "discover_tables") as mock_discover:
            mock_discover.return_value = [
                TableEntry(
                    namespace="default",
                    layer="silver",
                    name="orders",
                    s3_base_path="s3://test-bucket/default/silver/orders_abc",
                ),
            ]
            catalog.register_tables("default")

        # "orders" path is unchanged from the first registration — the
        # commit-hash / path-diff optimization skips re-registering it.
        engine.register_view.assert_not_called()
        engine.drop_view.assert_called_once_with(
            "gold",
            "revenue",
            namespace="default",
        )

    def test_register_tables_empty_namespace(
        self, s3_config: S3Config, nessie_config: NessieConfig
    ):
        engine = MagicMock()
        catalog = NessieCatalog(nessie_config, s3_config, engine)

        with patch.object(catalog, "discover_tables") as mock_discover:
            mock_discover.return_value = []
            catalog.register_tables("empty")

        engine.drop_all_views.assert_not_called()
        engine.register_view.assert_not_called()

    def test_get_tables_returns_cached(self, s3_config: S3Config, nessie_config: NessieConfig):
        engine = MagicMock()
        catalog = NessieCatalog(nessie_config, s3_config, engine)

        with patch.object(catalog, "discover_tables") as mock_discover:
            mock_discover.return_value = [
                TableEntry(namespace="default", layer="silver", name="orders"),
                TableEntry(namespace="default", layer="gold", name="revenue"),
            ]
            catalog.register_tables("default")

        # get_tables should return cached results without calling Nessie again
        tables = catalog.get_tables("default")
        assert len(tables) == 2

    def test_get_tables_filters_by_layer(self, s3_config: S3Config, nessie_config: NessieConfig):
        engine = MagicMock()
        catalog = NessieCatalog(nessie_config, s3_config, engine)

        with patch.object(catalog, "discover_tables") as mock_discover:
            mock_discover.return_value = [
                TableEntry(namespace="default", layer="silver", name="orders"),
                TableEntry(namespace="default", layer="gold", name="revenue"),
            ]
            catalog.register_tables("default")

        tables = catalog.get_tables("default", layer_filter="silver")
        assert len(tables) == 1
        assert tables[0].name == "orders"

    def test_refresh_loop_calls_register(self, s3_config: S3Config, nessie_config: NessieConfig):
        engine = MagicMock()
        catalog = NessieCatalog(nessie_config, s3_config, engine)
        stop_event = threading.Event()

        with patch.object(catalog, "register_tables") as mock_register:
            # Run refresh loop in a thread with very short interval
            thread = threading.Thread(
                target=catalog.refresh_loop,
                args=("default", stop_event, 0.05),
            )
            thread.start()

            # Let it run a couple iterations
            import time

            time.sleep(0.15)
            stop_event.set()
            thread.join(timeout=1)

        assert mock_register.call_count >= 2


def _ok_urlopen_response(data: bytes) -> MagicMock:
    """Helper to create a successful urlopen response mock."""
    resp = MagicMock()
    resp.read.return_value = data
    resp.__enter__ = lambda s: s
    resp.__exit__ = MagicMock(return_value=False)
    return resp


def _http_error(code: int, msg: str = "Error") -> urllib.error.HTTPError:
    """Helper to create an HTTPError with less boilerplate."""
    return urllib.error.HTTPError(
        url="http://localhost:19120/api/v2/trees/main/entries",
        code=code,
        msg=msg,
        hdrs=None,  # type: ignore[arg-type]
        fp=None,
    )


class TestDiscoverTablesUrlopen:
    """Tests that verify urlopen is called with a timeout and that error
    scenarios (timeout, connection refused, HTTP errors, malformed responses)
    are properly surfaced.
    """

    def test_urlopen_called_with_timeout(self, s3_config: S3Config, nessie_config: NessieConfig):
        """Verify that discover_tables passes timeout=10 to urlopen."""
        engine = MagicMock()
        catalog = NessieCatalog(nessie_config, s3_config, engine)
        response_data = _make_nessie_response([])

        with patch("rat_query.catalog.urlopen") as mock_urlopen:
            mock_urlopen.return_value = _ok_urlopen_response(response_data)
            catalog.discover_tables("default")

        # urlopen should be called with (request, timeout=10)
        assert mock_urlopen.call_count == 1
        _args, kwargs = mock_urlopen.call_args
        assert kwargs.get("timeout") == 10 or (len(_args) >= 2 and _args[1] == 10)

    def test_urlopen_request_has_accept_header(
        self, s3_config: S3Config, nessie_config: NessieConfig
    ):
        """Verify the request includes Accept: application/json header."""
        engine = MagicMock()
        catalog = NessieCatalog(nessie_config, s3_config, engine)
        response_data = _make_nessie_response([])

        with patch("rat_query.catalog.urlopen") as mock_urlopen:
            mock_urlopen.return_value = _ok_urlopen_response(response_data)
            catalog.discover_tables("default")

        req_obj = mock_urlopen.call_args[0][0]
        assert req_obj.get_header("Accept") == "application/json"

    def test_urlopen_request_url_contains_entries_endpoint(
        self, s3_config: S3Config, nessie_config: NessieConfig
    ):
        """Verify the request URL points to the Nessie v2 entries endpoint."""
        engine = MagicMock()
        catalog = NessieCatalog(nessie_config, s3_config, engine)
        response_data = _make_nessie_response([])

        with patch("rat_query.catalog.urlopen") as mock_urlopen:
            mock_urlopen.return_value = _ok_urlopen_response(response_data)
            catalog.discover_tables("default")

        req_obj = mock_urlopen.call_args[0][0]
        assert "/trees/main/entries" in req_obj.full_url
        assert "content=true" in req_obj.full_url

    def test_timeout_error_propagates(self, s3_config: S3Config, nessie_config: NessieConfig):
        """TimeoutError from urlopen should propagate to the caller."""
        engine = MagicMock()
        catalog = NessieCatalog(nessie_config, s3_config, engine)

        with patch("rat_query.catalog.urlopen") as mock_urlopen:
            mock_urlopen.side_effect = TimeoutError("timed out")

            with pytest.raises(TimeoutError, match="timed out"):
                catalog.discover_tables("default")

    def test_url_error_connection_refused_propagates(
        self, s3_config: S3Config, nessie_config: NessieConfig
    ):
        """URLError (connection refused) should propagate to the caller."""
        engine = MagicMock()
        catalog = NessieCatalog(nessie_config, s3_config, engine)

        with patch("rat_query.catalog.urlopen") as mock_urlopen:
            mock_urlopen.side_effect = urllib.error.URLError(
                reason="[Errno 111] Connection refused"
            )

            with pytest.raises(urllib.error.URLError, match="Connection refused"):
                catalog.discover_tables("default")

    def test_url_error_dns_failure_propagates(
        self, s3_config: S3Config, nessie_config: NessieConfig
    ):
        """URLError (DNS failure) should propagate to the caller."""
        engine = MagicMock()
        catalog = NessieCatalog(nessie_config, s3_config, engine)

        with patch("rat_query.catalog.urlopen") as mock_urlopen:
            mock_urlopen.side_effect = urllib.error.URLError(
                reason="[Errno -2] Name or service not known"
            )

            with pytest.raises(urllib.error.URLError, match="Name or service not known"):
                catalog.discover_tables("default")

    def test_http_404_propagates(self, s3_config: S3Config, nessie_config: NessieConfig):
        """HTTP 404 from Nessie should propagate as HTTPError."""
        engine = MagicMock()
        catalog = NessieCatalog(nessie_config, s3_config, engine)

        with patch("rat_query.catalog.urlopen") as mock_urlopen:
            mock_urlopen.side_effect = _http_error(404, "Not Found")

            with pytest.raises(urllib.error.HTTPError) as exc_info:
                catalog.discover_tables("default")
            assert exc_info.value.code == 404

    def test_http_500_propagates(self, s3_config: S3Config, nessie_config: NessieConfig):
        """HTTP 500 from Nessie should propagate as HTTPError."""
        engine = MagicMock()
        catalog = NessieCatalog(nessie_config, s3_config, engine)

        with patch("rat_query.catalog.urlopen") as mock_urlopen:
            mock_urlopen.side_effect = _http_error(500, "Internal Server Error")

            with pytest.raises(urllib.error.HTTPError) as exc_info:
                catalog.discover_tables("default")
            assert exc_info.value.code == 500

    def test_http_503_propagates(self, s3_config: S3Config, nessie_config: NessieConfig):
        """HTTP 503 (Service Unavailable) should propagate as HTTPError."""
        engine = MagicMock()
        catalog = NessieCatalog(nessie_config, s3_config, engine)

        with patch("rat_query.catalog.urlopen") as mock_urlopen:
            mock_urlopen.side_effect = _http_error(503, "Service Unavailable")

            with pytest.raises(urllib.error.HTTPError) as exc_info:
                catalog.discover_tables("default")
            assert exc_info.value.code == 503

    def test_malformed_json_raises_error(self, s3_config: S3Config, nessie_config: NessieConfig):
        """Malformed JSON from Nessie should raise a decode error."""
        engine = MagicMock()
        catalog = NessieCatalog(nessie_config, s3_config, engine)

        with patch("rat_query.catalog.urlopen") as mock_urlopen:
            mock_urlopen.return_value = _ok_urlopen_response(b"not-json{{{")

            with pytest.raises(json.JSONDecodeError):
                catalog.discover_tables("default")

    def test_empty_json_object_returns_no_tables(
        self, s3_config: S3Config, nessie_config: NessieConfig
    ):
        """A valid JSON response with no 'entries' key returns an empty list."""
        engine = MagicMock()
        catalog = NessieCatalog(nessie_config, s3_config, engine)

        with patch("rat_query.catalog.urlopen") as mock_urlopen:
            mock_urlopen.return_value = _ok_urlopen_response(b"{}")

            tables = catalog.discover_tables("default")
            assert tables == []

    def test_entries_with_missing_name_elements_skipped(
        self, s3_config: S3Config, nessie_config: NessieConfig
    ):
        """Entries with fewer than 3 name elements are skipped gracefully."""
        engine = MagicMock()
        catalog = NessieCatalog(nessie_config, s3_config, engine)
        response_data = _make_nessie_response(
            [
                {"type": "ICEBERG_TABLE", "name": {"elements": ["default", "silver"]}},
                {"type": "ICEBERG_TABLE", "name": {"elements": ["default"]}},
                {"type": "ICEBERG_TABLE", "name": {"elements": []}},
            ]
        )

        with patch("rat_query.catalog.urlopen") as mock_urlopen:
            mock_urlopen.return_value = _ok_urlopen_response(response_data)

            tables = catalog.discover_tables("default")
            assert tables == []

    def test_entries_with_no_metadata_location_have_empty_s3_base(
        self, s3_config: S3Config, nessie_config: NessieConfig
    ):
        """Entries without metadataLocation should get an empty s3_base_path."""
        engine = MagicMock()
        catalog = NessieCatalog(nessie_config, s3_config, engine)
        response_data = _make_nessie_response(
            [
                _iceberg_entry(["default", "bronze", "raw_data"]),
            ]
        )

        with patch("rat_query.catalog.urlopen") as mock_urlopen:
            mock_urlopen.return_value = _ok_urlopen_response(response_data)

            tables = catalog.discover_tables("default")
            assert len(tables) == 1
            assert tables[0].s3_base_path == ""


class TestRefreshLoopErrorHandling:
    """Tests that the refresh_loop catches exceptions and continues."""

    def test_refresh_loop_continues_after_timeout_error(
        self, s3_config: S3Config, nessie_config: NessieConfig
    ):
        """refresh_loop should log exceptions and continue, not crash."""
        engine = MagicMock()
        catalog = NessieCatalog(nessie_config, s3_config, engine)
        stop_event = threading.Event()
        call_count = 0

        def _failing_then_ok(namespace: str) -> None:
            nonlocal call_count
            call_count += 1
            if call_count == 1:
                raise TimeoutError("timed out")
            # On subsequent calls, stop the loop
            stop_event.set()

        with patch.object(catalog, "register_tables", side_effect=_failing_then_ok):
            thread = threading.Thread(
                target=catalog.refresh_loop,
                args=("default", stop_event, 0.05),
            )
            thread.start()
            thread.join(timeout=2)

        # Should have been called at least twice — first fails, second succeeds
        assert call_count >= 2

    def test_refresh_loop_continues_after_connection_refused(
        self, s3_config: S3Config, nessie_config: NessieConfig
    ):
        """refresh_loop should survive URLError and keep retrying."""
        engine = MagicMock()
        catalog = NessieCatalog(nessie_config, s3_config, engine)
        stop_event = threading.Event()
        call_count = 0

        def _failing_then_ok(namespace: str) -> None:
            nonlocal call_count
            call_count += 1
            if call_count == 1:
                raise urllib.error.URLError("Connection refused")
            stop_event.set()

        with patch.object(catalog, "register_tables", side_effect=_failing_then_ok):
            thread = threading.Thread(
                target=catalog.refresh_loop,
                args=("default", stop_event, 0.05),
            )
            thread.start()
            thread.join(timeout=2)

        assert call_count >= 2
