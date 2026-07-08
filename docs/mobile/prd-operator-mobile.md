# PRD — Goat OS Mobile (Android)

Status: draft for pre-implementation review · Owner: mobile · Surface:
operator/device field surface · UI reference (visual/layout/interaction only — the
backend contract owns data, labels, actions, statuses, options): `mock/vaccination-mobile-mock.html`.

## 1. Problem

Vaccination drives happen in the field, on cheap Android phones, often with poor
connectivity, by health field staff who are not desk users. Today the process
runs through Slack forms + Sheets, which gives no live shed-level progress, no
enforced SOP, no verifiable proof, no offline capture, and no clean feed into the
obligation kernel. Leadership cannot see, in real time, which sheds were covered,
what is overdue, and whether coverage numbers are trustworthy.

## 2. Goal

One Android app, **role-aware** (bootstrap decides content per principal), built on one shared
entry point.

**Calendar is the common vaccination entry point for every role** (CEO decision,
Manju, 2026-07-08). After login, operator and leadership both land on the
Calendar. Tapping a calendar drive card then opens the **backend-provided drill target** for the principal (the app maps the returned route/action ID to a screen; no client `when(role)` switch — see system-design §2):

- **Operator** (Health Asst Mgr / field) → execution: enter shed → tap-scan each
  animal by RFID → record the due vaccine(s) with video proof, fully
  **offline-first**, then sync to the backend as verifiable shed records.
- **Park Head / Park Manager** → read-only **drive-status follow-up** for their
  park. A delayed / not-started drive turns **red** so they can chase the team.
- **Director** → the same read-only follow-up **across all parks**, not one park.
- **COO / CEO** → like Director for now (company-wide, per-park rollup + data
  gaps), with more control later.

Leadership keeps a richer command **Overview** (backend-computed coverage,
per-vaccine backlog, data gaps, overdue decisions, assign — rendered, not
client-aggregated) one tap away via the Home nav tab — but the
Calendar, not the Overview, is where **every** role starts.

It is the mobile renderer on the **same backend contracts** as admin-web — not a
separate product.

## 3. Non-goals

- Not a dashboard-authoring surface. Config, SOP authoring, protocol rules, form
  building stay on the web. The phone only **executes** a pinned SOP/form and
  **records** what happened.
- Not iOS (operators are Android-only). Leadership on iPhone use admin-web
  responsive/PWA, not a native iOS build. (Revisit only if leadership demands a
  native iOS app — see extensibility doc.)
- Not a generic multi-vertical app yet. First shipped module is **Preventive
  Care → Vaccination**. The shell must leave room for future modules/verticals
  (extensibility doc) but must not show unbuilt modules as live.
- No direct datastore access, no client-authoritative permissions, no timed-video
  SOP as the canonical engine.

## 4. Users & roles

Single app; `GET <mobile bootstrap>` returns the principal's role, park/shed
scope, grants, and visible navigation. Every role lands on the **Calendar**; the
calendar-card drill and scope differ by **backend-returned route/action/scope
tokens**; the four role lenses below (mock `data-role`) are examples only, not a
client `when(role)`:

| Role | In app | Scope | Primary job |
|------|--------|-------|-------------|
| **operator** (Health Asst Mgr / field) | executes drives | own park | enter shed → scan animals → record + proof → submit |
| **parkmgr** (Health Manager) | read-only follow-up + assign | own park | monitor park sheds, chase delays, assign operator/backup, reschedule |
| **director** (Health Director) | read-only follow-up + **park picker** | **all parks** | company-wide coverage/backlog/overdue; drill into any park |
| **ceo/coo** | read-only follow-up + park picker + assign | **all parks** | company-wide + per-park rollup, backlog, data gaps, assign |

Scope rule (**backend-provided grant/scope options, not a client constant**):
operator & park manager resolve to their own park; **director and CEO/COO default
to all parks** with a park picker (Manju, 2026-07-08 — director was previously
park-locked, now company-wide). The app receives its scope options + default from
bootstrap and renders them; it does not decide scope.

Server RBAC is authoritative. The **assign** action target is **backend-granted**
(today: CEO/COO + Park Manager) — the app shows Assign only when that grant is in
the payload and never hardcodes `role == ceo|parkmgr`. Likewise **execution
(scan/submit) is the field operator (Health Asst Mgr) only** — Health Manager
(Park Manager) and every tier above are read-only follow-up; leadership tapping
scan is blocked server-side. "Health Manager" is the park-manager tier, not an
executor. These are described here as intended behaviour; the app enforces none of
it client-side — grants/action targets arrive from the backend.

## 5. Scope — screens (from the mock)

Shared entry (all roles):

- **Login** — work-email + OTP (not phone).
- **Calendar** — the universal landing for every role. Week (today's drive card),
  month (drive-day dots + day sheet), history (past shed records). Which calendar
  segments (week/month/history) are visible comes from the principal's bootstrap
  `presentationConfig` visible-nav set — today operators get week only and
  leadership also gets month + history, but the app renders whatever segments the
  backend marks visible (no `role==operator` check). Tapping the day's drive card
  is the **backend-provided drill** below.

Operator drill (execution):

- **Today's sheds** (`v-sheds`) — the day's sheds for the operator's park, each
  shed showing its own mix of due vaccine groups (shed-first, several vaccines per
  shed), with Start / Resume / View-records actions (**local execution UX** driven
  by the operator's own scan progress; the backend revalidates and decides on
  submit).
- **Scan** — per shed: progress ring (shed total **from backend**; the ring overlays
  locally-captured, unsynced scans on the backend total), per-vaccine-group progress
  chips (grouped by the **backend-tagged** vaccine group on each roster animal),
  tap-to-scan roster where each animal shows its own backend-provided due vaccine;
  Done / Pending / Skipped lists (render the backend roster/status with a local
  unsynced-scan overlay for draft UX; backend revalidates final truth — no client
  eligibility or aggregation).
- **Submit** — one shed record covering all its due vaccines (dose, the
  **backend-selected/reserved lot** — the operator scans/confirms the physical vial
  against it; the app does **not** pick FEFO/expiry — cold-chain, animal count,
  operator+backup, video proof, remarks).
- **You / settings**, **RFID reader** pairing, **Alerts**.

Leadership drill (read-only follow-up):

- **Drive status** (same `v-sheds` route, read-only lens) — the drive's sheds as
  live-status cards, **backend-scoped** (today's returned example: park manager =
  own park; director & CEO/COO = all parks). Per-shed status: **done = green, in-progress = amber, delayed /
  not-started = red** ("chase the team"). No Start/scan. A card opens the shed
  record. This is what a calendar-card tap opens for non-operators.
- **Overview** (`v-dhome`, reached via the Home nav tab — not the landing) —
  dose-coverage hero, **park picker** (director + CEO/COO), **doses-given** drill
  (per vaccine), **pending** drill, **data-gaps** drill, **today's sheds** (per-
  shed vaccine mix, assign), **backlog by vaccine**, **needs-a-decision** (overdue
  → reschedule), **coverage by park** (director + CEO/COO).
- **Overdue list**, **Reschedule** (backend-provided allowed date options with
  state + labels; assign primary/backup),
  **shed record** (per-vaccine breakdown, searchable animals).

Shared overlays: drawer (only when the backend chrome has ≥2 modules — see the
navigation-chrome rule; single-feature roles have no drawer), language, park
scope, data gaps, doses given, drive/shed record, date picker, assign team, scan
list, day sheet, **sync status**, toast.

Always-on **sync/connectivity bar**: a slim strip on every signed-in screen shows
Online/Offline and the sync state (All synced · Syncing N… with progress ·
Offline · N queued). Tapping it opens the **sync status** sheet — the outbox: each
queued shed record with its state (queued → uploading proof → syncing record →
synced) and a per-item progress bar. This makes the offline-first behaviour
visible to the operator instead of a silent background process.

Full per-screen contract in [screens.md](screens.md).

## 6. Key product rules (already validated on the mock)

- **Calendar-universal drill (backend-provided target)**: the Calendar is the
  single entry point for all roles. Tapping a drive card opens the drill target the
  backend returns for the principal — today the **execution** flow for the field
  operator (Health Asst Mgr) and a **read-only status follow-up** for leadership
  (Park Manager, Director, CEO/COO) — same card, backend-decided target.
- **Navigation chrome is backend-driven (no client sidebar heuristic)**: the
  bootstrap `presentationConfig` returns the nav chrome for the principal. A
  **single-feature** principal (today: the vaccination-only operator and leadership
  roles — vaccination and nothing else) gets the **bottom bar only, no
  sidebar/drawer**; the drawer's extra affordances (language, RFID reader,
  notifications/alerts, sign out) live in the **You / Settings** bottom-bar screen
  that already exists, so nothing is lost. The sidebar/drawer appears **only when
  the principal handles ≥2 modules/features** (the backend returns the multi-module
  chrome + module list). The app renders whatever chrome the backend specifies — it
  never counts modules or checks role to decide whether to draw a sidebar
  (Manju/CEO decision, 2026-07-09).
- **Red-on-delay follow-up**: in the leadership follow-up, a not-started / delayed
  drive renders **red** ("chase the team"); in-progress is amber, done is green.
  This is the signal leadership acts on to chase the ground team.
- **Scope (backend-provided per principal)**: operator and park manager resolve to
  their own park; director and CEO/COO to all parks and can drill into any single
  park via the picker. The app renders backend scope options/tokens and sends the
  selection; it does not resolve scope by role and the backend filters every read.
- **Shed-first**: the unit of work is the shed, not the vaccine. A shed's cohort
  can have several due vaccines at once (mix-and-match). A drive is a day of shed
  visits, not "one vaccine across sheds".
- **Both RFID tags**: animals may carry two RFID tags; scan matches either.
- **Deterministic skips**: not-due animals in a shed are skipped with a
  **backend-provided** reason from a scoped option set (e.g. quarantine/ICU, not in
  today's set, under treatment, pregnant hold) and stay visible; eligibility + the
  reason vocabulary come from the backend (obligation engine / pinned form), not a
  client enum or the operator.
- **Multi-sensory scan feedback**: every scan gives immediate non-visual feedback
  so the operator need not watch the screen — eligible = single buzz + soft
  confirm tone; **a not-due (red) hit = a stronger double-buzz AND an audible
  alert tone**. Operators scan one-handed with the reader; the not-due case must
  be unmistakable by feel and by sound. The not-due *signal* is **backend
  eligibility** (from the cached roster), not a client date/policy check — only the
  haptic/tone rendering is local (so it still fires offline).
- **Coverage honesty**: "N data gaps" surfaces animals excluded from coverage %
  (missing DOB/breed/tag); nothing is faked to look complete.
- **Buffer / missed = backend-computed outcome, not a client constant**: whether an
  overdue dose is still *in buffer* (recoverable by reschedule) or *missed* is
  **computed server-side** and delivered as a `status` field. Missed-dose handling
  is **cycle-relative** (catch-up-now vs wait depends on distance to the same-/next-
  cycle drive — see
  [vaccination-rules.md](../preventive-care-vaccination/vaccination-rules.md)); the
  explicit **7-day buffer applies to animals recovering from a defer/hold** rejoining
  a compatible drive, not to every overdue dose. The app renders the returned
  `status` + label + colour and shows the alert lead the backend supplies; it never
  does the math and never hardcodes the rule.
- **Explicit refresh**: read screens (Calendar, Drive status / Today's sheds,
  Overview, Overdue) support **pull-to-refresh** plus a header refresh button; it
  re-pulls the scoped read from the backend and updates the local cache. Offline →
  keep the last-synced data and tell the operator (never a blank or endless
  spinner). Execution screens (Scan / Submit) do not refresh — they are
  local-first and reconcile through the sync engine.
- **India business calendar**: the **backend** computes all due/missed/reminder/
  day-boundary buckets in `Asia/Kolkata`; the app formats/displays the returned
  fields (using `Asia/Kolkata` for pure display formatting only) and never buckets.

## 7. Languages

English, Hindi, Kannada, Telugu. Language is a device-level setting applied to
every screen. Backend-owned copy still comes from the contract; app strings are
localized. See design-system i18n section.

## 8. Constraints

- **Low-end Android**: target 2–3 GB RAM, weak GPU, old Android, spotty network.
  Cold start, jank, battery, APK size, and memory are first-class budgets (see
  performance doc).
- **Offline-first with visible sync**: a full drive must run with no connectivity.
  Most data (today's sheds, roster, scan events, shed records, media) is cached in
  the on-device DB (Room) and served locally first; an outbox syncs to the backend
  when online, showing live progress (queued → uploading proof → syncing record →
  synced). Sync must be idempotent — retries and app restarts never create
  duplicate records. The operator always sees whether their work is saved locally
  vs synced to the server.
- **Hardware**: Bluetooth UHF RFID handheld (Chainway-class), phone camera for
  video proof, vibration motor **and speaker** — scan feedback is both haptic and
  audible (a distinct alert tone on a not-due hit).
- **Auditability**: every recorded dose becomes a verifiable shed record with
  proof; every write is idempotent.

## 9. Success metrics

- Operator can complete a shed (scan → proof → submit) offline in the field and
  have it sync with zero duplicate records after retries.
- Time-to-record per animal ≤ mock's tap flow (no extra taps).
- Crash-free sessions ≥ 99.5% on target low-end devices (Crashlytics).
- Cold start ≤ 2.5 s and scan-tap-to-feedback ≤ 120 ms on target devices
  (Firebase Performance); a not-due scan fires a distinct haptic + audible alert
  within that budget.
- Leadership sees a submitted shed reflected in coverage within the sync SLA.
- Coverage % never silently excludes animals — data gaps always surfaced.

## 10. Future (must not block, must not be faked)

Space reserved for: Preventive Care → Treatment/Deworming, Counts, Breeding,
Procurement field capture, and other verticals — all on the same shell, sync
engine, and module registry. See [extensibility-future-modules.md](extensibility-future-modules.md).
Unbuilt modules must not appear as live nav.
