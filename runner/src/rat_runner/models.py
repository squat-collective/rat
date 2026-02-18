"""Domain models for the runner service."""

from __future__ import annotations

import threading
import time
from collections import deque
from dataclasses import dataclass, field
from enum import Enum, StrEnum
from typing import Protocol, runtime_checkable


@runtime_checkable
class PipelineLogger(Protocol):
    """Structural protocol for pipeline loggers.

    Any object with info(), warn(), error(), and debug() methods satisfies this
    protocol. RunLogger is the primary implementation, but MagicMock and other
    duck-typed objects work too — no inheritance required.
    """

    def info(self, message: str) -> None: ...
    def warn(self, message: str) -> None: ...
    def error(self, message: str) -> None: ...
    def debug(self, message: str) -> None: ...


class RunStatus(Enum):
    """Status of a pipeline run."""

    PENDING = "pending"
    RUNNING = "running"
    SUCCESS = "success"
    FAILED = "failed"
    CANCELLED = "cancelled"

    def is_terminal(self) -> bool:
        return self in (RunStatus.SUCCESS, RunStatus.FAILED, RunStatus.CANCELLED)


@dataclass
class LogRecord:
    """A single log entry from a pipeline run."""

    timestamp: float  # time.time()
    level: str  # "info", "warn", "error", "debug"
    message: str


_MAX_LOG_ENTRIES = 10_000


@dataclass
class RunState:
    """Mutable state of a single pipeline run. Thread-safe via internal lock."""

    run_id: str
    namespace: str
    layer: str
    pipeline_name: str
    trigger: str
    status: RunStatus = RunStatus.PENDING
    rows_written: int = 0
    duration_ms: int = 0
    error: str = ""
    created_at: float = field(default_factory=time.time)
    branch: str = ""
    env: dict[str, str] = field(default_factory=dict)
    quality_results: list[QualityTestResult] = field(default_factory=list)
    archived_zones: list[str] = field(default_factory=list)
    cancel_event: threading.Event = field(default_factory=threading.Event)
    logs: deque[LogRecord] = field(default_factory=lambda: deque(maxlen=_MAX_LOG_ENTRIES))
    _lock: threading.Lock = field(default_factory=threading.Lock)
    _log_condition: threading.Condition = field(init=False)

    def __post_init__(self) -> None:
        # Condition wraps the existing lock so add_log + StreamLogs share state.
        self._log_condition = threading.Condition(self._lock)

    def add_log(self, level: str, message: str) -> None:
        """Append a log record (thread-safe) and wake any waiting StreamLogs consumers."""
        record = LogRecord(timestamp=time.time(), level=level, message=message)
        with self._log_condition:
            self.logs.append(record)
            self._log_condition.notify_all()

    def get_logs_from(self, cursor: int) -> list[LogRecord]:
        """Return log records from *cursor* onwards (thread-safe).

        Acquires the internal lock so callers never need to touch ``_lock``
        or ``_log_condition`` directly.  Returns an empty list when no new
        entries are available.
        """
        from itertools import islice

        with self._log_condition:
            return list(islice(self.logs, cursor, None))

    def wait_for_logs(self, timeout: float = 1.0) -> None:
        """Block until new logs are available or *timeout* seconds elapse.

        Intended for StreamLogs-style consumers that need to sleep until
        ``add_log`` signals new data.  Encapsulates the condition variable
        so callers don't access private synchronisation primitives.
        """
        with self._log_condition:
            self._log_condition.wait(timeout=timeout)

    def is_terminal(self) -> bool:
        return self.status.is_terminal()


class MergeStrategy(StrEnum):
    """Supported merge strategies for pipeline writes."""

    FULL_REFRESH = "full_refresh"
    INCREMENTAL = "incremental"
    APPEND_ONLY = "append_only"
    DELETE_INSERT = "delete_insert"
    SCD2 = "scd2"
    SNAPSHOT = "snapshot"

    @classmethod
    def validate(cls, value: str) -> bool:
        """Return True if value is a recognised merge strategy."""
        try:
            cls(value)
            return True
        except ValueError:
            return False


@dataclass(frozen=True)
class PartitionByEntry:
    """A single partition field: column name + transform (e.g., day, month, identity)."""

    column: str
    transform: str = "identity"  # identity, day, month, year, hour


# Valid Iceberg partition transforms we support.
VALID_PARTITION_TRANSFORMS: frozenset[str] = frozenset(
    {
        "identity",
        "day",
        "month",
        "year",
        "hour",
    }
)


@dataclass(frozen=True)
class PipelineConfig:
    """Parsed pipeline config.yaml."""

    description: str = ""
    materialized: str = "table"  # "table" or "view"
    unique_key: tuple[str, ...] = ()
    merge_strategy: MergeStrategy = MergeStrategy.FULL_REFRESH
    watermark_column: str = ""  # column for max() watermark reads (incremental)
    archive_landing_zones: bool = False  # move landing zone files to _processed/ after run
    partition_column: str = ""  # column for snapshot partitioning
    partition_by: tuple[PartitionByEntry, ...] = ()  # Iceberg partition spec entries
    scd_valid_from: str = "valid_from"  # SCD2 valid-from column name
    scd_valid_to: str = "valid_to"  # SCD2 valid-to column name
    # Retry configuration: when a pipeline fails, the runner can automatically
    # retry up to max_retries times with retry_delay_seconds between attempts.
    # Set max_retries=0 (default) to disable automatic retries.
    max_retries: int = 0
    retry_delay_seconds: int = 30


@dataclass(frozen=True)
class QualityTestResult:
    """Result of a single quality test execution."""

    test_name: str
    test_file: str  # S3 key
    severity: str  # "error" or "warn"
    status: str  # "pass", "fail", "error"
    row_count: int  # violations (0 = pass)
    message: str = ""
    duration_ms: int = 0
    description: str = ""  # parsed from -- @description: annotation
    compiled_sql: str = ""  # compiled SQL that was executed
    sample_rows: str = ""  # first N violation rows formatted as text
    tags: tuple[str, ...] = ()  # parsed from -- @tags: annotation
    remediation: str = ""  # parsed from -- @remediation: annotation
