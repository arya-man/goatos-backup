# Backend-Driven Config — Goat OS Mobile (Android)

How the Android app is **configured from the backend** so the maintainer can push
**presentation** changes on the fly (nav, labels, flags, UI tunables, module
availability) **without shipping a new APK**.

**Governing rule: the phone is a dumb renderer.** Backend owns truth, policy,
permissions, scheduling, and all DB/Redis querying + aggregation. The app renders
backend payloads and queues user input/media. It follows that **business/medical
policy is NOT sent to the client to interpret** — the app receives *computed policy
outcomes* (status, labels, allowed date options, disabled reasons), never raw
buffer windows / reminder leads / thresholds to do math on. Pairs with
[TRD](trd-operator-mobile.md) §5/§6 and the golden frontend rule (the app is a
renderer, not product truth).

## Principle

The app renders what the backend contract says. **Visible nav, page/section
titles, table/filter labels, chips/tabs, filter/sort/page-size, row-click params,
drawer/action labels, empty/error copy, disabled reasons, feature flags, and
UI tunables are backend-owned.** The client only owns layout, CSS-equiv
styling, density, focus/hover, and local open/selected state. If it's visible text
or an available action, it comes from config — not a hardcoded constant. Business
rules, medical policy, scheduling, and permission decisions are **computed on the
backend**; the client never re-derives them.

## Source of truth: the bootstrap contract (primary)

`GET /app/bootstrap` is the live document. It carries **three distinct blocks**
that must not be conflated, plus computed policy **outcomes** on the domain reads:

**`presentationConfig`** — freely push-able UI presentation (the "change on the
fly" surface):

- role lens + park/shed scope + grants/capabilities (as data the app renders, not
  logic the app evaluates — see below);
- **visible navigation** + labels + icons(token) + disabled reasons;
- **module registry** — which modules/screens are enabled (kill-switch);
- **feature flags** (per tenant / role / env);
- pinned SOP/form versions + scoped option caches;
- compatible app-version gate;
- a monotonic **`revision`** (and HTTP `ETag`).

**`clientRuntimeConfig`** — client **operational** knobs (not business policy, not
presentation): default page sizes, sync backoff/jitter, refresh cadence, cache
TTLs, jank-sampling rate. Backend-owned and **backend-bounded** (the server clamps
each to a safe min/max), pushed on the same `revision`. These tune how the client
talks to the backend; they never encode a business rule.

**Policy is NOT a client block.** There is deliberately no `policySnapshot` of raw
thresholds for the app to compile. Instead the backend **computes policy outcomes**
and ships them on the relevant reads:

- a dose's `status` (`in_buffer` / `missed` / `due` …) — computed server-side from
  the buffer window + Asia/Kolkata bucketing;
- allowed **reschedule date options**, each with a precomputed state
  (`in_buffer` / `out_of_buffer`) and label;
- disabled reasons, reminder/escalation copy, and the alert lead the backend
  decided.

The only policy field that travels to the client is a read-only **`policy_revision`**
(with `source`, e.g. `vaccination-rules.md` / published `rule_dsl`) echoed **purely
for traceability/telemetry**. The app **never** does buffer/lateness math, never
schedules reminders, and never exposes policy as an editable setting. Policy
changes go through the business/medical maintainer-lock (see `AGENTS.md` "Business
and medical rule changes"), never a config push.

Changing `presentationConfig` / `clientRuntimeConfig` server-side changes the app
on the **next fetch** — no release. Policy outcomes change only when the backend
recomputes them under governed policy updates. Permissions stay
**server-authoritative**: config may hide a control, but the server still enforces
the actual grant on every command (config never widens access), and grants in the
payload are **action/route targets the app maps to screens**, not a client-side
role predicate.

## Config API

```text
GET /app/bootstrap
    If-None-Match: "<etag>"        → 200 {full config, revision, ETag} | 304 Not Modified
GET /app/config?since=<revision>   → 200 {delta since revision} (optional optimization)
```

- **Locale headers**: every mobile API call sends the persisted app language as
  `X-GoatOS-Locale: <en|hi|kn|te>` plus standard `Accept-Language` (for example
  `hi, en;q=0.8`). Backend-owned strings in bootstrap/config/read payloads must
  be composed for that locale, falling back to English only when no supported
  translation exists. Static client-only strings still live in Android locale
  resources.
- **ETag / If-None-Match**: the client caches the ETag; a 304 means "nothing
  changed" (cheap poll). A 200 delivers the new config + revision.
- **Firebase Remote Config** is a *fallback / global kill-switch* only (e.g.
  force-refresh, hard app-min-version, emergency disable). Bootstrap is primary so
  config stays tenant/role-scoped and server-authoritative.
- **Hard app-min-version must have an update path.** A force-update decision is
  valid only when the minimum supported version is positive, the installed build
  is below it, and `update_url` is a valid `http(s)` install URL. Blank,
  malformed, or missing `update_url` must fail open; otherwise Remote Config can
  hard-brick field operators with no actionable CTA.
- **Cold-start gate state is blocking.** The app must render a neutral checking
  state until the first force-update check resolves. It may fail open after an
  error or allowed decision, but it must not render auth/bootstrap/business UI as
  allowed before the first check has run.

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
  reactively. Room is a **durable cache + outbox, not product truth** — the
  backend remains the system of record.
- A new revision **never discards in-flight local work** (the outbox / unsynced
  shed records survive a config refresh).

## What is config-able vs not

| Config-able — pushed on the fly | NOT client-decided |
|---|---|
| `presentationConfig`: nav visibility, page/section titles, labels, chips/tabs, filter/sort/page-size labels, empty/error copy, disabled reasons, icons(token) | Live domain data — parks, sheds, breeds, SOP names, roles (come from module DB tables, compiled by backend) |
| `presentationConfig`: feature flags, per-screen kill-switch, module registry | Actual permissions/RBAC (server-enforced on every command; config only hides UI) |
| `clientRuntimeConfig`: page sizes, sync backoff/jitter, refresh cadence, cache TTLs, jank-sampling (backend-bounded) | **Business/medical policy** — buffer window, reminder lead, medical thresholds, lateness/`missed` math, Asia/Kolkata bucketing — **computed on the backend**; the app gets outcomes (`status`, date options, labels) + a `policy_revision` echo, never raw values |
| — | Scheduling — reminder/escalation **timing** is kernel-owned; the app never times a notification |
| — | Stock/FEFO/expiry lot selection + reservation — server/planner-side; the app shows the backend-selected lot or scans/confirms the physical vial |

Business-managed vocabularies (park codes, shed names, operator IDs, capacities)
are **backend-owned but sourced from Postgres/module tables**, compiled into the
contract by the backend — not hand-typed into config and not hardcoded in the app.

## Safety & rollout

- Config is **schema-validated**; unknown keys are ignored (forward-compatible, so
  an older app tolerates new config).
- Roll out changes behind **flags** (per tenant/role/env) for staged rollout; a
  **kill-switch** flag renders a screen/action disabled-with-reason instead of
  crashing.
- The app treats config as **presentation + bounded runtime knobs only**; every
  mutating action is still revalidated server-side (permissions + form_version +
  current state), and every policy/scheduling decision is computed server-side.

## Observability

Log the applied config **revision**, fetch latency, 304 ratio, and any
schema-validation drop (unknown/invalid keys) — so a bad push is diagnosable. When
a screen shows a policy outcome (`status`, date-option state), telemetry logs the
**backend-provided** value plus its `policy_revision`, never a client-computed
state. See TRD §7/§9.
