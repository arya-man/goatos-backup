# `google-services.json` per flavor

Full detail: `../docs/TELEMETRY.md`. This file is the short pointer next to the actual JSONs.

`google-services.json` is globally gitignored in this app (`apps/goatos-android/.gitignore`,
so a real per-machine/CI-provisioned file can sit at any of these paths without ever being
committed). That means a fresh clone has NONE of these files by default, and
`:app:processStgReleaseGoogleServices` (a dependency of `:app:compileStgReleaseKotlin` /
`:app:testStgReleaseUnitTest`, i.e. `make ci-local JOB=android`) fails with `File
google-services.json is missing.` The files below are force-committed
(`git add -f`) placeholders that fix that, one per flavor the build needs.

| Flavor | File | Status | Firebase project |
|---|---|---|---|
| `stg` | `app/src/stg/google-services.json` | **Real staging config** for package `sg.mesha.goatos.stg` | `goatos-stg` |
| `dev` | `app/src/dev/google-services.json` | **Placeholder** — schema-valid, fake ids (force-committed so `assembleDevDebug` / `android-dev-run` works) | not confirmed |
| `prod` | `app/src/prod/google-services.json` | **Real production-facing config** for package `sg.mesha.goatos`; Firebase/GCP project id still remains `goatos-stg` for this reused-project path | `goatos-stg` |

## stg: historical staging flavor config

`app/src/stg/google-services.json` is the real, non-secret Firebase Android client config for
the `goatos-stg` project and package `sg.mesha.goatos.stg`. It must match the Firebase Android
app used by `firebaseAppDistribution.appId` in `app/build.gradle.kts`.

The stg Android API host is intentionally the API hostname, not the dashboard HTML host and not
the raw Cloud Run URL:

```text
BuildConfig.API_BASE_URL = https://stg-api.dashboard.mesha.sg/
Dashboard/Auth continue URL = https://stg.dashboard.mesha.sg/login
```

`stg-api.dashboard.mesha.sg` is a DNS-only Cloudflare A record pointing to the staging Google
external managed HTTPS load balancer IP. The load balancer routes that host to `goatos-api-stg`;
`stg.dashboard.mesha.sg` remains the admin web host.

The production-facing release path does not use those public staging hosts. Its `prod` flavor uses:

```text
BuildConfig.API_BASE_URL = https://api.goatos.mesha.sg/
Dashboard/Auth continue URL = https://dashboard.mesha.sg/login
```

## dev placeholder and production-facing prod config

Do not create a new Firebase/GCP project for the current production-facing
GoatOS path. The approved path reuses the existing `goatos-stg` Firebase/GCP
project internally and adds the public package as a separate Android app in
that project:

```text
Firebase app id: 1:514832198871:android:2b3a80736ff2e8d9f19492
Package:         sg.mesha.goatos
Project id:      goatos-stg
Public API:      https://api.goatos.mesha.sg/
Public login:    https://dashboard.mesha.sg/login
```

`app/src/dev/google-services.json` is a placeholder: schema-valid (so the `google-services`
Gradle plugin can process it without failing the build) but with obviously-fake ids
(`000000000000`, `goatos-placeholder`). It lets `dev` assemble cleanly today (`make
android-dev-run` → `:app:assembleDevDebug`) — `BuildConfig.TELEMETRY_ENABLED` is `false` for
`dev` by default (see `app/build.gradle.kts`), so the app never depends on these fake
credentials actually working. `app/src/prod/google-services.json` is the real
public app package (`sg.mesha.goatos`) config downloaded from Firebase and
force-added because the generated file path is gitignored. That keeps the public
app package and URLs free of `stg`; Firebase Auth still uses `goatos-stg`
issuer/audience internally.

**Before turning `TELEMETRY_ENABLED` on for `dev`, or refreshing `prod`:**
1. Confirm the Firebase project in the `vgoats.com` GCP organization — Goat OS is
   Mesha/VGoats-owned, never Heva/Slice (see workspace org-boundary rules).
2. Register an Android app in that Firebase project with package name
   `sg.mesha.goatos.dev` for dev. The current prod package `sg.mesha.goatos` is
   already registered in `goatos-stg`; replace it only if the product later
   moves to a separate Firebase project.
3. Download the real `google-services.json` from the Firebase console and
   replace the placeholder/dev file or refresh the prod file (`git add -f`
   again, since the path is gitignored) — do not hand-edit generated values.
4. Flip `TELEMETRY_ENABLED` for that flavor in `app/build.gradle.kts` (or pass
   `-PgoatosTelemetryEnabled=true`).
