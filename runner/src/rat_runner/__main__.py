"""Entrypoint for the runner service: python -m rat_runner.

Modes:
  RUN_MODE=single  — Execute a single pipeline from env vars, print JSON result, exit.
  RUN_MODE=server   — Start the gRPC server (default).
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

if __name__ == "__main__":
    run_mode = os.environ.get("RUN_MODE", "server").lower()

    if run_mode == "single":
        from rat_runner.single_shot import run_single  # noqa: E402

        run_single()
    else:
        from rat_runner.server import serve  # noqa: E402

        port = int(os.environ.get("GRPC_PORT", "50052"))
        serve(port)
