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
| `stg` | `app/src/stg/google-services.json` | **Placeholder** — schema-valid, fake ids (force-committed so fresh-clone/CI compile works) | not asserted here |
| `dev` | `app/src/dev/google-services.json` | **Placeholder** — schema-valid, fake ids (force-committed so `assembleDevDebug` / `android-dev-run` works) | not confirmed |
| `prod` | `app/src/prod/google-services.json` | Not created — prod is never compiled by CI or `android-dev-run` | not confirmed |

## Why placeholders, not the real stg config

`app/src/stg/res/values/firebase.xml` ("Generated-equivalent Firebase options for the
goatos-stg Android client") already carries the real, non-secret `goatos-stg` client values
(`project_id`, `mobilesdk_app_id`, `api_key`, `storage_bucket`, `default_web_client_id`) and is
left untouched — this change never reads from or overwrites it. An earlier pass considered
reconstructing `app/src/stg/google-services.json` from those same values, but that file was
never actually committed (the blanket `google-services.json` gitignore rule silently dropped
it), which is the root cause of the CI gap this change closes. Rather than re-assert a real
project's identifiers from a placeholder-fixing change, `app/src/stg/google-services.json` now
ships as an **obviously-fake placeholder** (`goatos-placeholder` project id, `000000000000`
numbers) — sufficient for the `google-services`/Crashlytics/Perf Gradle plugins to process the
file and for `:app:compileStgReleaseKotlin` / `:app:testStgReleaseUnitTest` to pass, since
`BuildConfig.TELEMETRY_ENABLED` is only `true` for `stg` at runtime, not at compile/unit-test
time. Wiring real stg Firebase Crashlytics/Perf/Auth end-to-end (regenerating this file from the
Firebase console, or from `firebase.xml`'s values, as a deliberate reviewed change) is a
separate follow-up, not done here.

## dev / prod — placeholders, not invented project ids

No Firebase Android app was found registered for `dev` or `prod` anywhere in this repo (no
`firebase.xml`-equivalent, no `firebaseAppDistribution` block, no committed app id). Per the
observability design doc (`docs/observability/OBSERVABILITY_DESIGN.md` §2.5), the intended
convention is **one Firebase project per env flavor** (`goatos-dev` / `goatos-stg` / `goatos-prod`),
matching the `goatos-stg` example above — but `dev` and `prod` project existence is **NOT
verified** in this pass, so this doc does not assert `goatos-dev` / `goatos-prod` as fact.

`app/src/dev/google-services.json` is a placeholder: schema-valid (so the `google-services`
Gradle plugin can process it without failing the build) but with obviously-fake ids
(`000000000000`, `goatos-placeholder`). It lets `dev` assemble cleanly today (`make
android-dev-run` → `:app:assembleDevDebug`) — `BuildConfig.TELEMETRY_ENABLED` is `false` for
`dev` by default (see `app/build.gradle.kts`), so the app never depends on these fake
credentials actually working. `prod` has no placeholder committed — nothing in CI or the dev
scripts compiles the `prod` flavor today, so `app/src/prod/google-services.json` is left for
whoever actually wires a real `goatos-prod` Firebase project to create together with that work.

**Before turning `TELEMETRY_ENABLED` on for `dev`, or building `prod`:**
1. Confirm (or create) the matching Firebase project in the `vgoats.com` GCP organization —
   Goat OS is Mesha/VGoats-owned, never Heva/Slice (see workspace org-boundary rules).
2. Register an Android app in that Firebase project with package name `sg.mesha.goatos.dev`
   (dev) or `sg.mesha.goatos` (prod).
3. Download the real `google-services.json` from the Firebase console and REPLACE the
   placeholder file at the matching path (`git add -f` again, since the path is gitignored) —
   do not hand-edit the placeholder's fake values.
4. Flip `TELEMETRY_ENABLED` for that flavor in `app/build.gradle.kts` (or pass
   `-PgoatosTelemetryEnabled=true`).
