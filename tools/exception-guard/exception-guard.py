#!/usr/bin/env python3
"""exception-guard.py — thin CLI entrypoint.

The actual logic lives in exception_guard.py (underscore — importable as a
module). This hyphenated wrapper is the invocation name used by `make
exception-guard` and CI, matching the sibling telemetry-guard.py naming.
"""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

from exception_guard import main  # noqa: E402

if __name__ == "__main__":
    sys.exit(main())
