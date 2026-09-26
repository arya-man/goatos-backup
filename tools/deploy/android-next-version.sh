#!/usr/bin/env bash
# Android release versionCode for STG mobile distribution, computed at deploy time.
# No deploy path commits a version bump to main (ruleset main-land-receipt), so the next
# code is max(checked-in releaseVersionCode, highest published code) + 1, which is
# monotonic and never reuses a code already published. Published codes come from the
# release APK names in gs://goatos-stg-public-downloads/operator/releases/ (Mesha-1.0.N.apk
# carries versionCode N+50, the same mapping as version_name_for_code).

# android_published_max_code: read object names/paths on stdin, print the highest code (0 if none).
android_published_max_code() {
  local max=0 line name patch code
  while IFS= read -r line; do
    name="${line##*/}"
    [[ "$name" =~ ^Mesha-1\.0\.([0-9]+)\.apk$ ]] || continue
    patch="${BASH_REMATCH[1]}"
    code=$((10#$patch + 50))
    (( code > max )) && max=$code
  done
  printf '%s\n' "$max"
}

# android_next_version_code CHECKED_IN_CODE: published names on stdin, print the next code.
android_next_version_code() {
  local checked_in="$1" published
  [[ "$checked_in" =~ ^[0-9]+$ ]] || { echo "android_next_version_code: bad checked-in code '${checked_in}'" >&2; return 1; }
  published="$(android_published_max_code)"
  local base=$(( 10#$checked_in > published ? 10#$checked_in : published ))
  printf '%s\n' "$((base + 1))"
}
