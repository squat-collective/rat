"""Entrypoint for the query service: python -m rat_query.

Environment variables (see also rat_query.config):
    GRPC_PORT              gRPC listen port (default: 50051)
    GRPC_TLS_CERT/KEY      optional TLS cert/key file paths (mutually required)
    S3_ENDPOINT, S3_ACCESS_KEY, S3_SECRET_KEY, S3_BUCKET, S3_USE_SSL,
    S3_SESSION_TOKEN, S3_REGION
                           S3/MinIO connection for Iceberg reads
    NESSIE_URL             Nessie catalog URL (default: http://nessie:19120/api/v1)
    DUCKDB_MEMORY_LIMIT    DuckDB memory cap (default: 2GB)
    DUCKDB_THREADS         DuckDB worker threads (default: 4)
    QUERY_TIMEOUT_SECS     Per-query timeout in seconds; a watchdog calls
                           conn.interrupt() once the deadline passes and the
                           caller gets a DEADLINE_EXCEEDED status (default: 60)
    POSTGRES_USERDATA_URL  Optional federated user-data Postgres URL
    POSTGRES_USERDATA_ALIAS DuckDB alias for the attached Postgres (default: userdata)
"""

import logging
import os
import sys
from pathlib import Path

# Add gen/ directory to sys.path so generated proto stubs can use bare imports
# (e.g., `from common.v1 import common_pb2`)
_gen_dir = Path(__file__).parent / "gen"
if str(_gen_dir) not in sys.path:
    sys.path.insert(0, str(_gen_dir))

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s %(levelname)s [%(name)s] %(message)s",
    datefmt="%Y-%m-%d %H:%M:%S",
)

from rat_query.server import serve  # noqa: E402

if __name__ == "__main__":
    port = int(os.environ.get("GRPC_PORT", "50051"))
    serve(port)
