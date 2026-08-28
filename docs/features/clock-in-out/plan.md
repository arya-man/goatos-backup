# Clock In / Clock Out — Implementation Plan

Status: **GREEN-SIGNALED 2026-08-28; IMPLEMENTATION IN PROGRESS** on
`feat/clock-in-out` (worktree `wt-clock`). Backend core, page contract, and
the admin-web tab are built and tested; see §11 for the implementation record
and the two deviations decided during the build.

Builds on top of PR #126 (`feat/hrms-per-person-access` — person × module ×
surface × capability ticks) and the People/HRMS rewrite on
`feat/hrms-people-rewrite`.

---

## 1. What we are building (product statement)

Every person starts their working day by **clocking in** on the phone and ends
it by **clocking out**. From that pair the backend owns one number: **hours
worked that business day (IST)**.

- **Clock In / Out is its own phone module** — a separate entry in the module
  drawer / bottom bar, not a tab inside another module.
- Clock-in captures **everything the device can honestly tell us**: GPS
  fix + accuracy + reverse-geocoded address, full device identity (the existing
  `X-GoatOS-*` header set plus device registry row), app version, OS, network
  type, battery, device-vs-server clock skew, developer-options state, and a
  **mock-location verdict**.
- **Fake-GPS apps are a hard block.** If the location fix is mock-provided or a
  mock-location app is installed on the phone, clock-in is REFUSED on the
  client AND the server, with farm-worded copy telling the person to remove
  that app first. No override.
- **Forgot-to-clock-in reminder:** any screen of the app used while today has
  no clock-in shows a persistent shell-global banner ("You haven't clocked in
  today — tap to clock in"), exactly like the offline banner mechanism but
  with backend-owned copy. It disappears the moment clock-in lands.
- **Presence board for leadership (added 2026-08-28):** a second page inside
  the phone clock module, visible only to CXO/leadership (`clock.read`
  tick) — open today's date and see the whole roster grouped into *Working
  now / Clocked out / Not clocked in*, with times, live durations, name
  search, and Park + Designation filters. Full UI spec in §4.4.
- **Admin-web:** the `/people` (People / HRMS) page gets a new **Clock In /
  Out tab** listing every person's clockings — in/out times, hours, location,
  device, integrity flags — plus a "clocked in today?" status chip on the All
  People table. Row click opens a same-page drawer with the full captured
  detail of that clocking.

Explicitly **out of scope for V1** (each needs its own maintainer decision
later): payroll/salary math (kernel lock: the kernel never calculates salary),
leave integration with `workforce_absences`, roster-vs-actual variance
reports, escalation ladders for chronic non-clockers, iOS/web clock-in.

---

## 2. Why this shape (grounded in what exists today)

Verified facts from the tree (2026-08-27):

- **No attendance concept exists.** No table, route, or screen for
  clock-in/out, punch, or timesheet anywhere. `workforce_roster_assignments`
  is *planned* shifts; `workforce_absences` is leave. Nothing records actual
  arrival/departure. This is a green-field module — no migration of legacy
  behavior.
- **GPS capture exists** but only for proof overlays:
  `apps/goatos-android/.../capture/AppProofLocationProvider.kt` returns
  `ProofLocationSnapshot(locationStatus, lat, lng, gpsAccuracyM, address)`
  via platform `LocationManager`. We reuse this provider, extended with the
  mock verdict.
- **Mock-location / root / developer-mode detection: none exists.** All new.
- **Device identity is already rich:** every request carries
  `X-GoatOS-Device-Id`, app version/code, build type, OS, SDK, model
  (`backend/internal/platform/httpmiddleware/request_context.go`), and
  `workforce_member_devices` registers each phone. Clock events join to both.
- **The write pattern is settled:** Android outbox (`OutboxEntity`,
  `SyncEngine.dispatch`) + backend idempotency
  (`workforce/adapters/postgres/idempotency.go`: key reserved in the write
  txn with a semantic fingerprint, snapshot replayed on retry).
- **The banner pattern is settled:** `OfflineBanner` is shell-global
  (`GoatOsShell.kt`); `CoverageBanner` is the backend-owned-copy dumb
  renderer (`text` is its only field). The clock reminder is the marriage of
  the two: shell-global placement, backend-owned copy.
- **Module registration is settled:** one `moduleNavRegistry` entry in
  `workforce/app/bootstrap_copy.go` + label keys (en/hi/kn/te) + permission
  constants + routes + hosted `composable` routes in `AppNavHost.kt`.
- **PR #126 changes who gets a module:** access is now per-person ticks, and
  its own bug ledger records "silent module loss" when a module key is missing
  from the catalog. The clock module MUST be in the capability catalog from
  day one — see §7.

---

## 3. Backend design

### 3.1 Data model (new migration `0002xx_workforce_clock.sql`)

Two tables: an immutable event log, and a paired day-entry read model kept in
sync **inside the same transaction** (atomic transition + owned read model,
per the standing rule).

```sql
workforce_clock_events (          -- append-only, one row per punch
  clock_event_id    uuid PK,
  tenant_id         uuid NOT NULL,
  workforce_member_id uuid NOT NULL,
  user_id           uuid NOT NULL,          -- auth subject at punch time
  event_type        text CHECK (event_type IN ('clock_in','clock_out')),
  business_date     date NOT NULL,          -- IST day, derived server-side
  captured_at       timestamptz NOT NULL,   -- device clock at tap
  recorded_at       timestamptz NOT NULL DEFAULT now(),  -- server clock
  clock_skew_ms     bigint,                 -- recorded_at - captured_at
  -- location
  location_status   text NOT NULL,          -- captured | permission_missing | unavailable
  latitude          double precision,
  longitude         double precision,
  gps_accuracy_m    double precision,
  address           text,
  -- integrity
  mock_location     boolean NOT NULL DEFAULT false,
  mock_provider_packages text[],            -- installed fake-GPS apps found
  developer_options_enabled boolean,
  integrity_verdict text NOT NULL,          -- clean | mock_refused (server echo)
  -- device (denormalized snapshot; canonical row is workforce_member_devices)
  device_id         uuid,
  app_install_id    text,
  app_version       text,
  app_version_code  text,
  build_type        text,
  os_version        text,
  sdk_version       text,
  device_model      text,
  network_type      text,                   -- wifi | cellular | offline_queued
  battery_pct       smallint,
  metadata          jsonb NOT NULL DEFAULT '{}'
);

workforce_clock_entries (         -- one row per person per business day (pairing read model)
  clock_entry_id    uuid PK,
  tenant_id         uuid NOT NULL,
  workforce_member_id uuid NOT NULL,
  business_date     date NOT NULL,
  clock_in_event_id uuid NOT NULL REFERENCES workforce_clock_events,
  clock_out_event_id uuid REFERENCES workforce_clock_events,  -- NULL while open
  clock_in_at       timestamptz NOT NULL,   -- server recorded_at
  clock_out_at      timestamptz,
  worked_minutes    integer,                -- backend-owned; NULL while open
  status            text NOT NULL CHECK (status IN ('open','closed','auto_closed')),
  row_version       bigint NOT NULL DEFAULT 1,
  UNIQUE (tenant_id, workforce_member_id, business_date)   -- see open decision Q4
);
```

Grain proof (projection-review discipline): producer grain = one event row per
punch; consumer grain = one entry row per `(tenant, member, business_date)`;
`worked_minutes` numerator and denominator range over the same single entry
row — no join fan-out possible. Indexes: events on
`(tenant_id, business_date, workforce_member_id)`; entries unique key above
plus `(tenant_id, business_date)` for the admin list.

Business-day rule: `business_date` derives from `recorded_at` via
`biztime.BusinessDate` (Asia/Kolkata) **on the server** — never trusted from
the client (device clock can lie; `clock_skew_ms` records by how much). An
offline-queued punch is the one exception, handled per open decision Q1.

### 3.2 Permissions & routes

Dedicated permissions (per the feed-purchase precedent — never reuse a broader
key): `clock.self` (punch in/out, read own status — every app principal),
`clock.read` (admin-web view of everyone's clockings).

```
POST /app/clock/in        clock.self    (idempotent; body carries the capture)
POST /app/clock/out       clock.self
GET  /app/clock/status    clock.self    (today's state + backend-owned banner copy)
GET  /admin/workforce/clock-entries    clock.read   (keyset-paginated, filters: date range, park, department, person, integrity flag)
GET  /admin/workforce/clock-entries/{id}  clock.read (full event detail for the drawer)
```

Server-side integrity gate: a clock-in whose payload carries
`mock_location=true` or a non-empty `mock_provider_packages` is REFUSED with
422 `mock_location_detected` (farm-worded detail). The client blocks first,
but the server refuses independently so a tampered client cannot skip it.
A punch with `location_status != captured` is **accepted and flagged**, never
refused — GPS genuinely fails indoors; refusing would block honest people
(mirrors weighing's "counted, never rejected" philosophy). Only *proven
dishonesty* (mock) refuses.

### 3.3 Domain events / audit

V1 writes `audit_log` rows (with the standard client block) in the write txn.
**No outbox events yet**: there is no consumer, and the standing rule bans a
producer with no consumer as a silent drop. When a real consumer arrives
(e.g. leadership daily-attendance notification), `workforce.clock.in.recorded`
/ `.out.recorded` get registered properly across all five registration points
(envelope enums, outbox validator, both buses, registry JSON) in that change.
The domain-event registry gets a row documenting exactly this deferral.

### 3.4 Leadership assistant coverage

New tables ⇒ same-change coverage artifact: a `ceo_ai.attendance_daily` view
(person, park, date, in/out, hours, flags) + coverage-matrix row, so "who
clocked in late today?" is answerable in CEO chat. (Guard:
`leadership-assistant-coverage-guard` forces this anyway.)

---

## 4. Android design

### 4.1 New module

- Module key **`clock`**, label **"Clock In / Out"** (all four locales),
  `moduleNavRegistry` entry, `landingHref: "clock"` hosted in `AppNavHost.kt`.
  New gradle module `feature-clock` (feature → core only).
- One screen, deliberately simple: today's state (big Clock In button, or
  "Clocked in at 08:12 — working 3h 40m" with a Clock Out button), and below
  it the person's own recent days (backend-paginated, ~20). All copy
  backend-owned via the module contract / `GET /app/clock/status`.
- On tap: capture location (reuse `AppProofLocationProvider`, extended),
  gather device/battery/network facts, run the mock-location check, then
  enqueue the outbox op (`CLOCK_IN` / `CLOCK_OUT` — new `OutboxOpType`
  values; stable idempotency key `clock:<member>:<business_date>:<in|out>`
  persisted in `SavedStateHandle`; `groupKey` = member id so punches drain in
  order). Room caches status per offline-first rules; `RefreshOnResume` +
  `SyncIconButton` as on every read screen.

### 4.2 Mock-location detection (all new code)

Layered, strictest-available per OS level:

1. **The fix itself:** `Location.isMock` (API 31+) / `isFromMockProvider`
   (below) on the captured fix — if true, the punch is refused outright.
2. **Installed fake-GPS apps:** `PackageManager` scan for installed packages
   requesting `android.permission.ACCESS_MOCK_LOCATION` (the permission every
   mock-provider app must declare). Requires the `QUERY_ALL_PACKAGES`
   manifest permission — acceptable for this app (internal distribution:
   Firebase App Distribution + `mesha.sg/app.apk` + Play *internal* testing;
   flagged here as a recorded Play-policy consideration if the app ever goes
   to open Play tracks). Found packages are named to the user ("Remove
   *Fake GPS Location* to clock in") and sent to the server in
   `mock_provider_packages`.
3. **Context signals (recorded, not blocking):** developer options enabled,
   ADB enabled — stored as flags for the admin view, because blocking on
   developer options would lock out our own dev/test devices.
4. **Recheck at punch time, every time.** Not a one-time install check — the
   scan runs inside the clock-in tap path, so installing a fake-GPS app after
   a clean first day still blocks the next punch.
5. **Future hardening (not V1):** Play Integrity API verdict attached to each
   punch. Listed so the metadata jsonb leaves room for it.

Client refusal UX: a full-screen farm-worded block (backend-owned copy) naming
the offending app(s), with a "Check again" button that re-runs the scan after
the person uninstalls. Copy firewall applies — no words like "mock", "GPS
spoofing API", or package internals in the visible copy beyond the app's
human label.

### 4.3 The not-clocked-in reminder banner

- Shell-global, mounted next to `OfflineBanner` in `GoatOsShell.kt` so it
  shows on **every** screen, every module.
- Driven by a `ClockStatusViewModel` observing the Room-cached
  `GET /app/clock/status` (refreshed on resume + after every sync drain).
  Shows only when: today (IST) has no open/closed entry AND the person is
  clock-required (see open decision Q2).
- Copy is backend-owned (the status payload carries `banner_text`), rendered
  by a dumb `ClockReminderBanner` (CoverageBanner pattern: `text` +
  `onTap → clock module`). Never composed client-side.
- It is a **reminder, not a lock**: the app stays fully usable. (Locking the
  app behind clock-in punishes GPS-dead mornings and emergencies; the
  maintainer can escalate later if reminding proves insufficient.)

### 4.4 Presence board — the leadership page inside the clock module (added 2026-08-28)

The clock module has **two pages**, split by capability (page-grain access,
exactly the PR-#126 model):

- **My Clock** — the punch page (§4.1). Everyone (`clock.self`).
- **Team** — the presence board. Leadership/CXO only (`clock.read` tick);
  a person without the tick never sees the page, per the offered-only-when-
  openable rule.

The presence board answers, on opening, "who is working right now?" — and for
any picked date, "who worked, from when to when?". UI is decided here (no
further options round):

1. **Date strip** at the top — horizontally scrollable day chips, today
   selected by default (the same previous-dates-strip pattern the work queues
   use).
2. **Summary tiles** — four backend-owned whole-filter counts: **Working
   now** · **Clocked out** · **Not clocked in** · **Flagged**. For a past
   date, "Working now" becomes "Worked". Tapping a tile filters the list to
   that bucket.
3. **Search** — one field, server-side name search, debounced.
4. **Filter chips** — **Park** and **Designation** rows (options from
   backend option groups, single-select + "All"). Filters and search combine;
   the summary tiles always reflect the whole current filter, never the page.
5. **Grouped roster list**, three sections in fixed order, each header
   carrying its live count:
   - **Working now** — green dot, name, designation · park, "In 08:12 ·
     4h 05m so far". The elapsed figure ticks client-side purely as
     rendering of the backend-served `clock_in_at`; hours-worked truth stays
     backend-owned.
   - **Clocked out** — "08:02 – 17:31 · 9h 29m" rendered verbatim from the
     backend entry row.
   - **Not clocked in** — hollow dot, name + designation · park.
   Row-level flag chips: *Offline punch*, *No location*, *Not clocked out*
   (past dates). Keyset pagination ~20 within a section; Room-cached
   offline-first with the standard stale indicator; `RefreshOnResume` +
   `SyncIconButton`.
6. **Row tap → person-day detail**: both punches in full (times, address,
   distance-from-park, device model + app version, flags) plus that person's
   last 7 days of entries.

Backend: `GET /app/clock/presence?date=&park_id=&designation=&status=&search=&cursor=`
(`clock.read`). Grain: **person × business_date over the whole active
roster** — a LEFT JOIN from `workforce_members` (status `active`) to
`workforce_clock_entries`, so people who never punched still appear under
"Not clocked in" (grain proof to be written at the query per the
projection-review rule: one row per active member, entries join is 1:1 on the
unique day key). Section titles, tile labels, chip copy, empty states — all
backend-owned. **Parity lock:** this endpoint and the admin-web clock tab
(§5) must resolve to the same source and grain; a count shown on both
surfaces is asserted equal by a cross-surface test.

### 4.5 Telemetry

`AnalyticsEvents` constants for clock_in_attempted / succeeded / refused_mock
/ clock_out, banner_shown / banner_tapped; Crashlytics non-fatals on capture
failures. (telemetry-guard enforces this on the diff anyway.)

---

## 5. Admin-web design (/people, People / HRMS)

- The existing backend-owned `people_view_tabs` option group gains an enabled
  **`clock`** tab ("Clock In / Out") — the disabled placeholders stay as they
  are.
- **Tab content:** server-component table, backend contract, keyset-paginated
  (~25): Person · Park · Department · Date · Clock in (IST) · Clock out (IST)
  · Hours · Location (address, short) · Device (model + app version) · Flags
  (chips: "Location unavailable", "Mock app refused", "Not clocked out").
  Filters (GET-form, URL round-trip, same as people-board): date range
  (default today), park, department, person search, flagged-only.
- **Row drawer** (`LocalOverlayLink`, same-page overlay per the standing
  rule): every captured field of both punches — full coordinates with a
  map-link, accuracy, address, all device columns, skew, battery, network,
  integrity flags, refused-attempt history for that person/day.
- **All People table:** one new "Clocked in" chip column (Today: time, or
  "Not clocked in") so the roster view answers the daily question at a
  glance. Whole-filter summary tiles on the clock tab (clocked-in count /
  not-yet / flagged) come from the backend summary, never page-local math.
- Everything — tab label, column labels, chip copy, empty states, filter
  labels — compiled into the page contract in `adminui/app/service.go`
  (backend owns copy). Mock-fidelity + visual smoke + Chrome check before
  push, per standing frontend rules.

---

## 6. Working-hours number (the one the whole feature exists for)

- `worked_minutes = clock_out_at − clock_in_at` on the server, stored on the
  entry row at clock-out, in IST business-day terms. Backend owns it; both
  surfaces render it verbatim (cross-surface parity rule — no client ever
  re-derives hours).
- An entry never clocked out is **visibly honest**: status `open` all day,
  then `auto_closed` at day close with `worked_minutes = NULL` and a "Not
  clocked out" flag — we never invent an end time. (Exact auto-close
  mechanics: open decision Q4.)

---

## 7. Interplay with PR #126 (per-person access)

- The `clock` module is registered in the **capability catalog** from day one
  so the person-access audit (`tools/dev/audit-person-access.py`) and parity
  tests see it — PR #126's own ledger records "silent module loss" for
  modules missing a catalog entry.
- Proposed default: clock is a **baseline module for every active app
  principal** (like device registration — attendance is mandatory, so it
  should not be per-person revocable by an accidental untick). `clock.read`
  (the admin-web view) IS a normal tickable capability under People / HRMS.
  Confirm in open decision Q2/Q3.
- Landing note (updated 2026-08-28): PR #126 was closed unmerged, but its
  content **landed on `main`** as split commits (`b4f250870`, `9ab0a38c7`,
  `44607b4ff`) — so basing on fresh `main` includes the per-person access
  model. Worktree created: `/Users/manoharchowdary/Desktop/mesha/wt-clock`,
  branch `feat/clock-in-out` off `origin/main` (`2f5a5a92c`).
- The two clock pages are separate catalog pages under the module: **My
  Clock** (`clock.self`, baseline for everyone) and **Team / presence**
  (`clock.read`, ticked for CXO/leadership only).

---

## 8. Build order (each step lands green before the next)

1. **Backend core** — migration, domain, ports, postgres adapter
   (idempotent write path + entry pairing in one txn), routes, OpenAPI,
   generated clients. Postgres tests: first punch, exact replay, same-key
   different-payload, double clock-in same day, clock-out without in,
   mock-refusal, IST day-boundary punch (23:59 vs 00:01).
2. **`/app/clock/status` + admin list/detail reads** — including the
   backend-owned banner copy and the admin summary. Seed: nothing to seed
   (operational table), but `seed-migration-guard` classification recorded
   (operational/audit class).
3. **Android module** — feature-clock, nav registration, outbox op types +
   SyncEngine dispatch, Room status cache, clock screen, telemetry. Targeted
   tests: idempotency-key stability, outbox drain, Room migration pair
   (schema + upgrade-crash) for the new cache table.
4. **Mock-location layer** — provider extension (`isMock`), package scan,
   refusal screen, server echo tests (tampered-client refusal).
5. **Reminder banner** — status VM + shell mount + backend copy.
6. **Presence board (§4.4)** — `/app/clock/presence` read (grain proof at the
   query), Team page UI, page-grain gating (`clock.read`), cross-surface
   parity test against the admin list.
7. **Admin-web tab + drawer + chip column** — contract, tables, drawer,
   filters; mock-fidelity + Chrome visual proof on the exact route.
8. **Closeout** — leadership-assistant coverage view, coverage-matrix row,
   docs (`docs/features/clock-in-out/` TRD-ification of this plan +
   runbook), AGENTS.md pointer, guard wiring, `make ci-local`, E2E report
   published on the Pages index per the standing rule.

Estimated shape: ~1 migration, ~2 new backend packages' worth of files inside
`backend/internal/workforce/` (clock service + adapters, staying in the
workforce vertical — attendance is HRMS truth), 1 new Android feature module,
1 admin-web feature folder addition.

---

## 9. Recorded maintainer decisions (2026-08-27)

The four design questions were put to the maintainer and answered; these are
now locked into the plan:

**D1 — Offline punches: ALLOWED, flagged.** A clock-in/out captured offline
queues through the outbox like every other field write. The mock-location
check runs on the client at tap time AND again on the server at drain. The
server stamps arrival, stores the device `captured_at` + `clock_skew_ms`, and
the row carries a visible "recorded offline" flag on the web view
(`network_type = offline_queued`). For an offline punch, `business_date` and
`worked_minutes` anchor on the device `captured_at` (skew recorded), since
the server arrival time can be hours later — this is the one place device
time is used, and the flag + skew make it auditable.

**D2 — Everyone clocks in.** Every person with an app login — operators, park
heads, directors, verifier, CEO. Everyone sees the reminder banner until
clocked in. Exemptions, if ever needed, come later as per-person ticks.

**D3 — Location: record + distance flag, no hard geofence in V1.** Every
punch stores its location; the web view shows distance-from-park as a flag.
A refusal geofence is a later decision once real farm GPS accuracy is known.
Sub-task this creates: park reference coordinates must exist (a
lat/lng per park in config — `locations` carries none today); until a park
has coordinates, the distance flag simply doesn't render for it.

**D4 — One pair per day + midnight auto-close.** A single in/out pair per IST
business day (the `UNIQUE (tenant, member, business_date)` in §3.1 stands). A
forgotten clock-out auto-closes at midnight IST with `worked_minutes = NULL`
and a visible "Not clocked out" flag — an end time is never invented. The
auto-close runs as a small daily sweeper (keyset-chunked, idempotent, per the
worker-tick rules).

## 10. Still open — answer before green signal

**Q5 — "SPR" reference.** The request mentions "combination of SPR in a
different way" — please confirm what SPR refers to (separate PR? something
else?) so nothing is missed.

## 11. Implementation record (2026-08-28)

Built on `feat/clock-in-out` per §8, with two recorded deviations:

**Deviation 1 — punch routes ride `app.bootstrap`, not a `clock.self`
permission.** Everyone clocks (D2), so a dedicated self-permission would have
to be added to every one of the 47 role sets and the whole capability parity
matrix for zero narrowing. The punch/status routes gate on AppBootstrap ("any
authenticated app principal"), exactly like device registration. The
assignable capability is the CROSS-PERSON read: `clock.presence.read`
(ceo_internal + per-person ticks), catalog module `clock`, level Oversee.

**Deviation 2 (consequence, not a choice) — every principal now has a
drawer.** The baseline clock module makes even a single-module operator and a
single-feature verifier a two-module principal, so per the locked 2026-08-03
placement rule the drawer appears and "You" moves to its footer for everyone.
The previously-minimal chrome cases are gone; the nav tests were updated to
pin the new truth (service_test.go, bootstrap_copy_test.go,
bootstrap_profile_entry_test.go, bootstrap_verify_route_test.go).

Landed so far (each step green before the next):
- Migration `000221_workforce_clock.sql` — events (append-only) + entries
  (person x IST-day pairing read model, unique day key), gen_random_uuid PKs.
- `workforce` clock domain/ports/postgres/app/http; one-transaction punch
  write (idempotency reservation + snapshot replay, event insert, entry
  open/close, self-healing auto-close of stale open days); presence/list
  reads share ONE repository page (cross-surface parity by construction).
- Routes + `clock.presence.read` + capability catalog + backfill row; module
  registry entry with My Clock (ungated) and Team (ClockPresenceRead) pages,
  reviewContributions so the standalone verifier keeps the module, baseline
  offer exempt from tick narrowing.
- Backend-owned copy in en/hi/kn/te (banner, refusals, sections, flags);
  copy-parity test across locales.
- OpenAPI (app + admin) + regenerated TS client.
- Admin-web: `/people` Clock In / Out tab (summary tiles, date/park/
  designation/bucket/search filters, keyset table, record drawer with both
  punches + map link), "Clocked in" chip on All People, `view_clock` control.
- Tests: service unit suite (mock refusal before any write, offline anchor,
  refusal mapping, banner lifecycle, hours/flags, presence buckets, locale
  parity) + Docker Postgres write-path proof (replay, refusals, pairing,
  auto-close, presence/detail agreement). Guardrails green through the
  backend/web surface (module-alerts pending entry recorded; coverage-matrix
  scoped exclusion for the assistant with the named follow-up).

In progress: the Android module (feature-clock, outbox punches, mock-location
detection, reminder banner, Team presence board).
