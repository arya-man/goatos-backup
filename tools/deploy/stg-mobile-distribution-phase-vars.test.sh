#!/usr/bin/env bash
# The publish phase of stg-mobile-distribution.sh runs in its own Cloud Build step (MOBILE_PHASE=publish),
# so every shell variable it reads must be assigned before the phase branches, not only inside the
# build path. Regression: 26/09/2026 `play_base: unbound variable` failed Play Internal after
# Firebase and mesha.sg/app.apk were already published.
set -euo pipefail
f="$(cd "$(dirname "$0")" && pwd)/stg-mobile-distribution.sh"
split="$(grep -n 'MOBILE_PHASE" != "publish"' "$f" | head -1 | cut -d: -f1)"
[[ -n "$split" ]] || { echo "phase-vars: cannot find the publish/build split in $f" >&2; exit 1; }
rc=0
for v in play_base PLAY_QUOTA_PROJECT GOOGLE_PLAY_PACKAGE; do
  first_def="$(grep -nE "^${v}=" "$f" | head -1 | cut -d: -f1)"
  if [[ -z "$first_def" || "$first_def" -gt 60 ]]; then
    echo "phase-vars: ${v} must be assigned near the top of stg-mobile-distribution.sh (before any phase branch; found line ${first_def:-none})" >&2
    rc=1
  fi
done
# Every ${play_base} use after the split must have a top-level definition above it.
[[ $rc -eq 0 ]] && echo "phase-vars: ok (publish-phase Play variables are set before the phase split at line ${split})"
exit $rc
