# Vaccination Drive Operator Scheduling — Design (for ratification)

Status: **DESIGN — pending sign-off + implementation.** Do NOT seed/migrate/wire
kernel behavior until ratified and the review ledger (see §7) is closed. Source:
maintainer decisions in mock review session, 2026-07-23.

Scope: park **CPT / Channapatna**. This is the HRMS "Vaccination operators"
screen (roster + availability + drive-operator assignment) **and** the kernel
contract that makes cap / operator-count / leave / week-off changes cascade
correctly into obligations, drive assignments, reschedules, and every read model
(Control Tower, Action Center, Workflows, Calendar, Vaccination L1–L4).

---

## 1. Core model — configurable active-operators-per-day (N)

One knob `active_operators_per_day` (N) selects behaviour per drive/park:

| N | Mode | Behaviour | Daily capacity |
|---|---|---|---|
| **3** | Parallel (existing) | all available operators run together | up to N × cap |
| **2** | Pair | top-2 available run together | up to N × cap |
| **1** | Single + fallback | one operator runs; default CEO-set + fallback chain | cap |

- Committed baseline stays **N=3** until a drive/park is set to N=1.
- **Fallback chain (N=1)**: `default (CEO-set) → next by order → …`, first
  available wins; auto-reverts to default the next day; a CEO manual swap sticks
  for the rest of the drive.

## 2. Common cap (not per-operator)

- **One `cap_per_operator` value applies to ALL operators** (not per-seat).
- Edited once (single inline control). Validation: integer ≥ 1; present-but-out-of-range
  **rejected**, never silently defaulted (authored-config rule).
- `Daily capacity = N × cap`.

## 3. Leave = date ranges (not a boolean, not whole-week)

- Each operator has **planned-leave date ranges** (`{from,to}`), future-only.
- **Invariant: never zero operators on any drive day.** Week-offs are disjoint
  (Fri/Sat/Sun). Enforced as: **no two operators may be on leave on the same
  date** (a second same-date leave would empty a fallback day). Enforced **FE +
  BE**.
- Adjacent/overlapping ranges for one operator **merge** into one span.
- Past dates, own already-planned dates, and other operators' dates are not
  selectable.

## 4. Week-off

- Recurring, one weekday per operator (CPT: Darshan Sun, Sagar Sat, Amit Fri).
- Removes that operator from availability that weekday (recurring).

## 5. HRMS screen UX (mock: `scratchpad/hrms-drive-operator-mock.html`)

Single screen (the two old tabs — "Vaccination Operators" + "Operator Timetable"
— are **merged**). Sections:

### 5.1 KPI row (all reactive)
- **Operators** — active seat count.
- **Cap / operator** — the common cap; subtitle `default · N custom` (0 custom now that cap is common).
- **Operators / day** — N, subtitle mode (`single + fallback` / `pair` / `all parallel`).
- **Daily capacity** — `N × cap`; recomputes on cap Save and N change.

### 5.2 Operator roster & availability (one card)
- Columns: `Person | Park | Week off | Planned leave | Weekly schedule | Status`.
- **Common-cap control** in the card header: value + ✏️ Edit → inline number →
  Save (Enter saves, Esc cancels). One value for all.
- **Planned leave cell** = compact: next upcoming range + `N planned · X more
  upcoming`, never a growing chip wall. Clickable → right drawer (§5.4). A
  **Manage** button → calendar dialog (§5.3). Operators with no leave show `＋ Add leave` → dialog.
- **Weekly schedule** = 7 cells Mon–Sun, On/Off (recurring week-off).
- **Status** = **today's** availability: `On leave today` / `Week-off today` /
  `Available` (NOT a duplicate leave count).

### 5.3 Add-leave calendar dialog (modal)
- Themed month calendar, ‹ › nav, range select (click start → click end), live range label.
- Blocked/greyed: **past** (muted), **own already-planned** (amber, click-to-remove),
  **booked by another operator** (red). A range may not span a blocked/planned date.
- Close: Add leave (commits + closes) / Cancel / ✕ / Esc / click-outside.
- Legend: Selected · Already planned · Booked by another.

### 5.4 Leave drawer (right slide-in)
- Opened by clicking the leave summary text.
- Lists ALL leaves grouped **Upcoming / Past** (past dimmed), each with day count + remove.
- Footer **＋ Add leave** → opens the calendar dialog on top; adding refreshes the drawer.
- Close: ✕ / click-outside.

### 5.5 Drive operator assignment (below roster)
- **Active operators / day** select (1/2/3) — the mode switch.
- **Default operator** select (enabled only when N=1); change → CEO-swap banner.
- **Fallback chain** strip (→ for chain when N=1, + for parallel).
- **Weekly assignment preview** — `Day | Assigned operator | Reason`, Mon–Sun, recomputes live.
- No internal jargon on the CEO page (no `cap=1/day` pills, no dev validation footnotes).

## 6. Kernel cascade contract (the hard part — must be stitched)

Any change to **cap**, **N**, **default operator**, **week-off**, or **leave**
must correctly propagate through the operational kernel WITHOUT leaving stale or
orphaned state. For each trigger, define + test the cascade:

| Trigger | Must happen |
|---|---|
| **Cap changed** | Re-evaluate drive-day capacity; existing scheduled drives re-batch to new cap (respect per-animal shot cap, safe-window); no obligation lost or duplicated; read models refreshed. |
| **N changed** (mode) | Re-plan operator assignment per day from the new N; parallel↔single transitions must not orphan drive_assignments. |
| **Default operator changed** | Reassign future drive days from swap date; no auto-revert; preserve completed history. |
| **Leave added** | Days in range lose that operator → fallback re-assign (N=1) or reduce parallel slice; capacity for those days drops; drives on those days re-scoped, NOT cancelled; obligations preserved. |
| **Leave removed** | Reverse of add; days regain the operator; re-optimise assignment. |
| **Week-off** (already recurring) | Same recurring exclusion; ensure planners already honor it everywhere. |

Non-negotiables (existing rules — must not regress):
- Clinical defer set (sick/under_treatment/quarantine/icu) always deferred.
- Kid/adult path, boosters, live/killed spacing, ET+TT dose-2, safe-window,
  per-animal shot cap all preserved through any re-batch/reschedule.
- Reschedule = **kernel writes** to raw `vaccination_drive_assignments`, not a
  read-time `COALESCE(override_date)` sidecar. Move + revert both prove raw DB rows.
- Goat lifecycle stitching: shed move / stage change / transferred-sold exits
  must re-scope operator-driven drives too.

## 7. Review + implementation gate (no loops)

Before implementation, an adversarial multi-agent review (last-30-commit review
lens + kernel cascade audit + scale lens) produces a **gap ledger**: every place
cap/N/leave/default change fails to clear or reschedule obligations/drives, plus
every N+1 / hot-read latency / SSR-full-table / mobile over-fetch / slow-render
risk across CT / AC / WF / Calendar / Vaccination L1–L4. Each finding is
independently judged (confirmed/plausible) before it enters the plan.

Implementation is **phased**, each phase: red test reproducing the exact failure →
root-cause fix → judge re-verify on the real path → `make ci-local` on the exact
SHA → `make land-main`. Real E2E (production path, published to the CI report
site) for: cap change, leave add/remove, N change, default swap — each proving
obligations + drive_assignments + all read models stay correct, with query-plan
proof (no Seq Scan at the 5k–50k envelope) and the hot-read latency gate green.

## 8. Skills / refs / guards to update

- `.claude/skills/goatos-build` + `kernel-scale-lens` + `scale-anti-patterns` +
  `frontend-anti-patterns` + `mobile-anti-patterns` refs: add the operator-config
  cascade lens.
- `AGENTS.md` CPT operator invariant: update from "3 equal operators" to the
  configurable-N + common-cap + leave-date model (once ratified).
- Add machine guards where feasible (cascade completeness, same-date leave conflict).
