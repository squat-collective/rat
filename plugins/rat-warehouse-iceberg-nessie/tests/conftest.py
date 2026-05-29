"""Put the generated proto stubs on sys.path (bare imports like `warehouse.v1`)."""

import sys
from pathlib import Path

_gen = Path(__file__).parent.parent / "src" / "rat_warehouse_iceberg" / "gen"
if str(_gen) not in sys.path:
    sys.path.insert(0, str(_gen))
