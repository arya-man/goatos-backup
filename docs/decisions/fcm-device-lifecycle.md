# ADR: FCM device lifecycle — couple on launch, decouple on logout

Status: ACCEPTED — backend contract shipped; mobile wiring is a tracked TODO gated
on the `goatos-prod` Firebase project (see `docs/decisions/goatos-firebase-project`
context and the founder memory note). Applies to every agent (Claude + Codex) and
human working on `apps/goatos-android`.

References (authoritative): Firebase Cloud Messaging on Android
(https://firebase.google.com/docs/cloud-messaging/android/client) and
"Manage registration tokens"
(https://firebase.google.com/docs/cloud-messaging/manage-tokens).

## Problem

A device's FCM registration token is the address pushes are delivered to. Two
failure modes if its lifecycle is not bound to auth:

1. **Silent non-delivery** — the backend never learns the current token, so
   obligation/escalation pushes go nowhere.
2. **Cross-user push bleed** — a token stays bound to a signed-out user's device;
   the next person to sign in on that device (role switch, shared field phone) can
   receive pushes addressed to the previous operator. This is the device-identity
   twin of the "outbox actor/token bleed" the logout clean-sweep closes
   (`docs/decisions/android-offline-first.md` + the logout wipe).

So the token must be **coupled on launch/login** and **decoupled on logout**.

## Decision

The FCM token binding is a write-path fact owned by the workforce module, carried
on the existing device record (`workforce_member_devices.push_token_hash`). We store
only a HASH of the token, never the raw token at rest.

### Couple — on app launch / successful login

1. Fetch the current token: `FirebaseMessaging.getInstance().token` (suspending).
2. Send it as `push_token_hash` in the existing `registerDevice` call
   (`POST /app/devices/register`, `RegisterDeviceRequest.push_token_hash`). The
   backend upserts the device row keyed by `(tenant, device_id)` and records the
   hash. This is idempotent — re-registering with the same hash is a no-op update.
3. Re-couple on rotation: a `FirebaseMessagingService.onNewToken(token)` re-issues
   `registerDevice` with the new hash so a rotated token never goes stale.

### Decouple — on logout

1. Call `POST /app/devices/{device_id}/deregister` with the stored `deviceId`
   (from `DeviceStore`). Backend behavior (shipped):
   - single indexed `UPDATE workforce_member_devices` scoped by
     `tenant_id + device_id + registered_by = actor` (the caller's OWN device only);
   - `push_token_hash = NULL`, `status = 'revoked'`, `revoked_at = now()`,
     `row_version += 1`, audit `app.device.deregister`;
   - 0 rows affected (unknown device, or one registered by another operator) →
     `404`. Requires only the `AppBootstrap` permission (self-service, not the
     admin operator-scoped `RevokeDevice`).
2. Call `FirebaseMessaging.getInstance().deleteToken()` so the device stops holding
   a live token locally.
3. This runs as part of the logout clean-sweep, alongside the cache + draft +
   outbox + auth-token wipe (see the logout clean-sweep — "wipe all, always").
   The deregister call is **best-effort**: a network failure must not block local
   sign-out (the local wipe still proceeds, and the server-side token is
   additionally invalidated when the auth session ends).

## Status / what is done vs TODO

| Piece | State |
| --- | --- |
| Backend `push_token_hash` on `registerDevice` | DONE (pre-existing: `workforce/domain/types.go`, `RegisterDevice` upsert) |
| Backend `POST /app/devices/{device_id}/deregister` | **DONE** — `deregisterAppDevice`, workforce app/adapters + `app-api.yaml` |
| Mobile: send `push_token_hash` on register (launch) | TODO — needs the FirebaseMessaging SDK in the app (none present today) |
| Mobile: `onNewToken` re-register | TODO — needs `FirebaseMessagingService` |
| Mobile: call deregister + `deleteToken()` on logout | TODO — wire into `SessionViewModel.signOut()` with `DeviceStore.deviceId` |
| FCM message delivery | GATED on `goatos-prod` Firebase (India delivery) — not wired, device-untestable now |

The mobile pieces are one coherent unit and land with the offline-first mobile body
(they touch `SessionViewModel`, which the logout clean-sweep also edits). They are
deliberately NOT shipped as a partial edit on the current tree to avoid a
half-coupled token path (a decouple call for a token that was never coupled is a
no-op) and a merge conflict with the in-flight sign-out changes.

## Consequences

- The server always has the current token hash (delivery works) and drops it the
  instant a user signs out (no bleed).
- Storing only the hash keeps the raw token out of the DB — consistent with the
  "no ID token at rest" hardening already in the Firebase auth flow.
- Until the mobile SDK wiring lands, pushes are not delivered on-device; the
  backend contract is ready so the mobile unit is purely additive when Firebase is
  un-gated on `goatos-prod`.
