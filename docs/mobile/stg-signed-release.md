# Legacy staging Android signed release

This runbook is retained only for emergency compatibility builds of the legacy
staging Android package. Current public/operator-facing mobile work must use
`docs/mobile/production-facing-release.md`, package `sg.mesha.goatos`, API
`https://api.goatos.mesha.sg/`, and dashboard `https://dashboard.mesha.sg/`.

The legacy staging APK/AAB points to:

```text
API:     https://stg-api.dashboard.mesha.sg/
Package: sg.mesha.goatos.stg
```

Do not commit keystores or passwords to Git. The staging upload key is scoped
to the `stg` product flavor only; do not use it for public production-facing
builds.

## Signing secret source of truth

The Android staging upload key is stored in Google Secret Manager under project
`goatos-stg`. These secrets are required for `assembleStgRelease`; they do not
authenticate the Firebase App Distribution upload.

```text
Secret: android-stg-upload-keystore-jks
Secret: android-stg-upload-keystore-password
Secret: android-stg-upload-key-alias
Secret: android-stg-upload-key-password
```

Only release builders should have Secret Manager access.

Grant access through IAM on the secrets or an approved release-builder Google
group. Do not send the `.jks` or passwords through chat, tickets, or email.

## Firebase upload auth source of truth

`appDistributionUploadStgRelease` also needs Firebase App Distribution auth.
Release builders must use exactly one of:

```text
GOOGLE_APPLICATION_CREDENTIALS=/absolute/path/to/goatos-stg Firebase App Distribution service-account JSON
FIREBASE_TOKEN=<token minted by firebase login:ci>
firebase login as an authorized release builder
```

Current Secret Manager state:

```text
Exists: android-stg-upload-* signing secrets
Exists: goatos-stg-firebase-web-config
Exists: goatos-stg-firebase-app-distribution-sa-json
```

Codex, Claude, and developers should restore both Android signing and Firebase
upload auth through the repo helper:

```bash
make restore-stg-android-release-env
source .local/android-signing/stg-release-env.sh
```

Do not commit the JSON or paste it into chat, tickets, or email.

## Restore local signing files

Run from the repo root:

```bash
gcloud config set account ravi@mesha.sg
gcloud config set project goatos-stg

mkdir -p .local/android-signing

gcloud secrets versions access latest \
  --project goatos-stg \
  --secret android-stg-upload-keystore-jks \
  --out-file .local/android-signing/goatos-stg-upload.jks

export GOATOS_ANDROID_STG_KEYSTORE="$PWD/.local/android-signing/goatos-stg-upload.jks"
export GOATOS_ANDROID_STG_KEYSTORE_PASSWORD="$(
  gcloud secrets versions access latest \
    --project goatos-stg \
    --secret android-stg-upload-keystore-password
)"
export GOATOS_ANDROID_STG_KEY_ALIAS="$(
  gcloud secrets versions access latest \
    --project goatos-stg \
    --secret android-stg-upload-key-alias
)"
export GOATOS_ANDROID_STG_KEY_PASSWORD="$(
  gcloud secrets versions access latest \
    --project goatos-stg \
    --secret android-stg-upload-key-password
)"
```

If manual restore is needed, use the documented service-account JSON secret:

```bash
gcloud secrets versions access latest \
  --project goatos-stg \
  --secret goatos-stg-firebase-app-distribution-sa-json \
  --out-file .local/android-signing/firebase-app-distribution-sa.json

export GOOGLE_APPLICATION_CREDENTIALS="$PWD/.local/android-signing/firebase-app-distribution-sa.json"
```

Expected upload key fingerprints:

```text
SHA-1:   8a6c643b88c9b4c2b913e05e94006a8a84386af3
SHA-256: 79c61b82bc4832e93461cfd60d7042fbf4ab97c5885709e46b53299ac2f9f749
```

Verify locally:

```bash
keytool -list -v \
  -keystore .local/android-signing/goatos-stg-upload.jks \
  -alias "$GOATOS_ANDROID_STG_KEY_ALIAS"
```

Use `gcloud secrets versions access --out-file` for the binary `.jks`. Do not
redirect stdout with `>` for the keystore.

## Build and upload to Firebase App Distribution

Before every release, bump `versionCode` and `versionName` in
`apps/goatos-android/app/build.gradle.kts`.

Before running the upload, restore both:

```text
1. Android signing inputs: GOATOS_ANDROID_STG_*
2. Firebase upload auth: GOOGLE_APPLICATION_CREDENTIALS, FIREBASE_TOKEN, or firebase login
```

Firebase App Distribution uploads for the legacy Android STG channel must go
through the Gradle upload task below. Do not use this command for the current
production-facing GoatOS app. Current public/operator-facing releases go through
`tools/deploy/stg-mobile-distribution.sh`, which builds `ProdRelease` for
`sg.mesha.goatos` while reusing the existing `goatos-stg` Firebase project
internally.

```bash
cd apps/goatos-android

./gradlew \
  :app:assembleStgRelease \
  :app:appDistributionUploadStgRelease \
  --no-configuration-cache \
  -PfadReleaseNotes="Staging release: points to stg-api.dashboard.mesha.sg"
```

Every Firebase App Distribution upload is traceable to source:

- the APK bakes `BuildConfig.SOURCE_COMMIT`, `BuildConfig.SOURCE_TAG`,
  `BuildConfig.SOURCE_BRANCH`, `BuildConfig.SOURCE_LABEL`, and matching string
  resources into the build;
- default Firebase release notes append the same source label;
- `appDistributionUploadStgRelease` refuses dirty local worktrees unless the
  builder deliberately passes `-PallowDirtyFirebaseDistribution=true` for a
  throwaway/debug build.

After upload, create or update the GitHub release tag with the Firebase release
URL printed by Gradle:

```bash
make release-tag \
  ENV=stg \
  SHA="$(git rev-parse HEAD)" \
  TAG_NAME="android/stg/v0.1.14" \
  UPDATE_TAG=1 \
  ANDROID_VERSION=0.1.14-stg \
  ANDROID_VERSION_CODE=15 \
  FIREBASE_RELEASE_URL="<Firebase console release URL>"
```

Do not call the Firebase release closed until the GitHub tag exists and includes
the Android version/code and Firebase release URL.

## Mandatory employee distribution: Firebase + Play + mesha.sg/app.apk

Trigger phrases that require this section: "Firebase publish", "upload to
Firebase", "Firebase App Distribution", "release Android STG", "push APK",
"internal test", "Play internal testing", and any STG deploy that includes an
Android release artifact.

This section is a release gate, not an optional operator convenience. Do not
report an Android STG/Firebase release as complete until all three employee
distribution channels are updated and verified:

1. Firebase App Distribution for dev/QA testers.
2. Google Play Internal Testing for employees/operators who install from Play.
3. `https://mesha.sg/app.apk` for direct operator download / rescue install.

Goat OS is an internal employee app. Do not publish the STG app to production or
public Play tracks unless the maintainer explicitly asks for a separate public
release plan.

Every successful `appDistributionUploadStgRelease` must also publish the exact
same release APK to the stable operator download URL:

```text
https://mesha.sg/app.apk
```

This URL is for field operators who cannot reliably use Firebase App Tester. It
must download the APK directly; it must never render the Mesha website, a helper
page, or a Firebase tester page.

`mesha.sg/app.apk` is a stable redirect to Google Cloud Storage, not a website
static asset. Do not copy APKs into `/Users/ravi/mesha/website/public/`, do not
rebuild the Mesha marketing website, and do not deploy Firebase Hosting merely
to publish a new APK. Updating the operator APK must be a Storage upload only
after the redirect has been configured once.

## Google Play Internal Testing

Play Internal Testing is the preferred install/update path for employees and
operators because it appears in Play Store after the tester accepts the opt-in
link once. Keep Firebase App Distribution and the direct APK URL as backup
channels.

Use the STG package for the internal-only employee app:

```text
sg.mesha.goatos.stg
```

Play releases use an Android App Bundle (`.aab`), not the APK uploaded to
Firebase. Build the Play bundle from the same source commit, `versionName`, and
`versionCode` as the Firebase APK:

```bash
cd apps/goatos-android
./gradlew :app:bundleStgRelease --no-configuration-cache

AAB=app/build/outputs/bundle/stgRelease/app-stg-release.aab
test -f "$AAB"
```

Do not rebuild with a different version for Play. A release may contain
different file formats, but it must have one release identity:

```text
Firebase: APK, versionName/versionCode/source commit X
Play internal: AAB, same versionName/versionCode/source commit X
mesha.sg/app.apk: Storage-hosted APK bytes identical to Firebase APK
```

The Play internal tester list must be the same email IDs that have access to
Firebase App Distribution for the employee/operator test group. Do not maintain
a separate hand-picked Play tester list. Before publishing the Play internal
release, verify Play Console contains the same email set as Firebase App
Distribution, or sync Play from the Firebase tester group export/source-of-truth
list. If this cannot be verified, do not say employees can get the build from
Play yet.

Initial Play setup is manual in Play Console:

1. Create the Mesha/Goat OS STG app if it does not exist.
2. Create/enable the Internal testing track.
3. Add the Firebase tester emails to the Play internal tester list.
4. Upload the STG release AAB to Internal testing.
5. Copy the Play opt-in link and share it with employees/operators.

After the app exists and Play Developer API access is configured, future agents
may automate the AAB upload to the `internal` track. The automation must still
preserve the same source commit/version identity and tester-list alignment.
Email-list mutation through the Play Developer API is not always equivalent to
the Play Console UI, so verify the actual tester access before claiming the
release is available in Play.

Operator success criteria:

1. Tester opens the Play internal opt-in link once.
2. Tester joins the test.
3. Tester can install/update Mesha from Play Store.
4. The installed app shows the same version name/code as Firebase and
   `mesha.sg/app.apk`.

## Mandatory mirror: same APK to mesha.sg/app.apk

Use the APK produced by the same Gradle invocation above:

```bash
APK=apps/goatos-android/app/build/outputs/apk/stg/release/app-stg-release.apk
test -f "$APK"
```

Extract the Android version metadata from that APK:

```bash
ANDROID_VERSION_NAME=$(
  /Users/ravi/Library/Android/sdk/cmdline-tools/latest/bin/apkanalyzer \
    manifest version-name "$APK"
)
ANDROID_VERSION_CODE=$(
  /Users/ravi/Library/Android/sdk/cmdline-tools/latest/bin/apkanalyzer \
    manifest version-code "$APK"
)
DOWNLOAD_NAME="Mesha-${ANDROID_VERSION_NAME}.apk"
```

Publish the same bytes to the public `goatos-stg` download bucket. Keep both:

- a permanent versioned object for audit/rollback;
- the stable `latest/app.apk` object that `https://mesha.sg/app.apk` redirects
  to and that is replaced on every release.

```bash
gcloud storage cp "$APK" \
  "gs://goatos-stg-public-downloads/operator/releases/${DOWNLOAD_NAME}" \
  --project=goatos-stg \
  --content-type='application/vnd.android.package-archive' \
  --cache-control='public, max-age=31536000, immutable' \
  --content-disposition="attachment; filename=\"${DOWNLOAD_NAME}\""

gcloud storage cp "$APK" \
  gs://goatos-stg-public-downloads/operator/latest/app.apk \
  --project=goatos-stg \
  --content-type='application/vnd.android.package-archive' \
  --cache-control='no-cache, max-age=0' \
  --content-disposition="attachment; filename=\"${DOWNLOAD_NAME}\""
```

For example:

```text
attachment; filename="Mesha-0.1.17-stg.apk"
```

Do not bump or rebuild another Android version for the Storage copy. Firebase
App Distribution and `mesha.sg/app.apk` must carry the same `versionName`,
`versionCode`, and APK bytes for a given release.

Verify the Storage object is byte-for-byte the Android release APK:

```bash
gcloud storage cp \
  gs://goatos-stg-public-downloads/operator/latest/app.apk \
  /Users/ravi/mesha/.local/verify-latest-app.apk \
  --project=goatos-stg
shasum -a 256 "$APK" /Users/ravi/mesha/.local/verify-latest-app.apk
```

Verify the direct Storage URL returns an APK response instead of HTML:

```bash
curl -I https://storage.googleapis.com/goatos-stg-public-downloads/operator/latest/app.apk
```

Verify the stable operator URL returns either a redirect to Storage or the final
APK response:

```bash
curl -I https://mesha.sg/app.apk
```

Expected headers include:

```text
Content-Type: application/vnd.android.package-archive
Content-Disposition: attachment; filename="Mesha-<versionName>.apk"
Cache-Control: no-cache, max-age=0
```

If `curl -I https://mesha.sg/app.apk` returns a 302 first, follow redirects with
`curl -I -L https://mesha.sg/app.apk` and verify the final response headers.

Also validate the live URL in Chrome before reporting the release complete.
Because a previous bad deploy can be cached as website HTML, use the versioned
operator link for the release, for example:

```text
https://mesha.sg/app.apk?v=<versionCode>
```

Chrome must start an APK download. Seeing the Mesha website means the release is
not done, even if `curl` already returns APK headers.

The one-time Firebase Hosting configuration for the public marketing site is a
redirect only:

```json
{
  "source": "/app.apk",
  "destination": "https://storage.googleapis.com/goatos-stg-public-downloads/operator/latest/app.apk",
  "type": 302
}
```

After that redirect exists in production, future Android STG releases must not
deploy the Mesha marketing website just to update the APK.

Do not use a dirty upload to answer whether a production-like phone APK contains
a feature. If `-PallowDirtyFirebaseDistribution=true` is used, mark the Firebase
release notes as throwaway/debug and record the dirty source label.

After a legacy upload, install the Firebase App Distribution build on the phone
and verify the legacy compatibility channel only:

1. Android package is `sg.mesha.goatos.stg`.
2. App talks to `https://stg-api.dashboard.mesha.sg/`.
3. SSO succeeds for an approved founder/CXO email.
4. `/app/bootstrap` succeeds; a 403 means the email grant was not linked to an
   active `workforce_members.user_id` profile.
5. Firebase Analytics/Crashlytics show the backend workforce member id as the
   user id after bootstrap.

When answering whether the Firebase APK installed on a phone contains a feature,
verify and record the source label first. Compare the APK/Firebase source label
against the commit or tag that introduced the feature. If the source label is
missing or cannot be verified, say that the phone APK cannot be proven from the
available evidence; do not infer it from local `HEAD`, `origin/main`, or memory.

## Legacy stg signing is not public release signing

`assembleStgRelease` uses:

```text
Package: sg.mesha.goatos.stg
Secrets: android-stg-upload-*
Firebase project/app: goatos-stg / sg.mesha.goatos.stg
```

Current production-facing builds use package `sg.mesha.goatos` and the Firebase
Android app `1:514832198871:android:2b3a80736ff2e8d9f19492` in the reused
`goatos-stg` project. Public release notes, app version wording, and operator
links must not contain `stg`.

```text
Package: sg.mesha.goatos
Secrets: android-prod-upload-*
Firebase/Play: prod-owned app registration
```

Never load the stg signing env vars and build a prod release. The Gradle
configuration intentionally attaches the stg signing config only to the `stg`
flavor so the stg upload key cannot silently sign `prodRelease`.
