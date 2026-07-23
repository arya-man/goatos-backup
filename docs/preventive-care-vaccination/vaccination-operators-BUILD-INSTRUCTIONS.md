# Vaccination Operators (HRMS) — BUILD INSTRUCTIONS (self-contained)

Goal: ship the **real** admin-web "Vaccination Operators" screen so it matches the
mock EXACTLY (UI/UX), is **backend-integrated** (no fake/disabled shells where an
endpoint exists), and every operator-config change (**cap, active-operators/day N,
default operator, week-off, leave**) **correctly reschedules/clears vaccination
obligations + drive assignments** and stays correct across Control Tower, Action
Center, Workflows, Calendar, and Vaccination L1–L4 (admin-web + mobile) — no lost/
duplicated obligations, no clinical-rule regression, no N+1/latency/render
regression.

Scope lock: ONLY the vaccination-operator slice + its vaccination cascade. NOT a
full HRMS suite (no payroll/hiring/directory/appraisals).

Authoritative sources to read FIRST:
- Mock (UI truth): `mock/vaccination-operators-hrms-mock.html`
- Design/model: `docs/preventive-care-vaccination/vaccination-drive-operator-scheduling.PROPOSAL.md`
- Cascade gap ledger (42 judged findings): `docs/preventive-care-vaccination/operator-config-cascade-gap-ledger.md`
- Memory: `goatos-operator-cap-cascade-bugs`, `goatos-ctac-alias-contract-a9779fad`

---

## 0. Model (ratify these — they change committed CPT invariant)

- **N = active_operators_per_day** (1 single+fallback / 2 pair / 3 parallel). Baseline stays 3 until set.
- **Single COMMON cap for all operators** (NOT per-operator). Replaces the per-seat `vaccination_daily_animal_cap` model. `Daily capacity = N × cap`.
- **Leave = future date ranges**, invariant **no two operators on leave the same date** (week-offs Fri/Sat/Sun disjoint → a 2nd same-date leave empties a fallback day). Enforce FE **and** BE.
- **Week-off** recurring, one weekday/operator.
- **Default operator** CEO-set; **fallback chain** (N=1) default→next available; auto-revert next day; CEO manual swap sticks for rest of drive.
- Update `AGENTS.md` "CPT operator-drive rehearsal seed invariant" from "3 equal operators, 200 each" to this configurable-N + common-cap + leave-date model when ratified.

---

## 1. Current real admin-web state (what exists today)

- Two tabs on `/people`: `features/people/positions-panel.tsx` + `features/people/timetable-panel.tsx`, switched by `hrms-page.tsx`.
- Positions panel: KPIs (Operators / Cap-per-operator / Daily capacity / Directors), a "Positions — three axes" table, backup-config + coverage cards, and a "Vaccination drive roster" with a **per-row cap input + Save** (per-position `vaccination_daily_animal_cap`).
- Timetable panel: KPIs (CPT operators / Cap-per-operator / Full-cap day / Lowest day) + weekly availability grid (week-off).
- These MUST be merged into ONE screen matching the mock.

## 2. Backend endpoints AVAILABLE (wire these — do NOT stub)

Generated client `@goatos/api-client` (admin-api), via `apps/admin-web/lib/api`:
- `listStaffPositions()` → positions incl. `week_off`/`week_off_weekday`, `tier`, `center_label`, `vaccination_daily_animal_cap`, `row_version`, `status`.
- `updateStaffPosition(id, {row_version, ...})` — per-position write (week_off, cap).
- `getVaccinationCapacityConfig()` → `{ maxPerDay }` (the common default cap).
- Capacity paths `/capacity`, `/capacity/{capacity_record_id}` — use for the COMMON cap read/write (confirm the client method name; add one if only GET is surfaced).
- **Leave (REAL — wire the calendar dialog + drawer to these):**
  - `applyStaffLeave(ApplyStaffLeaveRequest)` — body `{ workforce_member_id, scope_type: "tenant"|"center", scope_id, reason_code, starts_on (date), ends_on (date) }`.
  - `listStaffLeave()`, `getStaffLeave(absence_id)`, `/roster/leave/{id}/approve`, `/roster/leave/{id}/resolve-coverage`, cancel/delete `/roster/leave/{absence_id}`.
- `listBackupConfig()`, `listCoverage()`, `getStaffPositionProfile(id)`.

## 3. Backend endpoints MISSING (must build — coordinate; these touch roster/kernel)

- **Common-cap write** as a single tenant/park value (if `/capacity/{id}` PATCH is not surfaced in the client, add it). Decide precedence: common cap wins over the legacy per-position `MAX(...)` — see ledger OPS-CASCADE-2 (`visit_shot_lock.go:505` currently uses `MAX(vaccination_daily_animal_cap)`; must source from `vaccination_capacity_config`).
- **N (active_operators_per_day)** config — new tenant/park config + bootstrap contract field.
- **Default operator + fallback order** config — new.
- **Same-date leave conflict rejection** in `ApplyLeave` (roster_service.go ~389) — BE guard, park-scoped (ledger OPS-CASCADE-3).
- Everything in §5 (the cascade).

## 4. Frontend build — mock-fidelity, backend-driven (per admin-web AGENTS.md)

Merge into ONE panel (drop the tab strip). Match mock component anatomy exactly. Backend-owned labels/fields from `/admin-web/bootstrap` page contract; frontend owns layout only. Every element either backend-backed or disabled-with-reason (never a plainer substitute).

Screen sections (from mock):
1. **KPI row (reactive):** Operators · **Cap/operator** (common cap, subtitle "applies to all operators") · **Operators/day** (N, mode label) · **Daily capacity** (`N × cap`, recompute on cap save + N change).
2. **Operator roster & availability (one card):** columns `Person | Park | Week off | Planned leave | Weekly schedule | Status`.
   - **Common-cap control in card header** (value + ✏️ Edit → inline number → Save; Enter=save Esc=cancel; validate ≥1, reject out-of-range, never silent-default). One value for all. Remove the per-row cap inputs.
   - **Planned leave cell** = compact: next upcoming range + `N planned · X more upcoming` (never a growing chip wall). Click summary → right drawer. **Manage** button → calendar dialog. No-leave → `＋ Add leave` → dialog.
   - **Weekly schedule** = 7 cells Mon–Sun On/Off (week-off), recurring.
   - **Status** = TODAY's availability: `On leave today` / `Week-off today` / `Available` (NOT a leave count).
3. **Add-leave calendar dialog (modal):** month calendar, ‹›, range select (start→end), live range label; blocked = **past** (muted), **own already-planned** (amber, click-to-remove), **other operator** (red); range may not span a blocked date; merge adjacent/overlapping own ranges; commit via `applyStaffLeave`; close Add/Cancel/✕/Esc/outside; legend Selected·Already planned·Booked by another.
4. **Leave drawer (right slide-in):** click summary opens; lists ALL leaves grouped Upcoming/Past (past dimmed) each with day-count + remove (cancel endpoint); footer `＋ Add leave` opens dialog; refresh on add.
5. **Drive operator assignment:** N select (1/2/3) · Default operator select (enabled only N=1) · fallback chain strip · weekly assignment preview (`Day | Assigned operator | Reason`, recompute live). NO internal jargon on the CEO page (no `cap=1/day` pills, no dev footnotes). **Until §3 config endpoints exist, N/default render disabled-with-reason** (mock look, `aria-disabled`+title); once wired, enable.

FE constraints/fixes learned from the mock iterations (bake in, do not re-derive):
- Merge the two tabs; single screen.
- Common cap, not per-operator.
- Leave is date-range, future-only, never-two-same-date; blocked/past/own handling as above; ranges merge.
- Compact leave cell + drawer, calendar dialog for add — never inline row forms or growing chips.
- Status column = today availability, not a duplicate count.
- Reactive KPIs on every change.
- No internal/dev text on screen.
- Toast: success green, error red.

FE gates before push: `npm --prefix apps/admin-web run check:mock-fidelity`, `lint`, `typecheck`, `build`, and `smoke:visual:live` (open the screenshots). Same-page drawer/modal must be client-local (`LocalOverlayLink`/local state), no route/RSC on open/close, Back/Esc/outside/X close.

## 5. Backend cascade — THE hard part (ledger §A/§B; currently UNBUILT)

Headline: the kernel is **generation + forward-batching only** (`sweepVersion`, `sweeper.go:731`, touches only UNBATCHED obligations). Once batched, NOTHING re-evaluates on config change. There is NO `capacity.changed`/`roster.changed`/`leave.changed`/`weekoff.changed` event or consumer.

Build (model on `goat.location.changed → ReScopeOpenForGoatShift` in `shift.go`):
- **One scoped domain event per config mutation**, registered in `context/architecture/domain-event-registry.json` (producer/event/consumer/replay/DLQ/E2E), passing `make domain-event-architecture-guard`:
  - `vaccination.capacity.changed` (cap or N), `vaccination.roster.changed` (default/week-off/position), `vaccination.leave.changed` (add/remove).
- **Idempotent, advisory-locked consumer** that, for the affected tenant/park/future window: releases ONLY affected future, still-actionable batches/assignments back to unbatched (preserve in-progress/completed history), nulls/re-derives `conducted_by` on affected batches, then lets the sweeper re-plan through the existing gated path.
- **Set-reconciling write** for `vaccination_drive_assignments`: inside the tenant advisory lock, DELETE future rows for `(park,date,batch)` whose `operator_id` ∉ new plan, then upsert — fixes DEFOP-4/N-03 (operator-grain unique key `000022` currently double-inserts on re-point). Guard with `atomic-readmodel-sync-guard`.
- **Re-plan MUST route through the clinical guards** in `drive_planner.go` (per-animal shot cap, safe-window, live/killed spacing, ET+TT dose-2, kid/adult, clinical-defer set sick/under_treatment/quarantine/icu) — never a raw date/operator reassign. Combo/ET+TT dose-2 pairing (`AlignComboDrivesAsOf`) preserved.
- **Point fixes still open (other session owns; confirm landed):** F1 `totalVaccinationOperatorCap` (`sweeper.go:351`) must **return 0 when operators found but summed remaining==0** (not fallbackCap). F2 partial-attach must **clear `DriveAssignments` before create + write only scoped `attachedRows` after `attachedIDs`** (`sweeper.go:~1019`, `repository.go:3504`) so no stale rows survive.
- **Also fix:** OPS-CASCADE-2 (cap from `vaccination_capacity_config`, not `MAX` per-position), OPS-CASCADE-4 (availability `valid_from <= date` not `+1 day`), WKOFF-3/4 (drive-date scoring must honor week-off; empty candidate set → defer/flag, not a zero-operator batch at base cap).
- **Read-model alias sync:** any new grouped CT/AC/WF/Calendar read must keep producer↔consumer SQL aliases in sync + guarded (see memory `goatos-ctac-alias-contract-a9779fad`; verify on a FRESH build at the exact SHA — stale :8080 binary hides column drift).

## 6. Scale (ledger §C — must not regress)

- Operator availability/capacity/assignment: one shared session cache at `(tenant,park,business_date,cap)` grain; no N+1 fan-out (do NOT call `AvailableVaccinationOperatorsForDrive` per date/row).
- Reschedule/re-plan queries: keyset/`FOR UPDATE SKIP LOCKED`, SARGable, query-plan proof at ~500k obligation rows (no Seq Scan); `make validate-sqlc-plans`.
- CT/AC/WF/Calendar reads: projection/canonical-index serving per the 5k–50k envelope; hot-read p95/p99 < 500ms.
- admin-web SSR: no full-table request reads (`make admin-web-request-reads-guard`).
- mobile L1–L4: ≤~20-row keyset pages, Room SSOT, bounded memory, off-main parse (`make mobile-guard` etc.).

## 7. E2E (production path, published to CI report site)

One real E2E per trigger, each asserting raw DB (`obligation_batches` + `vaccination_drive_assignments`) AND every read screen stays correct:
- **Cap change** (raise/lower) → drives re-batch to new cap, no lost/dup obligations.
- **N change** 3→1 and 1→3 → assignments re-planned, no orphan/strand rows.
- **Default swap** → future days reassigned from swap date, history preserved.
- **Leave add** (range) → those days re-scoped/fallback, drives NOT cancelled, obligations preserved; **leave remove** → reversed.
Each must preserve clinical rules + per-animal shot cap + safe-window, prove idempotency (replay), and include query-plan + latency evidence. Publish on `https://vgoats.github.io/goatos/` per the E2E publishing rule.

## 8. Process (no loops)

Phased; each phase: **red test reproducing the exact failure** → root-cause fix → **independent judge re-verify on the real path** → `make ci-local` on the exact SHA → `make land-main`. Use multiple judge agents at each step. Suggested order:
1. Confirm F1/F2 landed (other session).
2. FE merged screen wired to existing endpoints (roster/week-off/leave/cap) + N/default disabled-with-reason. Land.
3. BE: common-cap-write + OPS-CASCADE-2/-4 + same-date leave BE guard. Land.
4. BE: N + default-operator config + bootstrap contract → enable FE N/default. Land.
5. BE: the cascade (domain events + consumer + set-reconciling write + clinical re-plan) with the 4 E2Es. Land.
6. Update skills refs (`kernel-scale-lens`, `scale-anti-patterns`, `frontend-anti-patterns`, `mobile-anti-patterns`, `goatos-build`) + `AGENTS.md` CPT invariant + add cascade-completeness / same-date-leave machine guards.

Never trust the live :3300/:8080 stack as proof — verify on a fresh build at the exact pushed SHA.
