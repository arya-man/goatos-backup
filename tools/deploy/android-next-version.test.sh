#!/usr/bin/env bash
# Unit tests for android-next-version.sh plus the "no deploy path commits to main" precheck.
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
root="$(cd "$here/../.." && pwd)"
# shellcheck source=android-next-version.sh
source "$here/android-next-version.sh"
fail=0
check() { if [[ "$2" == "$3" ]]; then echo "ok: $1"; else echo "FAIL: $1: got $2 want $3" >&2; fail=1; fi; }

check "no published builds -> checked-in + 1" "$(printf '' | android_next_version_code 92)" 93
check "published ahead of checked-in -> published + 1" \
  "$(printf 'gs://b/operator/releases/Mesha-1.0.40.apk\ngs://b/operator/releases/Mesha-1.0.42.apk\n' | android_next_version_code 90)" 93
check "checked-in ahead of published -> checked-in + 1" \
  "$(printf 'gs://b/operator/releases/Mesha-1.0.42.apk\n' | android_next_version_code 100)" 101
check "equal -> +1, never reuses" "$(printf 'Mesha-1.0.42.apk\n' | android_next_version_code 92)" 93
check "numeric not lexical max (1.0.9 vs 1.0.100)" \
  "$(printf 'Mesha-1.0.9.apk\nMesha-1.0.100.apk\n' | android_next_version_code 92)" 151
check "ignores unrelated objects" \
  "$(printf 'Mesha-1.0.99.apk.sha256\nMesha-2.0.1.apk\nlatest/app.apk\nMesha-1.0.42.apk\n' | android_next_version_code 50)" 93
# Monotonic: publishing the result then recomputing always yields a strictly larger code.
prev=92; names=""
for _ in 1 2 3 4 5; do
  next="$(printf '%b' "$names" | android_next_version_code 92)"
  (( next > prev )) || { echo "FAIL: not monotonic ($next <= $prev)" >&2; fail=1; }
  names+="Mesha-1.0.$((next - 50)).apk\n"; prev=$next
done
echo "ok: monotonic across 5 simulated releases (last $prev)"
if printf '' | android_next_version_code abc 2>/dev/null; then echo "FAIL: accepted non-numeric" >&2; fail=1; else echo "ok: rejects non-numeric"; fi

# Precheck: no deploy path may commit to main via the GitHub contents API.
if grep -nE 'api\.github\.com/repos/[^ ]*/contents|bump-android-version|bumpAndroidReleaseVersion|GOATOS_GITHUB_PAT' \
  "$root/tools/deploy/slack-stg-deploy-bot/main.go" \
  "$root/tools/deploy/stg-mobile-distribution.sh" "$root/cloudbuild.stg.yaml"; then
  echo "FAIL: a deploy path still commits a version bump to main" >&2; fail=1
else
  echo "ok: no deploy path commits to main"
fi
grep -q 'android_next_version_code' "$root/tools/deploy/stg-mobile-distribution.sh" ||
  { echo "FAIL: stg-mobile-distribution.sh does not compute the next versionCode" >&2; fail=1; }
exit $fail
