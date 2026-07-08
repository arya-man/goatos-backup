# Screen Spec — Goat OS Operator Mobile

Every screen maps 1:1 to `mock/vaccination-mobile-mock.html`. For each: mock
source, Compose destination (feature module), the backend contract that feeds it,
role visibility, and key states. Backend owns nav/labels/filters/disabled reasons
(golden frontend rule); the app renders.

Legend — roles: **O** operator · **PM** parkmgr · **D** director · **C** ceo/coo.

## Shared entry (all roles)

### Login  (`v-login`)
- Module: `feature-auth`. Work **email + OTP** (not phone). Language sheet
  (`ovl-lang`) + theme toggle reachable pre-auth.
- Backend: auth token endpoint (Firebase Auth adapter) → then mobile bootstrap.
- States: idle, code-sent, verifying, error, version-gate-block.

### Calendar  (`v-calendar`: week / month / history)  — **universal landing, all roles**
- Module: `feature-calendar`. The Calendar is where **every** role lands after
  login (Manju, 2026-07-08). Segmented week/month/history.
  - **Week**: today's entry = "Today · N sheds · M due" → the **role-branched
    drill** (operator → execute; leadership → drive-status follow-up). Other days
    = scheduled shed markers.
  - **Month**: drive-day dots; tap day → `ovl-day` sheet (that day's sheds +
    status). Done day → opens the shed/drive record.
  - **History**: past shed/drive records → record sheet.
- Backend: `GET calendar?scope&range` (Asia/Kolkata day buckets); day/record reads.
- CTA label by role: operator "Open drive"; leadership "View drive status".
- Note: operator sees week only (`calMode` hidden for operator in mock);
  month/history are leadership.

## Role-branched drill (from the calendar card)

### Today's sheds / Drive status  (`v-sheds`)  — O execute (own park); PM read-only (own park); D/C read-only (all parks)
- Module: `feature-sheds`. Same route, role-branched lens. Header: date · window ·
  shed count · due total. Day progress bar. **Shed cards**, each: cohort · in-shed,
  status pill, **vaccine-group chips (mix-and-match)**, in-shed/due/done nums,
  progress. Roster-change cards + kernel info box.
  - **Operator (execute)**: eyebrow "Vaccination · CBE", title "Today's sheds",
    action per shed (Start / Resume / View records).
  - **Leadership (read-only follow-up)**: eyebrow scope label ("All parks · 2" for
    director/ceo, "CBE" for parkmgr), title "Drive status". No Start/scan. Per-shed
    status colour: **done = green, in-progress = amber, delayed / not-started =
    red** (left-border + "Delayed · chase team" / "Not started — chase the team ›").
    Card → shed record (a delayed shed shows "Delayed · not started", not a fake
    proof window).
- Backend: `GET sheds?scope=<park|all>` → shed_day/shed_group/roster_animal (cached
  in Room) + per-drive follow-up status. Scope = own park (operator/parkmgr) or all
  parks (director/ceo). Shed-first: a shed can have several due vaccine groups.
- States: ready / in-progress / done per shed; leadership rows are read-only
  (no Start; rolenote) with the red-on-delay treatment.

### Scan  (`v-scan`)  — O only (leadership tap blocked server-side)
- Module: `feature-scan`. Header: shed · cohort. **Progress ring** = shed total
  `done/T`. **Per-vaccine-group chips** (active group highlighted). Tap-to-scan
  (RFID or ring). Done / Pending / Skipped tiles → `ovl-scanlist` sheet
  (searchable, per-animal vaccine). Live feed of last taps (each animal → its due
  vaccine, "2 tags" when double-tagged). Submit gated until shed complete.
- Backend/device: `RfidReaderPort` (fake in dev); eligibility + roster from
  backend; each tap writes `scan_event` (given / skipped+reason) locally.
- States: scanning, not-due (red + double buzz **+ audible alert tone**), complete
  → submit enabled; resume preserves prior progress (no reset on re-show — mock
  fix). Feedback (haptic + tone) via `FeedbackPort`.

### Submit  (`v-submit`)  — O only
- Module: `feature-submit`. One **shed record** covering all its due vaccines:
  per-group vaccine · dose · **FEFO batch**, cold-chain, animals vaccinated,
  operator + backup, **video proof** capture, remarks. "Submit shed record".
- Backend: submit command (idempotency key) → one tx (form_submission + event +
  verification + outbox). Form rendered via forms-runner + pinned form_version.
- States: draft → queued → syncing → acked / conflict / dead-letter.

### You / Settings  (`v-you`), RFID reader  (`v-rfid`), Alerts  (`v-alerts`)  — O (leadership: profile only)
- Module: `feature-profile`. Profile (name/role/scope from bootstrap), language,
  RFID reader pairing (Chainway-class, battery/paired state), notifications
  (FCM), sign out.
- Backend/device: `RfidReaderPort` pairing; FCM token register; profile from
  bootstrap.

## Leadership surface

### Overview  (`v-dhome`)  — PM/D/C, reached via the **Home** nav tab (not the landing)
- Module: `feature-leadership`. **Coverage hero** (dose coverage %, doses line),
  pills: **park scope picker** (`ovl-scope`, director + CEO/COO), animals,
  **data gaps** (`ovl-gaps`). **KPI tiles**: Doses given → `ovl-given` (per-vaccine
  drill), Pending → overdue list. **Today's sheds** (per-shed vaccine mix +
  assign). **Backlog by vaccine**. **Needs a decision** (overdue → reschedule).
  **Coverage by park** (director + CEO/COO; tap re-scopes).
- Default scope: parkmgr = own park; **director + CEO/COO = all parks** (drill via
  picker).
- Backend: `GET rollup/backlog/gaps?scope`; scope filters everything.
- Role gates: park picker + coverage-by-park = director + CEO/COO; assign =
  CEO/COO + PM; scan absent.

### Overdue  (`v-overdue`)  — PM/D/C
- Module: `feature-leadership`. Missed (past buffer) vs in-buffer, with color
  legend and shed/animal detail → reschedule.
- Backend: `GET overdue?scope` (Asia/Kolkata buffer math).

### Reschedule  (`v-reschedule`)  — PM/C (assign) 
- Module: `feature-leadership`. Segmented reschedule / mark-scheduled;
  buffer-aware **date picker** (`ovl-date`, in/out of buffer); **assign**
  (`ovl-assign`) primary + backup from HR; confirm → 4-channel notify.
- Backend: reschedule + assign commands (idempotent); NotificationGateway.

### Shed / drive record  (`ovl-shedrec`, `ovl-driverec`)  — all (read-only)
- Module: `feature-record`. Per-vaccine-group breakdown (given/due, dose),
  cohort · shed, operator + backup, window · video proof, searchable animal
  list (each with its vaccine).
- Backend: `GET shed-record/{shed_id}` / drive record.

## Overlays → components

| Overlay | Component | Feeds |
|---|---|---|
| `ovl-drawer` | `NavDrawer` | module registry (built + "Soon"), profile, settings |
| `ovl-lang` | language `GoatBottomSheet` | DataStore locale |
| `ovl-scope` | park picker | scope → re-scopes overview + follow-up (director + CEO/COO) |
| `ovl-gaps` | data-gaps sheet | animals excluded from coverage + reason |
| `ovl-given` | doses-given drill | per-vaccine given + coverage bars (scope-aware) |
| `ovl-driverec`/`ovl-shedrec` | record sheets | per-vaccine breakdown + animals |
| `ovl-date` | buffer-aware date picker | reschedule |
| `ovl-assign` | assign primary/backup | assign command |
| `ovl-scanlist` | scan list (given/pending/skipped) | local scan_event + roster |
| `ovl-day` | month day sheet | that day's sheds/record |
| `ovl-sync` | sync status sheet | Room outbox: each queued record + state + progress |
| toast | `GoatToast` | one-shot effects |

## Cross-cutting

- **Role lens** switches content, not app. Server RBAC authoritative.
- **Scope** (park) lives in the top bar / picker; page bodies don't repeat it.
- **Empty vs error**: empty-but-OK shows zero-count sections; API/RBAC errors
  show a visible error state (never a blank that reads as "no data").
- **Sync/connectivity bar** (`#netbar`) on every signed-in screen: Online/Offline
  + sync state (All synced / Syncing N… with progress / N queued). Tap → `ovl-sync`
  outbox sheet. Backed by the Room outbox + `SyncStatus` Flow (TRD §6); records are
  local-first and sync when online, with no duplicates on retry.
- **Every list/row/tile is actionable or clearly informational** (no dead
  microcopy — repo standing UI rule; the mock already enforces this).
