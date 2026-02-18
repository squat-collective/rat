"""Tests for state_dir — crash recovery marker file management."""

from __future__ import annotations

import json
from pathlib import Path

import pytest

from rat_runner.state_dir import (
    CrashedRun,
    collect_crashed_runs,
    get_state_dir,
    remove_marker,
    write_marker,
)


class TestWriteMarker:
    def test_creates_json_file(self, tmp_path: Path):
        write_marker(tmp_path, "run-123", "prod", "silver", "orders", "manual")

        marker = tmp_path / "run-123.json"
        assert marker.exists()

        data = json.loads(marker.read_text(encoding="utf-8"))
        assert data == {
            "run_id": "run-123",
            "namespace": "prod",
            "layer": "silver",
            "pipeline_name": "orders",
            "trigger": "manual",
        }

    def test_overwrites_existing_marker(self, tmp_path: Path):
        write_marker(tmp_path, "run-1", "ns1", "bronze", "old", "manual")
        write_marker(tmp_path, "run-1", "ns2", "gold", "new", "schedule:daily")

        data = json.loads((tmp_path / "run-1.json").read_text(encoding="utf-8"))
        assert data["namespace"] == "ns2"
        assert data["pipeline_name"] == "new"


class TestRemoveMarker:
    def test_removes_existing_file(self, tmp_path: Path):
        write_marker(tmp_path, "run-42", "ns", "silver", "p", "manual")
        assert (tmp_path / "run-42.json").exists()

        remove_marker(tmp_path, "run-42")
        assert not (tmp_path / "run-42.json").exists()

    def test_ignores_missing_file(self, tmp_path: Path):
        """Removing a non-existent marker should not raise."""
        remove_marker(tmp_path, "nonexistent")  # should not raise


class TestCollectCrashedRuns:
    def test_returns_empty_when_no_markers(self, tmp_path: Path):
        result = collect_crashed_runs(tmp_path)
        assert result == []

    def test_returns_empty_when_dir_does_not_exist(self, tmp_path: Path):
        nonexistent = tmp_path / "nope"
        result = collect_crashed_runs(nonexistent)
        assert result == []

    def test_collects_single_marker(self, tmp_path: Path):
        write_marker(tmp_path, "run-1", "prod", "silver", "orders", "manual")

        result = collect_crashed_runs(tmp_path)

        assert len(result) == 1
        assert result[0] == CrashedRun(
            run_id="run-1",
            namespace="prod",
            layer="silver",
            pipeline_name="orders",
            trigger="manual",
        )

    def test_collects_multiple_markers_sorted(self, tmp_path: Path):
        write_marker(tmp_path, "b-run", "ns", "gold", "report", "schedule:daily")
        write_marker(tmp_path, "a-run", "ns", "bronze", "ingest", "manual")

        result = collect_crashed_runs(tmp_path)

        assert len(result) == 2
        # Sorted by filename (a-run.json < b-run.json)
        assert result[0].run_id == "a-run"
        assert result[1].run_id == "b-run"

    def test_removes_marker_files_after_collection(self, tmp_path: Path):
        write_marker(tmp_path, "run-1", "ns", "silver", "p", "manual")
        write_marker(tmp_path, "run-2", "ns", "bronze", "q", "manual")

        collect_crashed_runs(tmp_path)

        remaining = list(tmp_path.glob("*.json"))
        assert len(remaining) == 0

    def test_skips_corrupt_json(self, tmp_path: Path):
        write_marker(tmp_path, "good-run", "ns", "silver", "p", "manual")
        (tmp_path / "corrupt.json").write_text("{invalid", encoding="utf-8")

        result = collect_crashed_runs(tmp_path)

        assert len(result) == 1
        assert result[0].run_id == "good-run"
        # Corrupt file should also be removed
        assert not (tmp_path / "corrupt.json").exists()

    def test_skips_json_with_missing_keys(self, tmp_path: Path):
        write_marker(tmp_path, "good-run", "ns", "silver", "p", "manual")
        (tmp_path / "incomplete.json").write_text(json.dumps({"run_id": "x"}), encoding="utf-8")

        result = collect_crashed_runs(tmp_path)

        assert len(result) == 1
        assert result[0].run_id == "good-run"

    def test_ignores_non_json_files(self, tmp_path: Path):
        write_marker(tmp_path, "run-1", "ns", "silver", "p", "manual")
        (tmp_path / "notes.txt").write_text("not a marker", encoding="utf-8")

        result = collect_crashed_runs(tmp_path)

        assert len(result) == 1
        # .txt file should remain untouched
        assert (tmp_path / "notes.txt").exists()


class TestGetStateDir:
    def test_returns_default_when_env_unset(self, monkeypatch: pytest.MonkeyPatch):
        monkeypatch.delenv("RUNNER_STATE_DIR", raising=False)
        result = get_state_dir()
        assert result == Path("/tmp/rat-runner-state")

    def test_returns_custom_when_env_set(self, tmp_path: Path, monkeypatch: pytest.MonkeyPatch):
        custom = tmp_path / "custom-state"
        monkeypatch.setenv("RUNNER_STATE_DIR", str(custom))
        result = get_state_dir()
        assert result == custom
        assert result.exists()
