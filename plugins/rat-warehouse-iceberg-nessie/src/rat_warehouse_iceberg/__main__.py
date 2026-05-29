"""Entrypoint for the iceberg-nessie warehouse plugin: python -m rat_warehouse_iceberg.

Serves warehouse/v1 (ADR-024) over gRPC, backed by Iceberg + Nessie. Config comes
from the standard S3_*/NESSIE_* env vars (the warehouse owns its storage config).
"""

from __future__ import annotations

import logging
import os
import sys
from concurrent import futures
from pathlib import Path

# Add gen/ to sys.path so generated proto stubs can use bare imports
# (e.g. `from warehouse.v1 import warehouse_pb2`, `from common.v1 import ...`).
_gen_dir = Path(__file__).parent / "gen"
if str(_gen_dir) not in sys.path:
    sys.path.insert(0, str(_gen_dir))

import grpc  # noqa: E402
from warehouse.v1 import warehouse_pb2_grpc  # type: ignore[import-untyped]  # noqa: E402

from rat_warehouse_iceberg.config import NessieConfig, S3Config  # noqa: E402
from rat_warehouse_iceberg.server import WarehouseServicer  # noqa: E402
from rat_warehouse_iceberg.warehouse import IcebergNessieWarehouse  # noqa: E402

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger("rat_warehouse_iceberg")


def main() -> None:
    port = os.environ.get("GRPC_PORT", "50080")
    wh = IcebergNessieWarehouse(S3Config.from_env(), NessieConfig.from_env())

    server = grpc.server(futures.ThreadPoolExecutor(max_workers=8))
    warehouse_pb2_grpc.add_WarehouseServiceServicer_to_server(WarehouseServicer(wh), server)
    server.add_insecure_port(f"0.0.0.0:{port}")
    server.start()
    logger.info("warehouse %s serving warehouse/v1 on :%s", wh.name, port)
    server.wait_for_termination()


if __name__ == "__main__":
    main()
