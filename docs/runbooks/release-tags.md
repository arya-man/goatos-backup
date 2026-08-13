# GitHub Release Tags

Every Goat OS release must leave an annotated GitHub tag. The tag is the durable
source-of-truth breadcrumb that connects GitHub, Cloud Deploy, Firebase App
Distribution, and the exact source commit.

Use the repo command, not hand-written tags:

```bash
make release-tag ENV=stg
```

For staging Cloud Deploy, `tools/deploy/stg-clouddeploy-release.sh` creates the
tag automatically after rollout success and image parity verification.

For Firebase App Distribution, pass the Firebase release URL and Android version
into a mobile release tag after upload:

```bash
make release-tag \
  ENV=stg \
  SHA="$GIT_SHA" \
  TAG_NAME=android/stg/v0.1.14 \
  UPDATE_TAG=1 \
  ANDROID_VERSION=0.1.14-stg \
  ANDROID_VERSION_CODE=15 \
  FIREBASE_RELEASE_URL="https://console.firebase.google.com/..."
```

## Tag Names

Default tag names are:

```text
<env>/release-<yyyy-mm-dd>-<12-char-sha>
```

Example:

```text
stg/release-2026-08-08-d2357360a46d
```

Use `TAG_NAME=...` for deliberate secondary tags such as Android/Firebase
distribution markers:

```text
android/stg/v0.1.14
```

## Tag Sections

The generated tag message always contains these sections:

```text
Backend
Frontend/Admin Web
Mobile Android
Infra/Deploy
Docs/Seed/Data
Other
```

The classifier is path-based:

```text
backend/**, contracts/**, packages/api-client/** -> Backend
apps/admin-web/**, mock/**                       -> Frontend/Admin Web
apps/goatos-android/**                           -> Mobile Android
deploy/**, infra/**, tools/deploy/**, tools/ci/** -> Infra/Deploy
docs/**, context/**, fixtures/**                 -> Docs/Seed/Data
```

## Firebase Provenance

The Android APK already embeds source provenance in `BuildConfig.SOURCE_COMMIT`,
`BuildConfig.SOURCE_TAG`, `BuildConfig.SOURCE_BRANCH`, and
`BuildConfig.SOURCE_LABEL`. Firebase App Distribution release notes include the
same source label.

The GitHub release tag must additionally record:

```text
Firebase Android: <version> / versionCode <code>
Firebase release: <console URL>
```

If a Firebase upload succeeds but tagging fails, do not call the release closed.
Create or repair the tag before handoff.

`UPDATE_TAG=1` may only replace an annotated tag when the existing tag already
points to the same commit. The helper refuses to move tags across commits.
