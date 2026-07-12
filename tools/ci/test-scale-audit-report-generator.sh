#!/bin/bash
# Test the scale-audit report generator binding to latency gates.
#
# This test verifies:
# 1. Report generation without gate artifacts produces UNVERIFIED state
# 2. Report generation with gate artifacts produces VERIFIED state (when gates pass)
# 3. Report generation with failed gates produces FAILED state
# 4. The report includes gate results in the HTML when present

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
GENERATOR="$SCRIPT_DIR/generate-scale-audit-report.py"

# Test temp directory
TEST_DIR=$(mktemp -d)
trap "rm -rf $TEST_DIR" EXIT

echo "=== Test: Report generation without gate artifacts ==="
python3 "$GENERATOR" \
  --markdown "$REPO_ROOT/context/execution/scale-audit-fix-e2e-report-2026-07-11.md" \
  --output "$TEST_DIR/test1-unverified.html" \
  --commit-sha "abcd1234567890"

if grep -q "UNVERIFIED" "$TEST_DIR/test1-unverified.html"; then
  echo "PASS: Report shows UNVERIFIED when no gates are present"
else
  echo "FAIL: Report should show UNVERIFIED when no gates are present"
  exit 1
fi

if grep -q "latency gates not run" "$TEST_DIR/test1-unverified.html"; then
  echo "PASS: Report explains gates not run"
else
  echo "FAIL: Report should explain gates not run"
  exit 1
fi

echo ""
echo "=== Test: Report generation with passing gate artifacts ==="

# Create fake passing API latency gate result
cat > "$TEST_DIR/api-gate-pass.json" <<'EOF'
{
  "base_url": "http://localhost:8080",
  "tenant_id": "test",
  "results": [
    {
      "name": "control_tower",
      "method": "GET",
      "path": "/control-tower/vaccination",
      "samples": 20,
      "failures": 0,
      "first_failure": null,
      "p50_ms": 150,
      "p90_ms": 280,
      "p95_ms": 420,
      "p99_ms": 890,
      "max_ms": 950,
      "p90_threshold_ms": 300,
      "p95_threshold_ms": 500,
      "p99_threshold_ms": 1000,
      "passed": true
    }
  ]
}
EOF

# Create fake passing process-integrity gate result
cat > "$TEST_DIR/pi-gate-pass.json" <<'EOF'
{
  "checks": [
    {
      "name": "fresh_projection_control_tower",
      "passed": true,
      "latency_ms": 250,
      "threshold_ms": 500
    },
    {
      "name": "stale_projection_control_tower",
      "passed": true,
      "latency_ms": 280,
      "threshold_ms": 500
    }
  ]
}
EOF

python3 "$GENERATOR" \
  --markdown "$REPO_ROOT/context/execution/scale-audit-fix-e2e-report-2026-07-11.md" \
  --api-latency-output "$TEST_DIR/api-gate-pass.json" \
  --process-integrity-output "$TEST_DIR/pi-gate-pass.json" \
  --output "$TEST_DIR/test2-verified.html" \
  --commit-sha "abcd1234567890"

if grep -q "VERIFIED" "$TEST_DIR/test2-verified.html"; then
  echo "PASS: Report shows VERIFIED when all gates pass"
else
  echo "FAIL: Report should show VERIFIED when all gates pass"
  exit 1
fi

if grep -q "p95 latency" "$TEST_DIR/test2-verified.html"; then
  echo "PASS: Report includes gate results"
else
  echo "FAIL: Report should include gate results"
  exit 1
fi

echo ""
echo "=== Test: Report generation with failing gate artifacts ==="

# Create fake failing API latency gate result
cat > "$TEST_DIR/api-gate-fail.json" <<'EOF'
{
  "base_url": "http://localhost:8080",
  "tenant_id": "test",
  "results": [
    {
      "name": "control_tower",
      "method": "GET",
      "path": "/control-tower/vaccination",
      "samples": 20,
      "failures": 0,
      "first_failure": null,
      "p50_ms": 350,
      "p90_ms": 550,
      "p95_ms": 850,
      "p99_ms": 1500,
      "max_ms": 2000,
      "p90_threshold_ms": 300,
      "p95_threshold_ms": 500,
      "p99_threshold_ms": 1000,
      "passed": false
    }
  ]
}
EOF

python3 "$GENERATOR" \
  --markdown "$REPO_ROOT/context/execution/scale-audit-fix-e2e-report-2026-07-11.md" \
  --api-latency-output "$TEST_DIR/api-gate-fail.json" \
  --output "$TEST_DIR/test3-failed.html" \
  --commit-sha "abcd1234567890"

if grep -q "FAILED" "$TEST_DIR/test3-failed.html"; then
  echo "PASS: Report shows FAILED when gates fail"
else
  echo "FAIL: Report should show FAILED when gates fail"
  exit 1
fi

if grep -q "gate failures" "$TEST_DIR/test3-failed.html"; then
  echo "PASS: Report indicates gate failures"
else
  echo "FAIL: Report should indicate gate failures"
  exit 1
fi

echo ""
echo "=== All tests passed ==="
