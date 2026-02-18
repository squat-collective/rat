"""Entrypoint for the query service: python -m rat_query."""

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
