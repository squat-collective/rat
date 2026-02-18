"""Tests for callback — push-based status updates to ratd."""

from __future__ import annotations

import json
import threading
from http.server import BaseHTTPRequestHandler, HTTPServer
from unittest.mock import patch

from rat_runner.callback import notify_run_complete
from rat_runner.models import RunState, RunStatus


def _make_terminal_run(status: RunStatus = RunStatus.SUCCESS) -> RunState:
    """Create a RunState in terminal state for callback testing."""
    run = RunState(
        run_id="test-run-123",
        namespace="default",
        layer="silver",
        pipeline_name="orders",
        trigger="manual",
    )
    run.status = status
    run.duration_ms = 5000
    run.rows_written = 42
    run.error = "" if status == RunStatus.SUCCESS else "DuckDB OOM"
    run.archived_zones = ["default/raw-uploads"]
    return run


class TestNotifyRunComplete:
    """Tests for the notify_run_complete function."""

    def test_noop_when_callback_url_empty(self) -> None:
        """Should silently do nothing when RATD_CALLBACK_URL is not set."""
        run = _make_terminal_run()
        with patch("rat_runner.callback.RATD_CALLBACK_URL", ""):
            # Should not raise or make any HTTP calls
            notify_run_complete(run)

    def test_noop_for_non_terminal_run(self) -> None:
        """Should skip callback for runs that are still running."""
        run = RunState(
            run_id="test-run",
            namespace="default",
            layer="silver",
            pipeline_name="orders",
            trigger="manual",
        )
        run.status = RunStatus.RUNNING

        with patch("rat_runner.callback.RATD_CALLBACK_URL", "http://ratd:8080"):
            with patch("rat_runner.callback.urllib.request.urlopen") as mock_urlopen:
                notify_run_complete(run)
                mock_urlopen.assert_not_called()

    def test_posts_success_status(self) -> None:
        """Should POST correct JSON payload for successful runs."""
        run = _make_terminal_run(RunStatus.SUCCESS)
        captured_data = {}

        class Handler(BaseHTTPRequestHandler):
            def do_POST(self):
                length = int(self.headers["Content-Length"])
                body = json.loads(self.rfile.read(length))
                captured_data.update(body)
                captured_data["path"] = self.path
                self.send_response(200)
                self.end_headers()

            def log_message(self, *args):
                pass  # suppress stdout

        server = HTTPServer(("127.0.0.1", 0), Handler)
        port = server.server_address[1]
        thread = threading.Thread(target=server.handle_request, daemon=True)
        thread.start()

        with patch("rat_runner.callback.RATD_CALLBACK_URL", f"http://127.0.0.1:{port}"):
            notify_run_complete(run)

        thread.join(timeout=5)
        server.server_close()

        assert captured_data["path"] == "/api/v1/internal/runs/test-run-123/status"
        assert captured_data["run_id"] == "test-run-123"
        assert captured_data["status"] == "success"
        assert captured_data["duration_ms"] == 5000
        assert captured_data["rows_written"] == 42
        assert captured_data["archived_landing_zones"] == ["default/raw-uploads"]
        assert captured_data["error"] == ""

    def test_posts_failed_status_with_error(self) -> None:
        """Should include error message for failed runs."""
        run = _make_terminal_run(RunStatus.FAILED)
        captured_data = {}

        class Handler(BaseHTTPRequestHandler):
            def do_POST(self):
                length = int(self.headers["Content-Length"])
                body = json.loads(self.rfile.read(length))
                captured_data.update(body)
                self.send_response(200)
                self.end_headers()

            def log_message(self, *args):
                pass

        server = HTTPServer(("127.0.0.1", 0), Handler)
        port = server.server_address[1]
        thread = threading.Thread(target=server.handle_request, daemon=True)
        thread.start()

        with patch("rat_runner.callback.RATD_CALLBACK_URL", f"http://127.0.0.1:{port}"):
            notify_run_complete(run)

        thread.join(timeout=5)
        server.server_close()

        assert captured_data["status"] == "failed"
        assert captured_data["error"] == "DuckDB OOM"

    def test_handles_connection_failure_gracefully(self) -> None:
        """Should log warning but not raise on connection failure."""
        run = _make_terminal_run()

        # Point to a port that's not listening
        with patch("rat_runner.callback.RATD_CALLBACK_URL", "http://127.0.0.1:1"):
            # Should not raise — fire and forget
            notify_run_complete(run)

    def test_handles_http_error_gracefully(self) -> None:
        """Should log warning but not raise on HTTP 500."""
        run = _make_terminal_run()

        class ErrorHandler(BaseHTTPRequestHandler):
            def do_POST(self):
                self.send_response(500)
                self.end_headers()

            def log_message(self, *args):
                pass

        server = HTTPServer(("127.0.0.1", 0), ErrorHandler)
        port = server.server_address[1]
        thread = threading.Thread(target=server.handle_request, daemon=True)
        thread.start()

        with patch("rat_runner.callback.RATD_CALLBACK_URL", f"http://127.0.0.1:{port}"):
            # Should not raise
            notify_run_complete(run)

        thread.join(timeout=5)
        server.server_close()

    def test_strips_trailing_slash_from_url(self) -> None:
        """Should build correct URL even if RATD_CALLBACK_URL has trailing slash."""
        run = _make_terminal_run()
        captured_paths: list[str] = []

        class Handler(BaseHTTPRequestHandler):
            def do_POST(self):
                captured_paths.append(self.path)
                self.send_response(200)
                self.end_headers()

            def log_message(self, *args):
                pass

        server = HTTPServer(("127.0.0.1", 0), Handler)
        port = server.server_address[1]
        thread = threading.Thread(target=server.handle_request, daemon=True)
        thread.start()

        with patch("rat_runner.callback.RATD_CALLBACK_URL", f"http://127.0.0.1:{port}/"):
            notify_run_complete(run)

        thread.join(timeout=5)
        server.server_close()

        assert len(captured_paths) == 1
        # Should NOT have double slash
        assert "//" not in captured_paths[0].replace("//", "", 1)
        assert captured_paths[0] == "/api/v1/internal/runs/test-run-123/status"
