# Firebase Setup (India Region) — Goat OS Mobile (Android)

Runbook to wire Firebase **Analytics, Performance Monitoring, Crashlytics, and
Cloud Messaging (FCM)** for the Android app, with data in the **India region**,
under the correct Mesha/VGoats org.

> **GATED — NOT YET EXECUTED.** Creating/altering cloud resources is an
> outward mutation. Nothing in this runbook has been run. Execute only after the
> §1 org-boundary checklist passes and the maintainer says go. The Firebase
> console screenshot shows `goatos-dev` (and legacy `goatos-sheets` — leave
> untouched); this runbook adds the Android app + services to the `goatos-*`
> projects.

## 1. Org-boundary checklist (MUST pass before any command)

Goat OS = Mesha/VGoats only. Never Heva/Slice/`hevaplatform`. Before any
`firebase`/`gcloud` action, verify and state out loud:

```text
[ ] Active gcloud account is the Mesha/VGoats identity (ravi@mesha.sg), not Heva/Slice
[ ] Organization = vgoats.com
[ ] Target project = goatos-dev  (later goatos-stg / goatos-prod)   NOT goatos-sheets
[ ] Firebase project belongs to the vgoats.com org / goat-os folder
[ ] gcloud config: `gcloud config list` shows the above; `gcloud auth list` correct
```

If any line is wrong, STOP and fix context (org-boundary rule in AGENTS.md).

## 2. Region / data-residency decision

- **Default GCP resources location** for each `goatos-*` project: `asia-south1`
  (Mumbai). Set once per project and **immutable** after — verify before create.
- **Cloud Storage (proof media buckets)** and any **Firestore** (not used by the
  app for product data, but if enabled for Firebase internals): `asia-south1`.
- **Google Analytics (GA4)**: data collection is global; GA4 does not offer a
  strict India-only storage guarantee, but set the property's country/reporting
  to India and disable Google-signals data sharing not needed. Product analytics
  carry no PII (goat/shed ids only). Document this residency nuance for review.
- **Crashlytics / Performance / FCM**: global Google services (no per-region
  store); acceptable — they carry crash/latency/token data, no PII, no secrets.
- Operational truth stays in **Cloud SQL Postgres in India** (backend), not
  Firebase. Firebase here is telemetry + push only.

## 3. Create steps (per project: dev → stg → prod)

Do NOT run until §1 passes. Prefer the console for the one-time app registration;
CLI shown for reproducibility.

```bash
# 0. context (READ-ONLY — safe to run to VERIFY)
gcloud auth list
gcloud config list
gcloud projects describe goatos-dev            # confirm org/parent = vgoats.com

# 1. ensure Firebase is on the project (goatos-dev already appears in console)
firebase projects:list                         # confirm goatos-dev present, correct login

# 2. register the Android app (console: Project settings → Add app → Android)
#    package name:  sg.mesha.goatos.dev   (namespace sg.mesha.goatos; prod = no suffix)
#    app nickname:  Goat OS Dev
#    → download google-services.json
firebase apps:create ANDROID "Goat OS Dev" \
  --project goatos-dev --package-name sg.mesha.goatos.dev

# 3. fetch config for the flavor
firebase apps:sdkconfig ANDROID <APP_ID> --project goatos-dev \
  > apps/goatos-android/app/src/dev/google-services.json
```

Repeat for `goatos-stg` (`.stg`) and `goatos-prod` (no suffix) when those
projects exist. Each flavor gets its own `google-services.json` under
`app/src/<flavor>/`.

## 4. Enable services

Console (or Firebase Management API) per project:

- **Analytics**: enable GA4 for the Android app; India reporting; link to the
  project's GA4 property.
- **Performance Monitoring**: enable; the SDK auto-captures cold start, screen
  render, and network traces + our custom traces (`scan_tap_feedback`,
  `submit_roundtrip`, `shed_list_scroll`).
- **Crashlytics**: enable; upload mapping files from release builds (Gradle
  plugin does this); custom keys = role/scope/shed id (never tokens).
- **Cloud Messaging (FCM)**: enable; server sends push via backend
  `NotificationGateway` FCM adapter. Device registers its token through the
  mobile bootstrap `register-device` endpoint — the app does not own push policy.

## 5. Gradle wiring (in the Android app, when it exists)

```text
gradle/libs.versions.toml   → firebase-bom, gms-google-services, crashlytics,
                              perf plugins (pinned)
app/build.gradle.kts        → plugins: com.google.gms.google-services,
                              com.google.firebase.crashlytics,
                              com.google.firebase.firebase-perf
dependencies                → platform(firebase-bom) + analytics-ktx,
                              perf-ktx, crashlytics-ktx, messaging-ktx
flavors (dev/stg/prod)      → each reads its own google-services.json + app-api base URL
```

- Firebase SDKs are called **only inside adapters** behind ports
  (`AnalyticsPort`, `CrashReportingPort`, `PushTokenPort`), never from feature
  code (repo rule: no vendor SDK spread). Each port has a **no-op fake** for
  tests and for builds where telemetry is disabled.
- `google-services.json` is per-project config, not a secret, but do not commit
  prod config into a public path; keep it per-flavor and out of shared modules.

## 6. Privacy / logging

- No PII to Firebase. Goat/shed/RFID ids are operational data and allowed as
  params/keys. Tokens, credentials, service-account JSON are **never** logged or
  sent to Crashlytics/Analytics.
- Analytics collection respects a consent/enable flag from bootstrap (can be
  turned off per environment via the port's fake/no-op).

## 7. Definition of done for this runbook

```text
[ ] §1 checklist verified + stated
[ ] asia-south1 confirmed as project default location (dev/stg/prod)
[ ] Android app registered per flavor; google-services.json per src/<flavor>
[ ] Analytics + Performance + Crashlytics + FCM enabled per project
[ ] backend register-device endpoint accepts the FCM token
[ ] ports + fakes in place; no Firebase SDK call outside adapters
[ ] residency nuance (GA4 global) documented and accepted by maintainer
```
