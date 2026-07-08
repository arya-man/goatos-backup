# PRD — Goat OS Operator Mobile (Android)

Status: draft for pre-implementation review · Owner: mobile · Surface:
operator/device field surface · Source of truth UI: `mock/vaccination-mobile-mock.html`.

## 1. Problem

Vaccination drives happen in the field, on cheap Android phones, often with poor
connectivity, by health field staff who are not desk users. Today the process
runs through Slack forms + Sheets, which gives no live shed-level progress, no
enforced SOP, no verifiable proof, no offline capture, and no clean feed into the
obligation kernel. Leadership cannot see, in real time, which sheds were covered,
what is overdue, and whether coverage numbers are trustworthy.

## 2. Goal

One Android app, **role-aware** (login decides content), that:

1. Lets a field operator run a vaccination drive **shed by shed**, tap-scanning
   each animal by RFID, recording the due vaccine(s) per animal with video proof,
   fully **offline-first**, then syncs to the backend as verifiable shed records.
2. Lets leadership (Park Manager, Director, CEO/COO) see the **same app** in a
   read-only command view: live coverage, today's sheds, per-vaccine backlog,
   data gaps, overdue decisions, and assign teams to sheds.

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
scope, grants, and visible navigation. Four role lenses (mock `data-role`):

| Role | In app | Primary job |
|------|--------|-------------|
| **operator** (Health Asst Mgr / field) | executes drives | enter shed → scan animals → record + proof → submit |
| **parkmgr** (Health Manager) | read-only + assign | monitor park sheds, assign operator/backup, reschedule |
| **director** (Health Director) | read-only | park-scoped coverage, backlog, overdue |
| **ceo/coo** | read-only + park picker + assign | company-wide + per-park rollup, backlog, data gaps, assign |

Server RBAC is authoritative. `canAssign = ceo | parkmgr`. Operator scan/submit
is operator-only; leadership tapping scan is blocked server-side.

## 5. Scope — screens (from the mock)

Operator surface:

- **Login** — work-email + OTP (not phone).
- **Calendar** — week (today's shed entry), month (drive-day dots + day sheet),
  history (past shed records).
- **Today's sheds** — the day's sheds for the operator's park, each shed showing
  its own mix of due vaccine groups (shed-first, several vaccines per shed).
- **Scan** — per shed: progress ring (shed total), per-vaccine-group progress
  chips, tap-to-scan roster where each animal shows its own due vaccine; Done /
  Pending / Skipped lists.
- **Submit** — one shed record covering all its due vaccines (dose, FEFO batch,
  cold-chain, animal count, operator+backup, video proof, remarks).
- **You / settings**, **RFID reader** pairing, **Alerts**.

Leadership surface:

- **Overview** — dose-coverage hero, **park picker** (CEO), **doses-given**
  drill (per vaccine), **pending** drill, **data-gaps** drill, **today's sheds**
  (per-shed vaccine mix, assign), **backlog by vaccine**, **needs-a-decision**
  (overdue → reschedule), **coverage by park** (CEO).
- **Overdue list**, **Reschedule** (buffer-aware date + assign primary/backup),
  **shed record** (per-vaccine breakdown, searchable animals).

Shared overlays: drawer, language, park scope, data gaps, doses given, drive/shed
record, date picker, assign team, scan list, day sheet, toast.

Full per-screen contract in [screens.md](screens.md).

## 6. Key product rules (already validated on the mock)

- **Shed-first**: the unit of work is the shed, not the vaccine. A shed's cohort
  can have several due vaccines at once (mix-and-match). A drive is a day of shed
  visits, not "one vaccine across sheds".
- **Both RFID tags**: animals may carry two RFID tags; scan matches either.
- **Deterministic skips**: not-due animals in a shed are skipped with a reason
  (quarantine/ICU, not in today's set, under treatment, pregnant hold) and stay
  visible; eligibility comes from the obligation engine, not the operator.
- **Coverage honesty**: "N data gaps" surfaces animals excluded from coverage %
  (missing DOB/breed/tag); nothing is faked to look complete.
- **Buffer**: overdue within a 1-week buffer is recoverable by reschedule; beyond
  it is a missed dose and leadership was alerted a week ahead.
- **India business calendar**: all due/missed/reminder/among-day logic is
  `Asia/Kolkata`.

## 7. Languages

English, Hindi, Kannada, Telugu. Language is a device-level setting applied to
every screen. Backend-owned copy still comes from the contract; app strings are
localized. See design-system i18n section.

## 8. Constraints

- **Low-end Android**: target 2–3 GB RAM, weak GPU, old Android, spotty network.
  Cold start, jank, battery, APK size, and memory are first-class budgets (see
  performance doc).
- **Offline-first**: a full drive must run with no connectivity and sync later.
- **Hardware**: Bluetooth UHF RFID handheld (Chainway-class), phone camera for
  video proof, vibration feedback.
- **Auditability**: every recorded dose becomes a verifiable shed record with
  proof; every write is idempotent.

## 9. Success metrics

- Operator can complete a shed (scan → proof → submit) offline in the field and
  have it sync with zero duplicate records after retries.
- Time-to-record per animal ≤ mock's tap flow (no extra taps).
- Crash-free sessions ≥ 99.5% on target low-end devices (Crashlytics).
- Cold start ≤ 2.5 s and scan-tap-to-feedback ≤ 120 ms on target devices
  (Firebase Performance).
- Leadership sees a submitted shed reflected in coverage within the sync SLA.
- Coverage % never silently excludes animals — data gaps always surfaced.

## 10. Future (must not block, must not be faked)

Space reserved for: Preventive Care → Treatment/Deworming, Counts, Breeding,
Procurement field capture, and other verticals — all on the same shell, sync
engine, and module registry. See [extensibility-future-modules.md](extensibility-future-modules.md).
Unbuilt modules must not appear as live nav.
