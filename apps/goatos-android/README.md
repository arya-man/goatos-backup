# Goat OS Android (`sg.mesha.goatos`)

Native **Kotlin + Jetpack Compose** operator + leadership app. One common,
role-aware app; roles are runtime (from `/app/bootstrap`), env is a build flavor.
Spec: [`docs/mobile/`](../../docs/mobile/) (TRD, PRD, screens, design-system).

## Status: compiling skeleton (§13 step 3)

The multi-module structure, theme, nav shell, and the **backend-driven nav
chrome** are wired and **compile to an APK**. Feature screens are placeholders and
the network/data/DI layers run on fakes — this is the scaffold, not the product.

- ✅ 22 Gradle modules, Clean-Architecture boundaries, `assemble` green.
- ✅ Nav chrome spine end-to-end: `FakeAppApi` → `BootstrapRepository` →
  `BootstrapViewModel` → `GoatOsShell`, which renders `nav_chrome`
  (`EXPANDED` ⇒ module switcher, `MINIMAL` ⇒ bottom-bar only) straight from the
  backend. **The app never counts modules or checks role** (TRD §14).
- ⏳ Next passes: Hilt DI graph · Room + Proto DataStore + sync/outbox · Retrofit 3
  / OkHttp 5 + the **OpenAPI-generated Kotlin client** (from
  `contracts/openapi/app-api.yaml`) · real feature screens · RFID/CameraX adapters
  behind the existing ports · **Firebase (GATED** — needs `google-services.json`;
  analytics/notifications run on no-op fakes until then).

## Build

Goat OS Android developer entrypoints bootstrap Google's Android CLI first. Run
this once, or let `make android-doctor` / `make android-dev-run` do it
automatically:

```bash
bash ../../tools/dev/ensure-android-cli.sh
```

When `android` is missing, the helper installs the user-local CLI, runs
`android update`, `android init`, and `android skills add --all` so Codex,
Claude, and other detected agents get the official Android CLI and Android
skills. See
[`../../docs/mobile/android-cli-and-journeys.md`](../../docs/mobile/android-cli-and-journeys.md)
for how Goat OS uses Android CLI and Journeys.

```bash
# Requires JDK 17+ and the Android SDK (platform android-36).
cd apps/goatos-android
./gradlew assembleDevDebug   # or: ./gradlew assemble   (all modules)
```

`local.properties` (git-ignored) must point at the SDK: `sdk.dir=/path/to/Android/sdk`.

## Staging Firebase Upload

Do not guess or ask for the Android staging release placeholders in chat. The
stg signing material is intentionally outside Git and must be restored from the
official runbook before running `assembleStgRelease` or
`appDistributionUploadStgRelease`.

Required source of truth:

```text
../../docs/mobile/stg-signed-release.md
```

That runbook shows how to fetch the `goatos-stg` Secret Manager values for:

```text
GOATOS_ANDROID_STG_KEYSTORE
GOATOS_ANDROID_STG_KEYSTORE_PASSWORD
GOATOS_ANDROID_STG_KEY_ALIAS
GOATOS_ANDROID_STG_KEY_PASSWORD
```

For Firebase App Distribution auth, use either a `GOOGLE_APPLICATION_CREDENTIALS`
service-account JSON with the right `goatos-stg` permissions, or run
`firebase login` as an authorized release builder.

## Toolchain (verified at scaffold)

| | |
|---|---|
| JDK | 21 (compiles to JVM 17 bytecode) |
| Gradle | 9.6.1 (wrapper committed) |
| AGP | 9.2.0 — **built-in Kotlin** (no `org.jetbrains.kotlin.android` plugin) |
| Kotlin | 2.4.0 (compose + jvm plugins); serialization via AGP-managed KGP |
| compileSdk / minSdk / targetSdk | 36 / 29 / 36 (minSdk 29 = Android 10) |
| Compose | BOM 2026.06.01 |

**Version note:** the TRD's `lifecycle 2.11.0` / `activity 1.13.0` baselines require
`compileSdk 37`; since compileSdk is locked at **36**, the androidx runtime libs are
pinned to the 36-compatible line (`lifecycle 2.9.3`, `activity-compose 1.10.1`,
`navigation-compose 2.8.9`, `core-ktx 1.16.0`) in `gradle/libs.versions.toml`. Bump
together when compileSdk moves to 37.

## Repo separation

This app depends on the Go backend **only** through the app-api OpenAPI contract
(the generated Kotlin client), never on backend source. It is a self-contained
Gradle build (own wrapper, own version catalog, zero monorepo path deps), so it
lifts into its own repo by moving `apps/goatos-android/` out and pointing the
client generator at the published `app-api.yaml`. See
[`MODULE-MAP.md`](MODULE-MAP.md).
