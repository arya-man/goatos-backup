# `google-services.json` per flavor

Full detail: `../docs/TELEMETRY.md`. This file is the short pointer next to the actual JSONs.

| Flavor | File | Status | Firebase project |
|---|---|---|---|
| `stg` | `app/src/stg/google-services.json` | **Real**, reconstructed from already-committed values | `goatos-stg` (confirmed) |
| `dev` | `app/src/dev/google-services.json` | **Placeholder** — schema-valid, fake ids | not confirmed |
| `prod` | `app/src/prod/google-services.json` | **Placeholder** — schema-valid, fake ids | not confirmed |

## stg — real, not invented

`app/src/stg/google-services.json` was NOT hand-invented. Every value in it was already
committed in this repo before this change, in `app/src/stg/res/values/firebase.xml`
("Generated-equivalent Firebase options for the goatos-stg Android client") and in
`app/build.gradle.kts`'s `firebaseAppDistribution { appId = ... }` block:

- `project_id` = `goatos-stg`, `project_number`/`gcm_defaultSenderId` = `514832198871`
- `mobilesdk_app_id` = `1:514832198871:android:0cb898377ba4f7f7f19492`
- `api_key` = the committed `google_api_key`
- `storage_bucket` = the committed `google_storage_bucket`
- `oauth_client` web client id = the committed `default_web_client_id`
- `package_name` = `sg.mesha.goatos.stg` (`applicationId` + the `stg` flavor's
  `applicationIdSuffix = ".stg"`)

This file is ADDED, not a replacement — `firebase.xml` is left in place untouched (per the
task rule: never overwrite an existing real Firebase config). The two are equivalent; once the
`com.google.gms.google-services` Gradle plugin (applied in this change) regenerates the same
resources from this JSON on every build, `firebase.xml` becomes redundant and can be removed in
a later, deliberate cleanup — not done here to avoid any risk to the currently-working stg
build/App Distribution pipeline.

## dev / prod — placeholders, not invented project ids

No Firebase Android app was found registered for `dev` or `prod` anywhere in this repo (no
`firebase.xml`-equivalent, no `firebaseAppDistribution` block, no committed app id). Per the
observability design doc (`docs/observability/OBSERVABILITY_DESIGN.md` §2.5), the intended
convention is **one Firebase project per env flavor** (`goatos-dev` / `goatos-stg` / `goatos-prod`),
matching the `goatos-stg` example above — but `dev` and `prod` project existence is **NOT
verified** in this pass, so this doc does not assert `goatos-dev` / `goatos-prod` as fact.

`app/src/dev/google-services.json` and `app/src/prod/google-services.json` are therefore
placeholders: schema-valid (so the `google-services` Gradle plugin can process them without
failing the build) but with obviously-fake ids (`000000000000`, `REPLACE_WITH_REAL_...`). They
let `dev`/`prod` assemble cleanly today — `BuildConfig.TELEMETRY_ENABLED` is `false` for both
flavors by default (see `app/build.gradle.kts`), so the app never depends on these fake
credentials actually working.

**Before turning `TELEMETRY_ENABLED` on for `dev` or `prod`:**
1. Confirm (or create) the matching Firebase project in the `vgoats.com` GCP organization —
   Goat OS is Mesha/VGoats-owned, never Heva/Slice (see workspace org-boundary rules).
2. Register an Android app in that Firebase project with package name `sg.mesha.goatos.dev`
   (dev) or `sg.mesha.goatos` (prod).
3. Download the real `google-services.json` from the Firebase console and REPLACE the
   placeholder file at the matching path — do not hand-edit the placeholder's fake values.
4. Flip `TELEMETRY_ENABLED` for that flavor in `app/build.gradle.kts` (or pass
   `-PgoatosTelemetryEnabled=true`).
