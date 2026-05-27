"""HTTP / REST API source connector.

Fetches JSON from an HTTP endpoint and returns it as a PyArrow table.
Demonstrates the ``rat.sources`` extension point — a source connector pulls
data from a system outside S3/Iceberg.

NOTE: as of this writing the runner executor does not yet *invoke* source
connectors — there is no pipeline-side mechanism to declare "use source X".
This plugin is therefore a protocol-complete **example** of the ``rat.sources``
contract: it is discovered by the plugin registry and fully unit-tested, but
cannot yet run as part of a pipeline. It documents the intended shape for when
the executor gains source support.

Config keys (the ``config`` dict passed to ``fetch``):
    url          (required) — the endpoint to GET
    headers      (optional) — dict of request headers
    record_path  (optional) — dot-path to the list of records inside the JSON
                              response (e.g. "data.items"). When omitted, the
                              response itself is expected to be a JSON array.
    timeout      (optional) — request timeout in seconds (default 30)
"""

from __future__ import annotations

import json
import urllib.request
from typing import TYPE_CHECKING

import pyarrow as pa

if TYPE_CHECKING:
    from rat_runner.config import S3Config


class HttpSource:
    """Source connector that fetches JSON records from an HTTP endpoint."""

    @property
    def name(self) -> str:
        return "http"

    def fetch(
        self,
        config: dict[str, object],
        s3_config: S3Config,
    ) -> pa.Table:
        """Fetch JSON from ``config['url']`` and return it as an Arrow table."""
        url = config.get("url")
        if not isinstance(url, str) or not url:
            raise ValueError("http source requires a non-empty string 'url' in config")

        headers = config.get("headers") or {}
        if not isinstance(headers, dict):
            raise ValueError("http source 'headers' must be a mapping")
        timeout = float(config.get("timeout", 30))  # type: ignore[arg-type]

        req = urllib.request.Request(url, headers=headers)  # noqa: S310 — example plugin
        with urllib.request.urlopen(req, timeout=timeout) as resp:  # noqa: S310
            payload = json.loads(resp.read().decode("utf-8"))

        records = extract_records(payload, config.get("record_path"))
        if not records:
            return pa.table({})
        return pa.Table.from_pylist(records)


def extract_records(payload: object, record_path: object) -> list[dict]:
    """Navigate to the list of records inside a JSON payload.

    ``record_path`` is an optional dot-path (e.g. "data.items"). A single
    object is wrapped as a one-row list; a list is returned as-is.
    """
    node = payload
    if isinstance(record_path, str) and record_path:
        for part in record_path.split("."):
            if not isinstance(node, dict):
                raise ValueError(
                    f"record_path '{record_path}' does not resolve inside the response"
                )
            node = node.get(part)

    if isinstance(node, dict):
        return [node]
    if not isinstance(node, list):
        raise ValueError("HTTP source expected a JSON array (or object) of records")
    return [row for row in node if isinstance(row, dict)]
