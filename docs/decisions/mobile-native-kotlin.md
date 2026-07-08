# ADR: Goat OS mobile app is native Kotlin + Jetpack Compose

Status: accepted (maintainer decision, this session) · Supersedes: the "React
Native operator app" direction in
`context/frontend/final-frontend-mobile-backend-architecture.md`. App identity /
id / flavors: see [`mobile-app-id-and-flavors.md`](mobile-app-id-and-flavors.md).

## Context

The Goat OS mobile app (one common, role-aware app for field operator **and**
leadership) is driven by its heaviest user — **health field staff on cheap Android
phones** (target 2–3 GB RAM, weak GPU, old Android, poor connectivity) — to run
vaccination drives. It is **hardware-heavy**: Bluetooth UHF **RFID** handheld
(Chainway-class), **camera** video proof, vibration. The app is **Android only**;
leadership uses the **same app** on Android (read-only role lens) or admin-web on
iPhone (no iOS build).

The prior architecture doc chose **React Native** (reusing `procurement_app`),
and a TypeScript scaffold exists at `apps/operator-mobile/`.

## Decision

Build the operator app as a **native Android app in Kotlin + Jetpack Compose**.

## Why (vs RN and Flutter)

The deciding constraint is *low-end Android + heavy BLE/RFID + camera, Android
only*:

- **Low-end performance**: native has the smallest RAM/cold-start/battery/jank
  footprint on exactly the devices that matter. RN's JS bridge is the weakest
  here; Flutter is good but taxes weak GPUs and adds a runtime floor.
- **RFID SDK reality**: the Chainway-class UHF SDK ships as a **native Android
  `.aar` with Kotlin/Java samples**. Any cross-platform choice (RN/Flutter) still
  requires writing that Kotlin glue behind a bridge/platform-channel — so
  cross-platform buys a bridge cost without removing native code.
- **No iOS need**: operators are Android-only, so cross-platform's main payoff
  (one codebase for iOS+Android) does not apply to this surface.
- **Camera/BLE/vibration** are first-class native (CameraX, BLE, Vibrator).

Flutter would be the fallback **only if** one of these became true: operators get
iPhones, a single codebase for operator+leadership+iOS is mandated, or the team
can hire Flutter but not Kotlin. None hold. RN is not chosen for this profile.

## Scope of this decision

- **Client technology only.** All backend/platform contracts are unchanged and
  reused: ports/adapters, OpenAPI REST/JSON generated clients, backend-driven UI
  bootstrap contract, signed-media upload, outbox, idempotency,
  server-authoritative RBAC, `Asia/Kolkata` business time, operational kernel.
- The app remains a **renderer**, not product truth (golden frontend rule).
- New app module: **`apps/goatos-android/`** (Gradle multi-module; see
  `docs/mobile/trd-operator-mobile.md`).

## Consequences

- `context/frontend/final-frontend-mobile-backend-architecture.md`'s
  "operator-mobile = React Native" is superseded for the operator app; a note
  there points to `docs/mobile/`.
- The existing TypeScript `apps/operator-mobile/` scaffold and
  `packages/mobile-forms-runner` (RN renderer) are **superseded**. They are left
  in place, untouched, pending a maintainer decision to archive/remove. The
  Kotlin app introduces its own Compose forms-runner.
- Reused *ideas* from `procurement_app` (camera recorder UX, upload-queue
  pattern, team/login UX) carry over conceptually, re-implemented natively.

## Open items

- Maintainer to confirm removal/archival of the RN `apps/operator-mobile/`
  scaffold.
- Backend to publish the **mobile bootstrap + shed read/submit/reschedule/assign**
  OpenAPI contracts (first implementation task).
- Firebase India app creation is gated (`docs/mobile/firebase-india-setup.md`).
