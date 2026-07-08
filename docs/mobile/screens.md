# Screen Spec — Goat OS Mobile (Android)

Every screen follows `mock/vaccination-mobile-mock.html` for **visual/layout/
interaction** only. For each: mock (visual reference), Compose destination (feature
module), the backend contract that feeds it, role visibility, and key states. The
backend contract owns state, visibility, nav/labels/filters/disabled reasons,
actions, statuses, and options (golden frontend rule); the app renders.

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
  - **Week**: today's entry = "Today · N sheds · M due" (counts from the backend
    calendar/sheds read — the app does not re-derive which animals are due) → the
    **backend-provided drill target** (today: operator → execute; leadership →
    drive-status follow-up). Other days = scheduled shed markers.
  - **Month**: drive-day dots; tap day → `ovl-day` sheet (that day's sheds +
    status). Done day → opens the shed/drive record.
  - **History**: past shed/drive records → record sheet.
- Backend: `GET calendar?scope&range` (Asia/Kolkata day buckets); day/record reads.
- CTA label is **backend-provided** per principal (today: operator "Open drive";
  leadership "View drive status") — not a client role switch.
- Note: which calendar segments are visible comes from bootstrap
  `presentationConfig` visible-nav (the mock hides month/history for operators); the
  app renders the backend-marked segments, never a `role==operator` gate.

## Drill from the calendar card (backend-provided target)

### Today's sheds / Drive status  (`v-sheds`)  — backend-returned lens + scope token (today's examples: O execute own-park; PM read-only own-park; D/C read-only all-parks)
- Module: `feature-sheds`. Same route, **backend-scoped lens**. Header: date · window ·
  shed count · due total. Day progress bar (from backend totals). **Shed cards**, each: cohort · in-shed,
  status pill, **vaccine-group chips (mix-and-match)**, in-shed/due/done nums,
  progress. Roster-change cards + kernel info box.
  - **Operator (execute)**: eyebrow "Vaccination · CBE", title "Today's sheds",
    action per shed (Start / Resume / View records — **local execution UX** from the
    operator's own scan progress; server revalidates on submit).
  - **Leadership (read-only follow-up)**: eyebrow scope label ("All parks · 2" for
    director/ceo, "CBE" for parkmgr), title "Drive status". No Start/scan. Per-shed
    status colour: **done = green, in-progress = amber, delayed / not-started =
    red** (left-border + "Delayed · chase team" / "Not started — chase the team ›").
    Card → shed record (a delayed shed shows "Delayed · not started", not a fake
    proof window).
- Backend: `GET sheds?scope_token=<token>` → shed_day/shed_group/roster_animal (cached
  in Room) + per-drive follow-up status. Scope = own park (operator/parkmgr) or all
  parks (director/ceo). Shed-first: a shed can have several due vaccine groups.
- States: ready / in-progress / done per shed; leadership rows are read-only
  because the backend returned no Start/scan action for this principal (absence of
  the action in the payload — not a client `role==` check), with the red-on-delay
  treatment.

### Scan  (`v-scan`)  — O only (leadership tap blocked server-side)
- Module: `feature-scan`. Header: shed · cohort. **Progress ring** = shed total
  `done/T` (**T + eligibility from backend**; the ring reflects backend totals plus
  locally-captured, not-yet-synced scans — it is not a local source of truth). **Per-vaccine-group chips** (filter by the **backend-tagged** vaccine group on each roster animal; active group highlighted). Tap-to-scan
  (RFID or ring). Done / Pending / Skipped tiles (render the backend roster/status with a local unsynced-scan overlay for draft UX; backend revalidates final truth — no client eligibility/grouping) → `ovl-scanlist` sheet
  (searchable, per-animal vaccine). Live feed of last taps (each animal → its due
  vaccine, "2 tags" when double-tagged). Submit is **UX-disabled** from the
  backend-provided completion checklist + local draft scan state; the backend
  revalidates and decides on submit (the gate is a hint, not the authority).
- Backend/device: `RfidReaderPort` (fake in dev); eligibility + roster from
  backend; each tap writes `scan_event` (given / skipped+reason) locally.
- States: scanning, not-due (from **backend-provided roster eligibility**, cached;
  red + double buzz **+ audible alert tone**), complete → submit enabled (local
  execution state; backend revalidates on submit); resume
  preserves prior progress (no reset on re-show — mock fix). Feedback (haptic +
  tone) is rendered locally via `FeedbackPort`, but the not-due *signal* is backend
  eligibility, not a client date/policy check.

### Submit  (`v-submit`)  — O only
- Module: `feature-submit`. One **shed record** covering all its due vaccines:
  per-group vaccine · dose · **backend-selected lot** (operator scans/confirms the
  physical vial; the app does not pick FEFO/expiry), cold-chain, animals
  vaccinated, operator + backup, **video proof** capture, remarks. "Submit shed
  record".
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
- Module: `feature-leadership`. **Coverage hero** (dose coverage %, doses line — from the backend rollup, not client-aggregated),
  pills: **park scope picker** (`ovl-scope`, director + CEO/COO), animals,
  **data gaps** (`ovl-gaps`). **KPI tiles**: Doses given → `ovl-given` (per-vaccine
  drill), Pending → overdue list. **Today's sheds** (per-shed vaccine mix +
  assign). **Backlog by vaccine**. **Needs a decision** (overdue → reschedule).
  **Coverage by park** (director + CEO/COO; tap re-scopes).
- Default scope is **backend-provided** per principal (today: parkmgr = own park;
  **director + CEO/COO = all parks**, drill via picker) — the app renders the scope
  options + default, it does not resolve scope by role.
- Backend: `GET rollup/backlog/gaps?scope`; scope filters everything.
- Backend-returned action visibility / disabled reasons (not client role gates): park picker +
  coverage-by-park, assign, and scan visibility all come from the principal's
  bootstrap grant set (today: picker/coverage-by-park = director + CEO/COO; assign
  = CEO/COO + PM; scan operator-only). The app shows a control only when its
  grant/action target is present in the payload.

### Overdue  (`v-overdue`)  — PM/D/C
- Module: `feature-leadership`. **Backend-classified** missed vs in-buffer, with
  color legend and shed/animal detail → reschedule (the app renders the returned
  `status`; it does not classify).
- Backend: `GET overdue?scope` — backend does the Asia/Kolkata buffer/lateness math.

### Reschedule (`v-reschedule`) — PM/C (assign)
- Module: `feature-leadership`. Segmented reschedule / mark-scheduled; **date
  picker** (`ovl-date`) over **backend-provided allowed date options**, each with a
  precomputed in/out-of-buffer state + label — the app renders the option list, it
  does not compute the buffer; **assign** (`ovl-assign`) primary + backup from HR;
  confirm → 4-channel notify.
- Backend: reschedule + assign commands (idempotent); NotificationGateway.

### Shed / drive record  (`ovl-shedrec`, `ovl-driverec`)  — all (read-only)
- Module: `feature-record`. Per-vaccine-group breakdown (given/due, dose),
  cohort · shed, operator + backup, window · video proof, searchable animal
  list (each with its vaccine).
- Backend: `GET shed-record/{shed_id}` / drive record.

## Overlays → components

| Overlay | Component | Feeds |
|---|---|---|
| `ovl-drawer` | `NavDrawer` | module registry (backend-flagged built + non-tappable "Soon" placeholders), profile, settings |
| `ovl-lang` | language `GoatBottomSheet` | DataStore locale |
| `ovl-scope` | park picker | sends selected **backend scope token** → backend re-scopes overview + follow-up (director + CEO/COO) |
| `ovl-gaps` | data-gaps sheet | animals excluded from coverage + reason |
| `ovl-given` | doses-given drill | per-vaccine given + coverage bars (scope-aware) |
| `ovl-driverec`/`ovl-shedrec` | record sheets | per-vaccine breakdown + animals |
| `ovl-date` | date picker over **backend-provided** date options (each carries its in/out-of-buffer state + label) | reschedule |
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
- **Refresh** on read screens (Calendar, Drive status / Today's sheds, Overview,
  Overdue): pull-to-refresh (`PullToRefreshBox`) + a header refresh button →
  scoped re-pull into Room. Offline keeps last-synced + a short message. Scan /
  Submit have no refresh (local-first, sync-engine reconciled). See TRD §6.
- **Every list/row/tile is actionable or clearly informational** (no dead
  microcopy — repo standing UI rule; the mock already enforces this).
