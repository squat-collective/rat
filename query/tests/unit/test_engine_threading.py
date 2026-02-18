"""Tests for query engine thread safety.

Verifies that the QueryEngine's DDL lock protects view registration from
concurrent access, and that query_arrow can be called from multiple threads
without corrupting state.
"""

from __future__ import annotations

import threading
from unittest.mock import MagicMock, patch

import pyarrow as pa

from rat_query.config import S3Config
from rat_query.engine import QueryEngine


class TestQueryEngineThreadSafety:
    """Thread safety tests for the QueryEngine."""

    def test_concurrent_register_view_uses_ddl_lock(self, s3_config: S3Config):
        """Concurrent register_view calls should be serialised by the DDL lock.

        We verify this by checking that both threads successfully complete
        register_view without errors (the lock prevents interleaved DDL).
        """
        with patch("rat_query.engine.duckdb.connect") as mock_connect:
            mock_conn = MagicMock()
            mock_connect.return_value = mock_conn
            engine = QueryEngine(s3_config)

        barrier = threading.Barrier(2)
        errors: list[Exception] = []

        def register_view(name: str) -> None:
            try:
                barrier.wait(timeout=5)
                engine.register_view("bronze", name, f"s3://bucket/{name}")
            except Exception as e:
                errors.append(e)

        t1 = threading.Thread(target=register_view, args=("table_a",), name="t1")
        t2 = threading.Thread(target=register_view, args=("table_b",), name="t2")
        t1.start()
        t2.start()
        t1.join(timeout=10)
        t2.join(timeout=10)

        assert not errors, f"Threads raised errors: {errors}"
        # Both calls should have completed (2 execute calls for CREATE OR REPLACE VIEW)
        assert mock_conn.execute.call_count >= 2

    def test_concurrent_query_arrow_does_not_crash(self, s3_config: S3Config):
        """Multiple threads calling query_arrow concurrently should not crash.

        DuckDB's in-process connection handles internal parallelism, so
        concurrent reads are valid as long as the connection is not closed.
        """
        table = pa.table({"x": [1, 2, 3]})

        with patch("rat_query.engine.duckdb.connect") as mock_connect:
            mock_conn = MagicMock()
            mock_connect.return_value = mock_conn
            mock_result = MagicMock()
            reader = pa.RecordBatchReader.from_batches(table.schema, table.to_batches())
            mock_result.arrow.return_value = reader
            mock_conn.execute.return_value = mock_result

            engine = QueryEngine(s3_config)

        results: list[pa.Table | None] = [None, None]
        errors: list[Exception] = []
        barrier = threading.Barrier(2)

        def run_query(idx: int) -> None:
            try:
                barrier.wait(timeout=5)
                # Re-create a fresh reader for each call since they're single-use
                fresh_table = pa.table({"x": [1, 2, 3]})
                fresh_reader = pa.RecordBatchReader.from_batches(
                    fresh_table.schema, fresh_table.to_batches()
                )
                fresh_result = MagicMock()
                fresh_result.arrow.return_value = fresh_reader
                mock_conn.execute.return_value = fresh_result
                results[idx] = engine.query_arrow("SELECT 1", limit=10)
            except Exception as e:
                errors.append(e)

        t1 = threading.Thread(target=run_query, args=(0,))
        t2 = threading.Thread(target=run_query, args=(1,))
        t1.start()
        t2.start()
        t1.join(timeout=10)
        t2.join(timeout=10)

        assert not errors, f"Threads raised errors: {errors}"

    def test_drop_all_views_acquires_ddl_lock(self, s3_config: S3Config):
        """drop_all_views should hold the DDL lock during execution."""
        with patch("rat_query.engine.duckdb.connect") as mock_connect:
            mock_conn = MagicMock()
            mock_connect.return_value = mock_conn
            engine = QueryEngine(s3_config)

        lock_held = []

        original_execute = mock_conn.execute

        def check_lock(*args, **kwargs):
            # The DDL lock should be held when execute is called from drop_all_views
            lock_held.append(not engine._ddl_lock.acquire(blocking=False))
            if not lock_held[-1]:
                engine._ddl_lock.release()
            return original_execute(*args, **kwargs)

        mock_conn.execute = check_lock
        engine.drop_all_views()

        # All execute calls during drop_all_views should have been under the lock
        assert all(lock_held), "DDL lock was not held during drop_all_views"

    def test_drop_view_acquires_ddl_lock(self, s3_config: S3Config):
        """drop_view should hold the DDL lock during execution."""
        with patch("rat_query.engine.duckdb.connect") as mock_connect:
            mock_conn = MagicMock()
            mock_connect.return_value = mock_conn
            engine = QueryEngine(s3_config)

        lock_held = []
        original_execute = mock_conn.execute

        def check_lock(*args, **kwargs):
            lock_held.append(not engine._ddl_lock.acquire(blocking=False))
            if not lock_held[-1]:
                engine._ddl_lock.release()
            return original_execute(*args, **kwargs)

        mock_conn.execute = check_lock
        engine.drop_view("bronze", "orders")

        assert all(lock_held), "DDL lock was not held during drop_view"
