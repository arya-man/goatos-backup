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

## Production-Facing Firebase Setup

The production-facing Android app package is `sg.mesha.goatos`, with:

```text
API_BASE_URL=https://api.goatos.mesha.sg/
AUTH_ACTION_CONTINUE_URL=https://dashboard.mesha.sg/login
```

For the current cleanup path, add package `sg.mesha.goatos` to the existing
`goatos-stg` Firebase project and download that app's `google-services.json` to
`app/src/prod/google-services.json`. Public package names, URLs, release notes,
and app-version text must not include `stg`; the Firebase project id may remain
`goatos-stg` internally for Auth/FCM until a separate production Firebase project
is created.

## Retired Staging Package

The historical Android package `sg.mesha.goatos.stg` is no longer an active
release channel. Do not publish, upload, or hand testers builds from that
package.

The active production-facing package is:

```text
sg.mesha.goatos
```

Use `../../tools/deploy/stg-mobile-distribution.sh` for mobile distribution. It
publishes the same release identity to Firebase App Distribution, Google Play
Internal Testing, and `https://mesha.sg/app.apk`.

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
