#!/usr/bin/env bash
set -euo pipefail

if [ ! -f .agent/scope.json ] || ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  exit 0
fi

python3 - <<'PY'
import json
import subprocess
import sys
from pathlib import Path

scope = json.loads(Path(".agent/scope.json").read_text())
allowed = scope.get("allowed_paths", [])
if "*" in allowed:
    sys.exit(0)

def git_files(args):
    result = subprocess.run(["git", *args], text=True, capture_output=True, check=False)
    return set(filter(None, result.stdout.splitlines()))

changed = git_files(["diff", "--name-only"]) | git_files(["diff", "--cached", "--name-only"])
violations = []
for path in changed:
    if not any(path == a.rstrip("/") or path.startswith(a.rstrip("/") + "/") for a in allowed):
        violations.append(path)

if violations:
    print("Write-scope violation. Allowed paths:")
    for item in allowed:
        print(f"  - {item}")
    print("Changed paths outside scope:")
    for item in sorted(violations):
        print(f"  - {item}")
    sys.exit(1)
PY
