# Staging Android signed release

This runbook is for producing the **stg release** APK/AAB that points to:

```text
API:     https://stg-api.dashboard.mesha.sg/
Package: sg.mesha.goatos.stg
```

Do not commit keystores or passwords to Git. The staging upload key is scoped
to the `stg` product flavor only; a future prod build must use separate prod
signing secrets and the prod package.

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

Firebase App Distribution uploads for Android STG must go through the Gradle
upload task below. Do not manually distribute an APK through the Firebase
console, `firebase appdistribution:distribute`, or any other upload path unless
the maintainer explicitly asks for a one-off rescue build and the release notes
still include the source label.

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
Website: APK bytes identical to Firebase APK
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

Then update the website repo's static asset before the website deploy:

```bash
cp "$APK" /Users/ravi/mesha/website/public/app.apk
npm --prefix /Users/ravi/mesha/website run build
firebase --project goatos-sheets deploy --only hosting
```

Do not bump or rebuild another Android version for the website copy. Firebase
App Distribution and `mesha.sg/app.apk` must carry the same `versionName`,
`versionCode`, and APK bytes for a given release.

The browser's default save name comes from `website/firebase.json`
`Content-Disposition`. Before deploying, update it to the release being
published:

```text
attachment; filename="Mesha-<versionName>-code-<versionCode>.apk"
```

For example:

```text
attachment; filename="Mesha-0.1.19-stg-code-20.apk"
```

Verify the local website copy is byte-for-byte the Android release APK:

```bash
shasum -a 256 "$APK" /Users/ravi/mesha/website/public/app.apk /Users/ravi/mesha/website/dist/app.apk
```

Then run the release identity guard. It reads `versionName`/`versionCode` from
the APK manifest, checks the website `Content-Disposition` filename, and proves
the Android build, website `public`, and website `dist` APK bytes are identical:

```bash
tools/ci/check-android-stg-release-version.sh
```

After deploy, verify the URL returns an APK response instead of the website:

```bash
curl -I https://mesha.sg/app.apk
```

Expected headers include:

```text
Content-Type: application/vnd.android.package-archive
Content-Disposition: attachment; filename="Mesha-<versionName>-code-<versionCode>.apk"
Cache-Control: no-cache, max-age=0
```

Also validate the live URL in Chrome before reporting the release complete.
Because a previous bad deploy can be cached as website HTML, use the versioned
operator link for the release, for example:

```text
https://mesha.sg/app.apk?v=<versionCode>
```

Chrome must start an APK download. Seeing the Mesha website means the release is
not done, even if `curl` already returns APK headers.

After deploy, run the same guard against the live URL:

```bash
LIVE_URL="https://mesha.sg/app.apk?v=<versionCode>" tools/ci/check-android-stg-release-version.sh
```

Do not report the Android release as complete unless this guard prints one
release identity and the same identity is visible in Firebase App Distribution
and Google Play Internal Testing. If Play upload or Play Console access is not
available, say Play is not verified instead of guessing a version.

Do not use a dirty upload to answer whether a production-like phone APK contains
a feature. If `-PallowDirtyFirebaseDistribution=true` is used, mark the Firebase
release notes as throwaway/debug and record the dirty source label.

After upload, install the Firebase App Distribution build on the phone and
verify:

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

## Stg signing is not prod signing

`assembleStgRelease` uses:

```text
Package: sg.mesha.goatos.stg
Secrets: android-stg-upload-*
Firebase project/app: goatos-stg / sg.mesha.goatos.stg
```

Prod must use its own package, Firebase app, Play/App Signing setup, and Secret
Manager names, for example:

```text
Package: sg.mesha.goatos
Secrets: android-prod-upload-*
Firebase/Play: prod-owned app registration
```

Never load the stg signing env vars and build a prod release. The Gradle
configuration intentionally attaches the stg signing config only to the `stg`
flavor so the stg upload key cannot silently sign `prodRelease`.
