# Staging Android signed release

This runbook is for producing the **stg release** APK/AAB that points to:

```text
API:     https://stg-api.dashboard.mesha.sg/
Package: sg.mesha.goatos.stg
```

Do not commit keystores or passwords to Git. The staging upload key is scoped
to the `stg` product flavor only; a future prod build must use separate prod
signing secrets and the prod package.

## Secret source of truth

The staging upload key is stored in Google Secret Manager under project
`goatos-stg`.

```text
Secret: android-stg-upload-keystore-jks
Secret: android-stg-upload-keystore-password
Secret: android-stg-upload-key-alias
Secret: android-stg-upload-key-password
```

Only release builders should have Secret Manager access.

Grant access through IAM on the secrets or an approved release-builder Google
group. Do not send the `.jks` or passwords through chat, tickets, or email.

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

```bash
cd apps/goatos-android

./gradlew \
  :app:assembleStgRelease \
  :app:appDistributionUploadStgRelease \
  --no-configuration-cache \
  -PfadReleaseNotes="Staging release: points to stg-api.dashboard.mesha.sg"
```

After upload, install the Firebase App Distribution build on the phone and
verify:

1. Android package is `sg.mesha.goatos.stg`.
2. App talks to `https://stg-api.dashboard.mesha.sg/`.
3. SSO succeeds for an approved founder/CXO email.
4. `/app/bootstrap` succeeds; a 403 means the email grant was not linked to an
   active `workforce_members.user_id` profile.
5. Firebase Analytics/Crashlytics show the backend workforce member id as the
   user id after bootstrap.

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
