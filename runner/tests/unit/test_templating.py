"""Tests for templating — SQL Jinja compilation with ref() resolution."""

from __future__ import annotations

import logging
from unittest.mock import patch

from rat_runner.config import NessieConfig, S3Config
from rat_runner.models import PipelineConfig
from rat_runner.templating import (
    _resolve_landing_zone_preview,
    compile_sql,
    extract_dependencies,
    extract_landing_zones,
    extract_metadata,
    metadata_to_config,
    validate_landing_zones,
    validate_template,
)


class TestExtractMetadata:
    def test_parses_metadata_headers(self):
        sql = """-- @description: Clean orders
-- @materialized: table
SELECT * FROM raw_orders
"""
        meta = extract_metadata(sql)
        assert meta == {"description": "Clean orders", "materialized": "table"}

    def test_stops_at_non_comment(self):
        sql = """-- @description: Pipeline
SELECT 1
-- @late: should not appear
"""
        meta = extract_metadata(sql)
        assert meta == {"description": "Pipeline"}

    def test_empty_sql(self):
        assert extract_metadata("") == {}

    def test_no_metadata(self):
        assert extract_metadata("SELECT 1") == {}

    def test_parses_python_style_metadata(self):
        source = """# @description: Ingest CSV
# @merge_strategy: incremental
# @unique_key: id

import duckdb
"""
        meta = extract_metadata(source)
        assert meta == {
            "description": "Ingest CSV",
            "merge_strategy": "incremental",
            "unique_key": "id",
        }

    def test_python_stops_at_non_comment(self):
        source = """# @description: Pipeline
import duckdb
# @late: should not appear
"""
        meta = extract_metadata(source)
        assert meta == {"description": "Pipeline"}

    def test_python_stops_at_docstring(self):
        source = '''# @description: Pipeline
"""This is a docstring."""
# @late: should not appear
'''
        meta = extract_metadata(source)
        assert meta == {"description": "Pipeline"}


class TestMetadataToConfig:
    def test_basic_config(self):
        meta = {"description": "Clean orders", "merge_strategy": "incremental"}
        config = metadata_to_config(meta)
        assert config.description == "Clean orders"
        assert config.merge_strategy == "incremental"
        assert config.unique_key == ()
        assert config.materialized == "table"

    def test_csv_unique_key(self):
        meta = {"unique_key": "id, updated_at"}
        config = metadata_to_config(meta)
        assert config.unique_key == ("id", "updated_at")

    def test_defaults(self):
        config = metadata_to_config({})
        assert config.description == ""
        assert config.materialized == "table"
        assert config.unique_key == ()
        assert config.merge_strategy == "full_refresh"
        assert config.watermark_column == ""

    def test_all_fields(self):
        meta = {
            "description": "Full pipeline",
            "materialized": "view",
            "unique_key": "order_id",
            "merge_strategy": "incremental",
            "watermark_column": "updated_at",
        }
        config = metadata_to_config(meta)
        assert config.description == "Full pipeline"
        assert config.materialized == "view"
        assert config.unique_key == ("order_id",)
        assert config.merge_strategy == "incremental"
        assert config.watermark_column == "updated_at"

    def test_archive_landing_zones_true(self):
        meta = {"archive_landing_zones": "true"}
        config = metadata_to_config(meta)
        assert config.archive_landing_zones is True

    def test_archive_landing_zones_false(self):
        meta = {"archive_landing_zones": "false"}
        config = metadata_to_config(meta)
        assert config.archive_landing_zones is False

    def test_archive_landing_zones_default(self):
        config = metadata_to_config({})
        assert config.archive_landing_zones is False

    def test_partition_column(self):
        meta = {"partition_column": "date"}
        config = metadata_to_config(meta)
        assert config.partition_column == "date"

    def test_scd_columns(self):
        meta = {"scd_valid_from": "start_ts", "scd_valid_to": "end_ts"}
        config = metadata_to_config(meta)
        assert config.scd_valid_from == "start_ts"
        assert config.scd_valid_to == "end_ts"

    def test_scd_columns_default(self):
        config = metadata_to_config({})
        assert config.scd_valid_from == "valid_from"
        assert config.scd_valid_to == "valid_to"

    def test_new_strategies(self):
        for strategy in ("append_only", "delete_insert", "scd2", "snapshot"):
            config = metadata_to_config({"merge_strategy": strategy})
            assert config.merge_strategy == strategy


class TestExtractDependencies:
    def test_finds_single_ref(self):
        sql = "SELECT * FROM {{ ref('bronze.orders') }}"
        deps = extract_dependencies(sql)
        assert deps == ["bronze.orders"]

    def test_finds_multiple_refs(self):
        sql = """
        SELECT o.*, c.name
        FROM {{ ref('silver.orders') }} o
        JOIN {{ ref('silver.customers') }} c ON o.customer_id = c.id
        """
        deps = extract_dependencies(sql)
        assert deps == ["silver.orders", "silver.customers"]

    def test_handles_double_quotes(self):
        sql = 'SELECT * FROM {{ ref("bronze.events") }}'
        deps = extract_dependencies(sql)
        assert deps == ["bronze.events"]

    def test_no_refs(self):
        assert extract_dependencies("SELECT 1") == []


class TestCompileSql:
    def _s3(self) -> S3Config:
        return S3Config(endpoint="minio:9000", bucket="test-bucket")

    def _nessie(self) -> NessieConfig:
        return NessieConfig(url="http://nessie:19120/api/v1")

    def test_replaces_ref_with_iceberg_scan(self):
        sql = "SELECT * FROM {{ ref('bronze.orders') }}"
        result = compile_sql(sql, "myns", "silver", "clean_orders", self._s3(), self._nessie())
        assert "iceberg_scan(" in result
        assert "s3://test-bucket/myns/bronze/orders/" in result
        assert "allow_moved_paths" in result

    def test_injects_this(self):
        sql = "SELECT * FROM {{ this }}"
        result = compile_sql(sql, "myns", "silver", "orders", self._s3(), self._nessie())
        assert "iceberg_scan(" in result

    def test_injects_run_started_at(self):
        sql = "SELECT '{{ run_started_at }}' AS ts"
        result = compile_sql(sql, "ns", "silver", "p", self._s3(), self._nessie())
        # Should contain an ISO timestamp
        assert "T" in result  # ISO format has T separator

    def test_is_incremental_returns_false(self):
        sql = "{% if is_incremental() %}WHERE 1{% else %}WHERE 2{% endif %}"
        result = compile_sql(sql, "ns", "silver", "p", self._s3(), self._nessie())
        assert "WHERE 2" in result
        assert "WHERE 1" not in result

    def test_strips_metadata_comments(self):
        sql = """-- @description: test
-- @materialized: table
SELECT 1"""
        result = compile_sql(sql, "ns", "silver", "p", self._s3(), self._nessie())
        assert "@description" not in result
        assert "@materialized" not in result
        assert "SELECT 1" in result

    def test_three_part_ref_cross_namespace(self):
        sql = "SELECT * FROM {{ ref('other_ns.bronze.events') }}"
        result = compile_sql(sql, "myns", "silver", "p", self._s3(), self._nessie())
        assert "iceberg_scan(" in result
        assert "s3://test-bucket/other_ns/bronze/events/" in result

    def test_invalid_ref_raises(self):
        sql = "SELECT * FROM {{ ref('just_a_name') }}"
        try:
            compile_sql(sql, "ns", "silver", "p", self._s3(), self._nessie())
            assert False, "Should have raised"
        except ValueError as e:
            assert "Invalid ref" in str(e)

    def test_is_incremental_true_with_config(self):
        config = PipelineConfig(merge_strategy="incremental")
        sql = "{% if is_incremental() %}WHERE 1{% else %}WHERE 2{% endif %}"
        result = compile_sql(sql, "ns", "silver", "p", self._s3(), self._nessie(), config=config)
        assert "WHERE 1" in result
        assert "WHERE 2" not in result

    def test_watermark_value_available(self):
        config = PipelineConfig(merge_strategy="incremental", watermark_column="ts")
        sql = "SELECT * FROM raw WHERE ts > '{{ watermark_value }}'"
        result = compile_sql(
            sql,
            "ns",
            "silver",
            "p",
            self._s3(),
            self._nessie(),
            config=config,
            watermark_value="2024-01-01",
        )
        assert "2024-01-01" in result

    def test_backward_compat_no_config(self):
        """compile_sql without config/watermark still works (backward compat)."""
        sql = "{% if is_incremental() %}INC{% else %}FULL{% endif %}"
        result = compile_sql(sql, "ns", "silver", "p", self._s3(), self._nessie())
        assert "FULL" in result
        assert "INC" not in result

    def test_landing_zone_resolves_to_s3_path(self):
        sql = "SELECT * FROM read_csv_auto('{{ landing_zone('uploads') }}')"
        result = compile_sql(sql, "myns", "bronze", "ingest", self._s3(), self._nessie())
        assert "s3://test-bucket/myns/landing/uploads/**" in result

    def test_landing_zone_and_ref_together(self):
        sql = """SELECT * FROM read_csv_auto('{{ landing_zone('raw_files') }}')
UNION ALL
SELECT * FROM {{ ref('bronze.orders') }}"""
        result = compile_sql(sql, "myns", "silver", "combined", self._s3(), self._nessie())
        assert "s3://test-bucket/myns/landing/raw_files/**" in result
        assert "iceberg_scan(" in result
        assert "s3://test-bucket/myns/bronze/orders/" in result


class TestResolveRefCatalogLookup:
    """Tests for ref() resolution via Nessie catalog metadata lookup."""

    def _s3(self) -> S3Config:
        return S3Config(endpoint="minio:9000", bucket="test-bucket")

    def _nessie(self) -> NessieConfig:
        return NessieConfig(url="http://nessie:19120/api/v1")

    @patch("rat_runner.iceberg.get_catalog")
    def test_ref_uses_catalog_metadata_location(self, mock_get_catalog):
        """When catalog is reachable, ref() uses the exact metadata file path."""
        mock_table = mock_get_catalog.return_value.load_table.return_value
        mock_table.metadata_location = (
            "s3://test-bucket/myns/bronze/orders/metadata/00003-abc.metadata.json"
        )

        sql = "SELECT * FROM {{ ref('bronze.orders') }}"
        result = compile_sql(sql, "myns", "silver", "p", self._s3(), self._nessie())

        assert "iceberg_scan(" in result
        assert "00003-abc.metadata.json" in result
        assert "allow_moved_paths" not in result
        mock_get_catalog.return_value.load_table.assert_any_call("myns.bronze.orders")

    @patch("rat_runner.iceberg.get_catalog")
    def test_ref_falls_back_on_catalog_error(self, mock_get_catalog):
        """When catalog is unreachable, ref() falls back to directory path."""
        mock_get_catalog.side_effect = ConnectionError("catalog unavailable")

        sql = "SELECT * FROM {{ ref('bronze.orders') }}"
        result = compile_sql(sql, "myns", "silver", "p", self._s3(), self._nessie())

        assert "iceberg_scan(" in result
        assert "s3://test-bucket/myns/bronze/orders/" in result

    @patch("rat_runner.iceberg.get_catalog")
    def test_ref_fallback_logs_warning(self, mock_get_catalog, caplog):
        """When catalog lookup fails, a warning is logged with table ref and exception."""
        mock_get_catalog.side_effect = ConnectionError("catalog unavailable")

        sql = "SELECT * FROM {{ ref('bronze.orders') }}"
        with caplog.at_level(logging.WARNING, logger="rat_runner.templating"):
            compile_sql(sql, "myns", "silver", "p", self._s3(), self._nessie())

        warning_messages = [r.message for r in caplog.records if r.levelno == logging.WARNING]
        assert any(
            "bronze.orders" in msg and "catalog unavailable" in msg for msg in warning_messages
        )

    @patch("rat_runner.iceberg.get_catalog")
    def test_ref_cross_namespace_uses_catalog(self, mock_get_catalog):
        """Three-part ref resolves through catalog with correct table name."""
        mock_table = mock_get_catalog.return_value.load_table.return_value
        mock_table.metadata_location = (
            "s3://test-bucket/other/gold/summary/metadata/00001-xyz.metadata.json"
        )

        sql = "SELECT * FROM {{ ref('other.gold.summary') }}"
        result = compile_sql(sql, "myns", "silver", "p", self._s3(), self._nessie())

        assert "00001-xyz.metadata.json" in result
        mock_get_catalog.return_value.load_table.assert_any_call("other.gold.summary")

    @patch("rat_runner.iceberg.get_catalog")
    def test_ref_escapes_metadata_location_with_quotes(self, mock_get_catalog):
        """Metadata locations with single quotes are escaped to prevent SQL injection."""
        mock_table = mock_get_catalog.return_value.load_table.return_value
        mock_table.metadata_location = (
            "s3://test-bucket/myns/bronze/o'rders/metadata/00001.metadata.json"
        )

        sql = "SELECT * FROM {{ ref('bronze.orders') }}"
        result = compile_sql(sql, "myns", "silver", "p", self._s3(), self._nessie())

        assert "iceberg_scan(" in result
        # Single quote should be doubled for SQL safety
        assert "o''rders" in result
        assert "o'rders" not in result.replace("o''rders", "")

    @patch("rat_runner.iceberg.get_catalog")
    def test_ref_fallback_uses_allow_moved_paths(self, mock_get_catalog):
        """Fallback path uses allow_moved_paths so DuckDB can find version-hint.text."""
        mock_get_catalog.return_value.load_table.side_effect = Exception("table not found")

        sql = "SELECT * FROM {{ ref('bronze.orders') }}"
        result = compile_sql(sql, "myns", "silver", "p", self._s3(), self._nessie())

        assert "iceberg_scan(" in result
        assert "s3://test-bucket/myns/bronze/orders/" in result
        assert "allow_moved_paths" in result
        # Should NOT contain read_parquet (migrated away from it)
        assert "read_parquet" not in result

    @patch("rat_runner.iceberg.get_catalog")
    def test_this_uses_catalog_metadata_when_available(self, mock_get_catalog):
        """The 'this' variable also resolves via catalog metadata lookup."""
        mock_table = mock_get_catalog.return_value.load_table.return_value
        mock_table.metadata_location = (
            "s3://test-bucket/myns/silver/orders/metadata/00005-def.metadata.json"
        )

        sql = "SELECT * FROM {{ this }}"
        result = compile_sql(sql, "myns", "silver", "orders", self._s3(), self._nessie())

        assert "iceberg_scan(" in result
        assert "00005-def.metadata.json" in result
        mock_get_catalog.return_value.load_table.assert_any_call("myns.silver.orders")


class TestExtractLandingZones:
    def test_single_zone(self):
        sql = "SELECT * FROM read_csv_auto('{{ landing_zone('uploads') }}/*.csv')"
        zones = extract_landing_zones(sql)
        assert zones == ["uploads"]

    def test_multiple_zones(self):
        sql = """{{ landing_zone('uploads') }} and {{ landing_zone('raw_files') }}"""
        zones = extract_landing_zones(sql)
        assert zones == ["uploads", "raw_files"]

    def test_double_quotes(self):
        sql = '{{ landing_zone("my_zone") }}'
        zones = extract_landing_zones(sql)
        assert zones == ["my_zone"]

    def test_no_zones(self):
        assert extract_landing_zones("SELECT 1") == []


class TestValidateLandingZones:
    def _s3(self) -> S3Config:
        return S3Config(endpoint="minio:9000", bucket="test-bucket")

    @patch("rat_runner.config.list_s3_keys", return_value=[])
    def test_warns_on_empty_zone(self, mock_list):
        sql = "SELECT * FROM read_csv_auto('{{ landing_zone('uploads') }}/*.csv')"
        warnings = validate_landing_zones(sql, "myns", self._s3())
        assert len(warnings) == 1
        assert "uploads" in warnings[0]
        assert "no files" in warnings[0]
        mock_list.assert_called_once_with(self._s3(), "myns/landing/uploads/")

    @patch(
        "rat_runner.config.list_s3_keys",
        return_value=["myns/landing/uploads/file1.csv"],
    )
    def test_no_warning_when_files_exist(self, mock_list):
        sql = "SELECT * FROM read_csv_auto('{{ landing_zone('uploads') }}/*.csv')"
        warnings = validate_landing_zones(sql, "myns", self._s3())
        assert warnings == []

    @patch("rat_runner.config.list_s3_keys")
    def test_no_zones_no_warnings(self, mock_list):
        sql = "SELECT 1"
        warnings = validate_landing_zones(sql, "myns", self._s3())
        assert warnings == []
        mock_list.assert_not_called()


class TestResolveLandingZonePreview:
    def _s3(self) -> S3Config:
        return S3Config(endpoint="minio:9000", bucket="test-bucket")

    @patch(
        "rat_runner.config.list_s3_keys",
        return_value=["myns/landing/orders/_samples/sample.csv"],
    )
    def test_uses_samples_when_present(self, mock_list):
        warnings: list[str] = []
        result = _resolve_landing_zone_preview("orders", "myns", self._s3(), warnings)
        assert result == "s3://test-bucket/myns/landing/orders/_samples/**"
        assert warnings == []
        mock_list.assert_called_once_with(self._s3(), "myns/landing/orders/_samples/")

    @patch("rat_runner.config.list_s3_keys", return_value=[])
    def test_falls_back_when_no_samples(self, mock_list):
        warnings: list[str] = []
        result = _resolve_landing_zone_preview("orders", "myns", self._s3(), warnings)
        assert result == "s3://test-bucket/myns/landing/orders/**"
        assert len(warnings) == 1
        assert "No sample files" in warnings[0]
        assert "_samples/" in warnings[0]


class TestCompileSqlLandingZoneFn:
    def _s3(self) -> S3Config:
        return S3Config(endpoint="minio:9000", bucket="test-bucket")

    def _nessie(self) -> NessieConfig:
        return NessieConfig(url="http://nessie:19120/api/v1")

    def test_custom_landing_zone_fn_overrides_default(self):
        sql = "SELECT * FROM read_csv_auto('{{ landing_zone('uploads') }}')"
        custom_path = "s3://custom-bucket/custom/_samples/**"
        result = compile_sql(
            sql,
            "myns",
            "bronze",
            "ingest",
            self._s3(),
            self._nessie(),
            landing_zone_fn=lambda name: custom_path,
        )
        assert custom_path in result
        assert "test-bucket" not in result

    def test_without_custom_fn_uses_default(self):
        sql = "SELECT * FROM read_csv_auto('{{ landing_zone('uploads') }}')"
        result = compile_sql(
            sql,
            "myns",
            "bronze",
            "ingest",
            self._s3(),
            self._nessie(),
        )
        assert "s3://test-bucket/myns/landing/uploads/**" in result


class TestValidateTemplate:
    def test_valid_sql_passes(self):
        sql = "SELECT * FROM {{ ref('bronze.orders') }} WHERE id > 0"
        errors, warnings = validate_template(sql)
        assert errors == []
        assert warnings == []

    def test_syntax_error_detected(self):
        sql = "SELECT * FROM {{ ref('bronze.orders') }"
        errors, warnings = validate_template(sql)
        assert len(errors) == 1
        assert "syntax error" in errors[0].lower() or "Jinja" in errors[0]

    def test_ref_inside_jinja_antipattern(self):
        sql = "SELECT * FROM {{ ref('{{this}}') }}"
        errors, warnings = validate_template(sql)
        assert len(errors) == 1
        assert "Nested Jinja" in errors[0]

    def test_bare_ref_outside_jinja(self):
        sql = "SELECT * FROM ref('bronze.orders')"
        errors, warnings = validate_template(sql)
        assert len(warnings) == 1
        assert "Bare function call" in warnings[0]

    def test_valid_ref_no_warning(self):
        sql = "SELECT * FROM {{ ref('silver.customers') }}"
        errors, warnings = validate_template(sql)
        assert errors == []
        assert warnings == []


class TestValidateTemplateEdgeCases:
    """Tests for validate_template handling of SQL comments and Jinja blocks."""

    def test_bare_ref_in_sql_line_comment_not_warned(self):
        """A ref() inside a SQL line comment (--) should not trigger a warning."""
        sql = "-- ref('bronze.orders') is the source\nSELECT * FROM {{ ref('bronze.orders') }}"
        errors, warnings = validate_template(sql)
        assert errors == []
        assert warnings == []

    def test_bare_ref_in_sql_block_comment_not_warned(self):
        """A ref() inside a SQL block comment (/* */) should not trigger a warning."""
        sql = "/* ref('bronze.orders') */ SELECT * FROM {{ ref('bronze.orders') }}"
        errors, warnings = validate_template(sql)
        assert errors == []
        assert warnings == []

    def test_bare_ref_inside_jinja_block_not_warned(self):
        """A ref() inside a {% %} Jinja block should not trigger a bare function warning."""
        sql = "{% if is_incremental() %}SELECT * FROM {{ ref('bronze.orders') }}{% endif %}"
        errors, warnings = validate_template(sql)
        assert errors == []
        assert warnings == []

    def test_truly_bare_ref_still_warned(self):
        """A ref() that is genuinely outside all Jinja delimiters and comments should warn."""
        sql = "SELECT * FROM ref('bronze.orders') WHERE 1=1"
        errors, warnings = validate_template(sql)
        assert len(warnings) == 1
        assert "Bare function call" in warnings[0]

    def test_bare_landing_zone_in_comment_not_warned(self):
        """landing_zone() inside a SQL comment should not trigger a warning."""
        sql = "-- landing_zone('uploads') is a source\nSELECT * FROM {{ landing_zone('uploads') }}"
        errors, warnings = validate_template(sql)
        assert errors == []
        assert warnings == []

    def test_bare_ref_in_multiline_block_comment_not_warned(self):
        """A ref() inside a multiline block comment should not trigger a warning."""
        sql = """/*
  This pipeline reads from ref('bronze.orders')
  and produces silver output.
*/
SELECT * FROM {{ ref('bronze.orders') }}"""
        errors, warnings = validate_template(sql)
        assert errors == []
        assert warnings == []


class TestNewJinjaHelpers:
    def _s3(self) -> S3Config:
        return S3Config(endpoint="minio:9000", bucket="test-bucket")

    def _nessie(self) -> NessieConfig:
        return NessieConfig(url="http://nessie:19120/api/v1")

    def test_is_scd2_true(self):
        config = PipelineConfig(merge_strategy="scd2")
        sql = "{% if is_scd2() %}SCD2{% else %}OTHER{% endif %}"
        result = compile_sql(sql, "ns", "silver", "p", self._s3(), self._nessie(), config=config)
        assert "SCD2" in result
        assert "OTHER" not in result

    def test_is_scd2_false(self):
        config = PipelineConfig(merge_strategy="full_refresh")
        sql = "{% if is_scd2() %}SCD2{% else %}OTHER{% endif %}"
        result = compile_sql(sql, "ns", "silver", "p", self._s3(), self._nessie(), config=config)
        assert "OTHER" in result

    def test_is_snapshot_true(self):
        config = PipelineConfig(merge_strategy="snapshot")
        sql = "{% if is_snapshot() %}SNAP{% else %}OTHER{% endif %}"
        result = compile_sql(sql, "ns", "silver", "p", self._s3(), self._nessie(), config=config)
        assert "SNAP" in result

    def test_is_append_only_true(self):
        config = PipelineConfig(merge_strategy="append_only")
        sql = "{% if is_append_only() %}APPEND{% else %}OTHER{% endif %}"
        result = compile_sql(sql, "ns", "silver", "p", self._s3(), self._nessie(), config=config)
        assert "APPEND" in result

    def test_is_delete_insert_true(self):
        config = PipelineConfig(merge_strategy="delete_insert")
        sql = "{% if is_delete_insert() %}DI{% else %}OTHER{% endif %}"
        result = compile_sql(sql, "ns", "silver", "p", self._s3(), self._nessie(), config=config)
        assert "DI" in result

    def test_helpers_false_without_config(self):
        sql = "{% if is_scd2() %}YES{% else %}NO{% endif %}"
        result = compile_sql(sql, "ns", "silver", "p", self._s3(), self._nessie())
        assert "NO" in result
