#!/usr/bin/env python3
"""compact.py — invoked by the Go plugin to rewrite a single Iceberg table.

Reads the table via PyIceberg, then calls .overwrite() with the same data.
overwrite() materialises a single (or PyArrow-chunked) parquet file in a
new snapshot — effectively compacting the small-file tail.

Note: this is a poor-man's compaction. The old data files remain in S3
(unreferenced by the new snapshot) until snapshot expiration runs. The
caller should pair this with maintenance.expire_snapshots eventually.

Reads config from env (matches what the Go plugin passes through):
  NESSIE_URL       Nessie base URL (e.g. http://nessie:19120)
  S3_ENDPOINT      MinIO/S3 endpoint URL (e.g. http://minio:9000)
  S3_ACCESS_KEY    S3 credentials
  S3_SECRET_KEY
  S3_REGION        (default us-east-1)

Args: <namespace> <layer> <table_name>

Output: single JSON object on stdout — { ok, files_before, files_after,
size_before, size_after, duration_ms, error }. Exit non-zero on any error.
"""

from __future__ import annotations

import json
import os
import sys
import time
import traceback


def main() -> int:
    if len(sys.argv) != 4:
        emit({"ok": False, "error": "usage: compact.py <ns> <layer> <name>"})
        return 2

    ns, layer, name = sys.argv[1], sys.argv[2], sys.argv[3]
    t0 = time.time()

    try:
        from pyiceberg.catalog.rest import RestCatalog

        nessie = os.environ.get("NESSIE_URL", "http://nessie:19120").rstrip("/")
        # The Iceberg REST endpoint Nessie exposes lives under /iceberg/<branch>/.
        # We always target the main branch — compaction on a side branch would
        # only matter if RAT shipped per-environment branches at the table
        # level, which it does not.
        uri = f"{nessie}/iceberg/main/"

        cat = RestCatalog(
            "nessie",
            uri=uri,
            **{
                "s3.endpoint": os.environ.get("S3_ENDPOINT", "http://minio:9000"),
                "s3.access-key-id": os.environ.get("S3_ACCESS_KEY", "minioadmin"),
                "s3.secret-access-key": os.environ.get("S3_SECRET_KEY", "minioadmin"),
                "s3.region": os.environ.get("S3_REGION", "us-east-1"),
                "s3.path-style-access": "true",
            },
        )

        tbl = cat.load_table((ns, layer, name))

        files_before = list(tbl.scan().plan_files())
        size_before = sum(f.file.file_size_in_bytes for f in files_before)

        # If the table is already a single file, there's nothing to do.
        # The Go side filters by ratio first, so this is a defence-in-depth
        # guard for a manual /compact call on an already-tidy table.
        if len(files_before) <= 1:
            emit({
                "ok": True,
                "skipped": True,
                "reason": "already compact",
                "files_before": len(files_before),
                "files_after": len(files_before),
                "size_before": size_before,
                "size_after": size_before,
                "duration_ms": int((time.time() - t0) * 1000),
            })
            return 0

        data = tbl.scan().to_arrow()
        tbl.overwrite(data)
        tbl.refresh()

        files_after = list(tbl.scan().plan_files())
        size_after = sum(f.file.file_size_in_bytes for f in files_after)

        emit({
            "ok": True,
            "files_before": len(files_before),
            "files_after": len(files_after),
            "size_before": size_before,
            "size_after": size_after,
            "rows": data.num_rows,
            "duration_ms": int((time.time() - t0) * 1000),
        })
        return 0

    except Exception as e:
        emit({
            "ok": False,
            "error": f"{type(e).__name__}: {e}",
            "trace": traceback.format_exc(),
            "duration_ms": int((time.time() - t0) * 1000),
        })
        return 1


def emit(payload: dict) -> None:
    # Single-line JSON so the Go side can parse stdout deterministically.
    sys.stdout.write(json.dumps(payload) + "\n")
    sys.stdout.flush()


if __name__ == "__main__":
    sys.exit(main())
