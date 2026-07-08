# Backend-Driven Config — Goat OS Mobile (Android)

How the Android app is **configured from the backend** so the maintainer can push
changes on the fly (nav, labels, flags, thresholds, module availability) **without
shipping a new APK**. Pairs with [TRD](trd-operator-mobile.md) §5/§6 and the golden
frontend rule (the app is a renderer, not product truth).

## Principle

The app renders what the backend contract says. **Visible nav, page/section
titles, table/filter labels, chips/tabs, filter/sort/page-size, row-click params,
drawer/action labels, empty/error copy, disabled reasons, feature flags, and
tunable thresholds are backend-owned.** The client only owns layout, CSS-equiv
styling, density, focus/hover, and local open/selected state. If it's visible text
or an available action, it comes from config — not a hardcoded constant.

## Source of truth: the bootstrap contract (primary)

`GET /app/bootstrap` is the live document. It carries **two distinct blocks** that
must not be conflated:

**`presentationConfig`** — freely push-able UI config (this is the "change on the
fly" surface):

- role lens + park/shed scope + grants/capabilities;
- **visible navigation** + labels + icons(token) + disabled reasons;
- **module registry** — which modules/screens are enabled (kill-switch);
- **feature flags** (per tenant / role / env);
- **UI tunables ONLY** — default page sizes, sync backoff/jitter, jank-sampling
  rate, refresh cadence, cache TTLs;
- pinned SOP/form versions + scoped option caches;
- compatible app-version gate;
- a monotonic **`revision`** (and HTTP `ETag`).

**`policySnapshot`** — read-only business/medical policy the app **renders but
never treats as tweakable UI config**: reschedule **buffer window**, **reminder
lead time**, and any medical thresholds. It is NOT a casual config push. It has its
own **`policy_revision`**, a **`source`** (e.g. `vaccination-rules.md` / published
`rule_dsl`), and an **audit trail**; changing it goes through the business/medical
maintainer-lock governance (see `AGENTS.md` "Business and medical rule changes"),
not a UI config edit. The app compiles it into behaviour (e.g. buffer math) but
must not expose it as an editable setting.

Changing `presentationConfig` server-side changes the app on the **next fetch** —
no release. `policySnapshot` changes only via governed policy updates. Permissions
stay **server-authoritative**: config may hide a control, but the server still
enforces the actual grant (config never widens access).

## Config API

```text
GET /app/bootstrap
    If-None-Match: "<etag>"        → 200 {full config, revision, ETag} | 304 Not Modified
GET /app/config?since=<revision>   → 200 {delta since revision} (optional optimization)
```

- **ETag / If-None-Match**: the client caches the ETag; a 304 means "nothing
  changed" (cheap poll). A 200 delivers the new config + revision.
- **Firebase Remote Config** is a *fallback / global kill-switch* only (e.g.
  force-refresh, hard app-min-version, emergency disable). Bootstrap is primary so
  config stays tenant/role-scoped and server-authoritative.

## When the app fetches config

1. **Cold start** —
   - **First-ever launch with no cached config**: block business UI until the
     first bootstrap succeeds (no local-default flash; matches the admin-web rule).
     Offline on a fresh install → a "connect to finish setup" state, not a guess.
   - **Later launches with a valid cache**: render the cached config immediately
     (works offline) **if** the app-version + config-schema gates pass, then
     refresh in the background (ETag) and re-render on a new revision. A failed
     gate falls back to the blocking bootstrap.
2. **On resume** (app foregrounded) — conditional GET (ETag); apply if changed.
3. **Pull-to-refresh** on read screens (TRD §6 refresh).
4. **Push-on-the-fly** — backend sends an **FCM data message** `config_changed`
   (with the new revision); the app silently re-fetches bootstrap and re-renders
   nav/labels/flags. This is the "push a change and it lands on device" path.

## Room / DataStore cache

- The whole bootstrap/config is **persisted locally** (Proto DataStore for the
  config blob + `revision`/ETag; Room for the scoped option caches and all
  operational reads — sheds, roster, records, outbox).
- The app is **cache-first**: it renders from the persisted config immediately
  (works offline), then refreshes in the background and re-renders on a new
  revision. Room queries expose `Flow`, so a config/data refresh recomposes the UI
  reactively.
- A new revision **never discards in-flight local work** (the outbox / unsynced
  shed records survive a config refresh).

## What is config-able vs not

| Config-able — `presentationConfig` (push on the fly) | NOT freely config-able |
|---|---|
| Nav visibility, page/section titles, labels | Live domain data — parks, sheds, breeds, SOP names, roles (come from module DB tables) |
| Chips/tabs, filter/sort/page-size semantics | Actual permissions/RBAC (server-enforced; config only hides UI) |
| Empty/error copy, disabled reasons | **Business/medical policy** — reschedule buffer window, reminder lead, medical thresholds — lives in read-only `policySnapshot` (own `policy_revision` + source + audit; governed by the maintainer-lock, not a UI push) |
| Feature flags, per-screen kill-switch, module registry | Business/medical *rules* that need a source-of-truth change (vaccination-rules.md, migrations) |
| **UI tunables** — page sizes, sync backoff, refresh cadence, cache TTLs, jank-sampling | Anything that would let the client widen its own access |

Business-managed vocabularies (park codes, shed names, operator IDs, capacities)
are **backend-owned but sourced from Postgres/module tables**, compiled into the
contract by the backend — not hand-typed into config and not hardcoded in the app.

## Safety & rollout

- Config is **schema-validated**; unknown keys are ignored (forward-compatible, so
  an older app tolerates new config).
- Roll out changes behind **flags** (per tenant/role/env) for staged rollout; a
  **kill-switch** flag renders a screen/action disabled-with-reason instead of
  crashing.
- The app treats config as **presentation only**; every mutating action is still
  revalidated server-side (permissions + form_version + current state).

## Observability

Log the applied config **revision**, fetch latency, 304 ratio, and any
schema-validation drop (unknown/invalid keys) — so a bad push is diagnosable.
See TRD §7/§9.
