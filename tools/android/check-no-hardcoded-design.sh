#!/usr/bin/env bash
# Design-system guard for goatos-android.
#
# Fails if any UI code under app/ or feature/ hardcodes a color hex `Color(0x…)`
# or an ad-hoc `TextStyle(…)`. Colors MUST come from MeshaColors and type from
# MeshaType — both live in core-designsystem, the only module allowed to define
# raw values (and it is not scanned here). Keeps the whole app on one design system.
#
# Usage: bash tools/android/check-no-hardcoded-design.sh
set -euo pipefail

ROOT="apps/goatos-android"
# Only production UI sources; core-designsystem (token definitions) + tests excluded.
SCAN=("$ROOT/app/src/main" "$ROOT/feature")

scan() {
  local pattern="$1"
  if command -v rg >/dev/null 2>&1; then
    rg -n --glob '*.kt' \
       --glob '!**/src/test/**' --glob '!**/src/androidTest/**' \
       --glob '!**/core-designsystem/**' \
       "$pattern" "${SCAN[@]}" 2>/dev/null || true
  else
    grep -rn --include='*.kt' "$pattern" "${SCAN[@]}" 2>/dev/null \
      | grep -v -e '/src/test/' -e '/src/androidTest/' -e 'core-designsystem' || true
  fi
}

fail=0

hex="$(scan 'Color\(0[xX]')"
if [ -n "$hex" ]; then
  echo "❌ Hardcoded hex colors found — use MeshaColors.* instead:"
  echo "$hex"
  echo
  fail=1
fi

ts="$(scan 'TextStyle\(')"
if [ -n "$ts" ]; then
  echo "❌ Ad-hoc TextStyle(...) found — use MeshaType.* instead:"
  echo "$ts"
  echo
  fail=1
fi

if [ "$fail" -ne 0 ]; then
  echo "Design-system guard FAILED: move the values into core-designsystem (MeshaColors / MeshaType)."
  exit 1
fi

echo "✅ Design-system guard passed: no hardcoded hex / TextStyle in app or feature UI."
