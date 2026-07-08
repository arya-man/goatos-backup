# Goat OS Operator Mobile — Documentation Index

This directory is the **source of truth for the Goat OS Android operator app**
(native **Kotlin + Jetpack Compose**), to be built to the finalized prototype
mock `mock/vaccination-mobile-mock.html`.

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
| [performance-and-memory.md](performance-and-memory.md) | Low-end-device budgets, Compose performance rules, memory-leak prevention checklist, profiling gates |
| [firebase-india-setup.md](firebase-india-setup.md) | Runbook to create the Firebase app in the India region (Analytics, Performance, Crashlytics, FCM push) under the correct org — **gated, not yet executed** |
| [extensibility-future-modules.md](extensibility-future-modules.md) | How new modules/verticals plug in without a rewrite; module registry + navigation contract |

Decision record: [`docs/decisions/mobile-native-kotlin.md`](../decisions/mobile-native-kotlin.md).

## Direction change flagged (maintainer-lock)

`context/frontend/final-frontend-mobile-backend-architecture.md` and the existing
`apps/operator-mobile/` scaffold describe a **React Native / TypeScript** operator
app. The maintainer has since chosen **native Kotlin + Jetpack Compose** for the
operator app (low-end Android target, heavy BLE/RFID + camera hardware,
Android-only operators). That decision **supersedes the RN direction for the
operator app** and is recorded in the ADR above; the arch doc's mobile section
carries a superseding note pointing here.

Open item for the maintainer: the existing TypeScript `apps/operator-mobile/`
scaffold (+ `packages/mobile-forms-runner`) is now superseded. It is **left in
place, untouched** until you decide to remove or archive it — this doc set does
not delete it. The new Kotlin app lands at `apps/operator-android/` (see TRD).

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
- The mock is the **only UI/UX source of truth**; port its structure, not a
  plainer substitute.

## Status

```text
docs           : authored (this set)
firebase app   : NOT created (gated on org verification + explicit go)
android app    : NOT started (apps/operator-android does not exist yet)
backend mobile : app-api mobile bootstrap + submit contracts NOT yet specified
                 as OpenAPI (tracked as first implementation task in the TRD)
```
