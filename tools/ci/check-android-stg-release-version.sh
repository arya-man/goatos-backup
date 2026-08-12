#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
APK="${APK:-$ROOT/apps/goatos-android/app/build/outputs/apk/stg/release/app-stg-release.apk}"
WEBSITE_DIR="${WEBSITE_DIR:-/Users/ravi/mesha/website}"
LIVE_URL="${LIVE_URL:-}"

die() {
  echo "ERROR: $*" >&2
  exit 1
}

if [[ "${1:-}" == "--self-test" ]]; then
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' EXIT
  mkdir -p "$tmp/site/public" "$tmp/site/dist"
  printf 'apk-bytes' > "$tmp/app.apk"
  cp "$tmp/app.apk" "$tmp/site/public/app.apk"
  cp "$tmp/app.apk" "$tmp/site/dist/app.apk"
  cat > "$tmp/site/firebase.json" <<'JSON'
{"hosting":{"headers":[{"source":"/app.apk","headers":[{"key":"Content-Disposition","value":"attachment; filename=\"Mesha-1.2.3-stg-code-123.apk\""}]}]}}
JSON
  output="$("$0" --metadata-only "1.2.3-stg" "123" "$tmp/app.apk" "$tmp/site")"
  grep -q "release_identity=1.2.3-stg code=123" <<<"$output"
  echo "self-test passed"
  exit 0
fi

metadata_only=0
if [[ "${1:-}" == "--metadata-only" ]]; then
  metadata_only=1
  expected_version="${2:?versionName required}"
  expected_code="${3:?versionCode required}"
  APK="${4:?apk required}"
  WEBSITE_DIR="${5:?website dir required}"
fi

[[ -f "$APK" ]] || die "APK not found: $APK"
[[ -d "$WEBSITE_DIR" ]] || die "website dir not found: $WEBSITE_DIR"
[[ -f "$WEBSITE_DIR/firebase.json" ]] || die "website firebase.json missing: $WEBSITE_DIR/firebase.json"
[[ -f "$WEBSITE_DIR/public/app.apk" ]] || die "website public/app.apk missing"
[[ -f "$WEBSITE_DIR/dist/app.apk" ]] || die "website dist/app.apk missing; run npm --prefix $WEBSITE_DIR run build"

if [[ "$metadata_only" == "0" ]]; then
  aapt_bin="${AAPT:-}"
  if [[ -z "$aapt_bin" ]]; then
    for candidate in \
      "$ANDROID_HOME/build-tools/36.0.0/aapt" \
      "$ANDROID_SDK_ROOT/build-tools/36.0.0/aapt" \
      "$HOME/Library/Android/sdk/build-tools/36.0.0/aapt"; do
      [[ -x "$candidate" ]] && aapt_bin="$candidate" && break
    done
  fi
  [[ -n "$aapt_bin" && -x "$aapt_bin" ]] || die "aapt not found; set AAPT=/path/to/aapt"
  badging="$("$aapt_bin" dump badging "$APK" | sed -n '1p')"
  expected_version="$(sed -n "s/.*versionName='\\([^']*\\)'.*/\\1/p" <<<"$badging")"
  expected_code="$(sed -n "s/.*versionCode='\\([^']*\\)'.*/\\1/p" <<<"$badging")"
fi

[[ -n "$expected_version" ]] || die "could not read APK versionName"
[[ -n "$expected_code" ]] || die "could not read APK versionCode"
expected_filename="Mesha-${expected_version}-code-${expected_code}.apk"

actual_filename="$(
  node -e '
    const fs = require("fs")
    const doc = JSON.parse(fs.readFileSync(process.argv[1], "utf8"))
    const blocks = Array.isArray(doc.hosting) ? doc.hosting : [doc.hosting]
    for (const block of blocks.filter(Boolean)) {
      for (const rule of block.headers || []) {
        if (rule.source !== "/app.apk") continue
        for (const header of rule.headers || []) {
          if (String(header.key).toLowerCase() !== "content-disposition") continue
          const match = String(header.value).match(/filename="([^"]+)"/)
          if (match) {
            console.log(match[1])
            process.exit(0)
          }
        }
      }
    }
  ' "$WEBSITE_DIR/firebase.json"
)"

[[ "$actual_filename" == "$expected_filename" ]] || die "website Content-Disposition filename mismatch: got $actual_filename want $expected_filename"

hashes="$(shasum -a 256 "$APK" "$WEBSITE_DIR/public/app.apk" "$WEBSITE_DIR/dist/app.apk" | awk '{print $1}' | sort -u | wc -l | tr -d ' ')"
[[ "$hashes" == "1" ]] || die "APK bytes differ between Android build, website public, and website dist"

if [[ -n "$LIVE_URL" ]]; then
  headers="$(curl -fsSI "$LIVE_URL")"
  grep -qi "content-type: application/vnd.android.package-archive" <<<"$headers" || die "live URL is not serving APK content-type"
  grep -q "filename=\"$expected_filename\"" <<<"$headers" || die "live URL filename header does not match $expected_filename"
fi

echo "release_identity=$expected_version code=$expected_code filename=$expected_filename"
echo "apk_sha256=$(shasum -a 256 "$APK" | awk '{print $1}')"
