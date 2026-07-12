# Goat OS Mobile (Android) — Documentation Index

This directory is the **source of truth for the Goat OS Android app** — one common,
role-aware app for the field operator **and** leadership (native **Kotlin +
Jetpack Compose**), to be built to the finalized prototype mock
`mock/vaccination-mobile-mock.html`.

Read these before writing any Android code. Nothing here is implemented yet —
this is the pre-implementation design set the maintainer asked for.

## Documents

| Doc | What it covers |
|-----|----------------|
| [prd-operator-mobile.md](prd-operator-mobile.md) | Product requirements: who, what, scope, non-goals, success metrics, constraints |
| [trd-operator-mobile.md](trd-operator-mobile.md) | Technical requirements: stack, Gradle modules, Clean Architecture layers, offline sync engine, hardware (RFID/camera), auth/RBAC, Firebase integration, security, testing, CI |
| [system-design.md](system-design.md) | Runtime architecture, data flow, sync state machine, threading model, push/notification flow, analytics taxonomy, boot/bootstrap contract |
| [design-system.md](design-system.md) | Design tokens, Compose theme, component inventory (ported from the mock), motion, dark/light, accessibility, i18n |
| [screens.md](screens.md) | Screen-by-screen spec: every mock view/overlay → Compose destination, state, backend contract, role visibility |
| [performance-and-memory.md](performance-and-memory.md) | Low-end-device budgets, Compose recomposition-safety cheat sheet, memory-leak prevention checklist, profiling gates |
| [backend-driven-config.md](backend-driven-config.md) | Bootstrap-as-live-config: nav/labels/flags/tunables/module kill-switch pushed on the fly (no APK), config API (ETag/revision), Room/DataStore cache, FCM config-ping |
| [rfid-keyboard-reader.md](rfid-keyboard-reader.md) | RFID reader V1 rule: Bluetooth HID keyboard-wedge capture, Android 10+ permission/status matrix, no edit box/soft keyboard, SDK/BLE optional later |
| [firebase-india-setup.md](firebase-india-setup.md) | Runbook to set up Firebase (Analytics, Performance, Crashlytics, FCM push, Remote Config) under the correct org, using `asia-south1` for location-selectable resources — GA4/Crashlytics/Perf/FCM are global — **gated, not yet executed** |
| [extensibility-future-modules.md](extensibility-future-modules.md) | How new modules/verticals plug in without a rewrite; module registry + navigation contract |

Decision records: [`mobile-native-kotlin.md`](../decisions/mobile-native-kotlin.md)
(client tech) · [`mobile-app-id-and-flavors.md`](../decisions/mobile-app-id-and-flavors.md)
(app id, namespace, env-only flavors, runtime roles).

## Direction change flagged (maintainer-lock)

`context/frontend/final-frontend-mobile-backend-architecture.md` and the existing
`apps/operator-mobile/` scaffold describe a **React Native / TypeScript** mobile
app. The maintainer has since chosen **native Kotlin + Jetpack Compose** for the
Goat OS mobile app (low-end Android target, heavy BLE/RFID + camera hardware,
Android-only field staff). That decision **supersedes the RN direction** and is
recorded in the ADR above; the arch doc's mobile section carries a superseding
note pointing here.

Open item for the maintainer: the existing TypeScript `apps/operator-mobile/`
scaffold (+ `packages/mobile-forms-runner`) is now superseded. It is **left in
place, untouched** until you decide to remove or archive it — this doc set does
not delete it. The new Kotlin app lands at `apps/goatos-android/` (see TRD) with
app id `sg.mesha.goatos` (see the app-id + flavors ADR).

## What does NOT change

The Kotlin pivot is a **client-technology** decision only. Every backend and
platform contract from the existing architecture still holds and is reused
verbatim:

- Backend-driven UI contract: the app is a **renderer**, not product truth.
  Nav, labels, filters, disabled reasons, summary/detail field sets come from
  the backend bootstrap contract (mobile equivalent of `/admin-web/bootstrap`).
- Data access only through the app-api via generated clients — never Firestore,
  BigQuery, Sheets, GCS, or Postgres directly.
- Ports/adapters, OpenAPI REST/JSON, signed-media upload, outbox, idempotency,
  server-authoritative RBAC, `Asia/Kolkata` business time, operational-kernel
  semantics.
- The mock is the **visual/layout/interaction** source of truth (port its
  structure, not a plainer substitute) — but the **backend contract owns data,
  labels, actions, statuses, and options**; never copy the mock's sample
  state/labels/actions as implementation truth.

## Status

```text
docs           : authored (this set)
firebase app   : NOT created (gated on org verification + explicit go)
android app    : SKELETON (§13 step 3) — apps/goatos-android/ scaffolded &
                 compiling: 22 Gradle modules, Clean-Arch boundaries, theme, and
                 the backend-driven nav-chrome shell (FakeAppApi → bootstrap →
                 GoatOsShell) build to an APK. Next: Hilt, Room+DataStore+sync,
                 Retrofit/OpenAPI client, real screens, RFID/CameraX adapters,
                 Firebase (gated). See apps/goatos-android/README.md + MODULE-MAP.md
app id         : sg.mesha.goatos (prod) · .dev · .stg — env-only flavors, roles
                 are runtime from /app/bootstrap (see app-id + flavors ADR)
sdk / toolchain: minSdk 29 (Android 10 — locked) · compileSdk/targetSdk 36
                 (Android 16) · JDK 21 Gradle runtime · Java/Kotlin target 17
versions       : Kotlin 2.4.0 · AGP 9.2 / Gradle 9.6.1 · Compose BOM 2026.06.01 ·
                 Room 2.8 · Coroutines 1.11 · Hilt 2.60.1 · WorkManager 2.11.2 ·
                 CameraX 1.6.1 · Nav-Compose 2.9 — known-good baseline, NOT "current
                 latest"; pin exact stable at scaffold vs official Maven / release
                 notes (TRD §1/§2)
backend mobile : baseline app-api EXISTS (/app/bootstrap, /app/devices/register,
                 /app/proofs/*, /app/tasks/*/submissions, /vaccination/execution/
                 sheds/{id}, /calendar/vaccination/events, leadership reads).
                 Mock-shaped shed-first mobile DELTAS not yet specified as OpenAPI
                 (today's-sheds-by-drive, scan roster, shed submit, reschedule,
                 assign, role-scoped follow-up status) — additive pass, per TRD.
```
