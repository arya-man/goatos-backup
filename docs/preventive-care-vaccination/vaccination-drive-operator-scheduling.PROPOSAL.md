# Vaccination Drive Operator Scheduling — PROPOSAL (unratified)

Status: **PROPOSAL — awaiting maintainer sign-off.** Do NOT seed, migrate, or
wire kernel behavior from this doc until it is ratified. Source: maintainer
decision, 2026-07-23.

This proposal describes how a **vaccination drive** picks its executing
operator(s) per business day for park **CPT / Channapatna**. It is additive to
the existing HRMS "cap per operator" timetable view; it does not remove the
per-operator availability display.

---

## 1. Core model — configurable active-operators-per-day

The drive is parameterised by an **active-operators/day** setting, N. This single
knob selects the behaviour:

| N | Mode | Behaviour | Daily cap |
|---|---|---|---|
| **3** | Parallel (existing) | All available operators run the drive together (the current equal-operator model). | up to 3 × 200 = 600 |
| **2** | Pair | Top 2 available operators run together. | up to 2 × 200 = 400 |
| **1** | Single + fallback | Exactly one operator runs; default is CEO-set, with a fallback chain when unavailable. | 200 |

- The **committed default stays N=3** (existing seed/`timetable-panel` behaviour)
  until a drive/park is explicitly configured to N=1.
- The per-operator capacity view (each operator = 200/day) is unchanged; N only
  governs how many run on a given drive-day.
- Week-off / leave removes that operator from the available set for the day; the
  daily cap is `(#available, capped at N) × 200`.

Rules §3 below detail the N=1 single-operator + fallback behaviour.

---

## 2. Roster (CPT)

| Operator | Week-off | Shift (from `CPT_Nuanced Timetable.xlsx`) | Cap/day |
|---|---|---|---|
| Darshan Talwar | Sunday | midday rover 8:30am–6:00pm | 200 |
| Sagar Mahoor | Saturday | afternoon/night 3:00pm–12:00am | 200 |
| Amit Kumar | Friday | morning 7:00am–3:00pm | 200 |

Director **Chandrakant** = monitoring only, never an execution operator, no cap.

---

## 3. Rules

1. **Operator cap = 1 active operator per drive per day.** A drive is executed by
   exactly one operator on any given business day.
2. **Default operator is CEO-set at drive start.** Current default = **Darshan**.
3. **Fallback chain (first available wins):**
   `default (Darshan) → Sagar → Amit`.
   "Available" = not on week-off that day AND not on leave.
4. **Auto-fallback is per-day and reverts.** When the default operator is on
   week-off/leave, the drive falls to the next available operator **for that day
   only**; the next day it reverts to the default.
5. **CEO manual swap sticks.** If the CEO changes the drive operator mid-drive,
   the new operator continues for the **rest of the drive** (no auto-revert to the
   old default). Week-off/leave auto-fallback still applies on top of the new
   default.
6. **Leave coverage = shift counterpart, then chain.** If the default is on
   leave, their afternoon-shift counterpart (Sagar) fills; if that person is also
   unavailable, the chain continues to Amit.
7. **Never zero operators.** All three are never off the same day (week-offs are
   Fri/Sat/Sun — disjoint; leave overlap that empties all three is disallowed by
   policy). The chain is guaranteed to resolve to ≥1 operator every day.
8. **Invariant: exactly 1 operator/day — never 0, never >1.**

### Worked example — 1,000-goat drive, 200/day, start Thu 2026-07-23

| # | Date | Day | Operator | Why |
|---|---|---|---|---|
| 1 | 07-23 | Thu | Darshan | default |
| 2 | 07-24 | Fri | Darshan | default (Amit's off day, irrelevant) |
| 3 | 07-25 | Sat | Darshan | default (Sagar's off day, irrelevant) |
| 4 | 07-26 | Sun | Sagar | Darshan week-off → chain |
| 5 | 07-27 | Mon | Darshan | reverts next day |

### Edge case (resolved by rule 7)

Darshan on multi-day leave **and** the day is Sagar's week-off (Sat) → chain
continues to **Amit**. Because all three are never simultaneously off, the day is
always covered.

---

## 4. UI expectation (HRMS → Timetable screen)

Add a **"Drive operator assignment"** section under the existing weekly
availability card (same screen):

- Default-operator control (CEO-set) — pill + change action.
- Fallback-chain strip: `Darshan → Sagar → Amit` with the active link highlighted.
- Per-operator availability toggles (week-off shown, leave toggleable).
- A drive-day table that recomputes the assigned operator live as availability /
  default changes, with the reason per day and a running cumulative-animals total.

All controls are backend-contract-driven when built (default operator, chain
order, caps, leave come from roster config + a new drive-operator contract);
frontend owns only layout and local preview state.
