#!/usr/bin/env python3
"""telemetry-guard.py — thin CLI entrypoint.

The actual logic lives in telemetry_guard.py (underscore — importable as a
module by test_telemetry_guard.py). This hyphenated wrapper is the invocation
name used by `make telemetry-guard` and CI, matching the sibling `mobile-guard`
/ `scale-guard` script naming in tools/agent-hooks and tools/scale-guard.
"""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

from telemetry_guard import main  # noqa: E402

if __name__ == "__main__":
    sys.exit(main())
