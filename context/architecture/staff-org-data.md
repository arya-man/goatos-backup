# Goat OS — Staff / Org Data (extracted from Staff-Timetable + Attendance DB)

Status: reference data extracted from the live `Staff-Timetable` Google Sheet
(all role/duty tabs) and the `Attendance DB` `Employee Master`, on 2026-07-13.
Companion to [`org-role-model.md`](./org-role-model.md) (the derived role model).
This is the raw org/staff picture the RBAC + role×module design must match. Not
all of it is built; it is the source-of-truth shape + future scope. Names are
omitted (role-based per repo policy); this captures roles, counts, duties,
cadence, and structure only.

## 1. Staff inventory — 40 staff, 3 parks
`Bangalore = 6 (central: Heads, HR, Operation) · CBE = 20 · CPT = 14 (ground)`

Designation → headcount (park breakdown):

| Designation (tier) | Count | Parks |
| --- | --- | --- |
| Feeding Assistant Manager | 5 | CBE 3, CPT 2 |
| Health Manager 1 / 2 | 3 + 4 | CBE/CPT |
| Crops Manager | 3 | CBE 2, CPT 1 |
| Cleaning Assistant Manager | 3 | CBE 2, CPT 1 |
| Backup Assistant Manager | 3 | CBE 2, CPT 1 |
| Crops Assistant Manager | 3 | CBE 2, CPT 1 |
| Backup Manager | 2 | CBE 1, CPT 1 |
| Feeding Manager | 2 | CBE 1, CPT 1 |
| Milking Assistant Manager | 2 | CBE 1, CPT 1 |
| Milk Manager / Milking Manager / Farm Manager | 1 each | CBE/CPT |
| Crops Watering Assistant Manager | 1 | CBE |
| Health Head / Infrastructure Head / Farming Head / Feeding Head | 1 each | Bangalore |
| HR Manager / Operation | 1 each | Bangalore |

Reading: **ground tiers (Manager + Assistant Manager) live at the parks (CBE/CPT);
Heads are central (Bangalore)**; Directors + CEO/CxO are the 5-founder leadership
cohort (not in the operational roster). "operator" in RBAC ≈ Assistant Manager.

## 2. Tiers (Hirarchy tab: `Director → Heads → Managers`, + AM below)
`CEO / CxO  →  Director (per vertical)  →  Head (Ops-Head)  →  Manager  →  Assistant Manager (AM, ground)`

## 3. Verticals / departments (Park-Departments tab) — 9
`Procurement · Preventive Care · Breeding · Health · Growth · Infrastructure ·
Feed · Milk · Sales`

## 4. Role duties (from the per-role Director/Head/Manager/AM tabs)

**Preventive-Care-Director** — owns: Vaccinations, Deworming, Bio-Security, Feed
Testing, Water Testing/Cleaning, Feed-Panel Cleaning, Shed Sanitization, Fire
Safety, Team Building. Explicit: *"all execution SOPs are double-verified by
meeting the video verification team every day"* + keep ≥2 weeks stock (e.g.
vaccines). So Preventive Care contains **many modules**; Vaccination is one.

**Breeding-Director** — adult-goat breeding (AI / natural breeding / embryo
transfer), pregnant-goat care (medicines, vaccines, nutrition, shiftings,
delivery + post-delivery care), stock (sponges, semen, embryos, AI equipment).

**Weekly SOP cadence (all Directors, same pattern):** `Mon = plan · Tue =
arrange logistics · Wed–Sat = execute · daily = video-verification review`.
Directors execute *through* park/goat/infra Heads — they don't do ground work.

**Head-Duties tab** is the shed-level EXECUTION LOG format (what the ground
captures), columns: `Date · Farm(park) · Shed · Session · Scheduled Time · start
time · Feed · Feed Type · Animal variety · Number of Kids/Adults · Fed Quantity ·
Scheduled Quantity · Violation · Quality · Feed Wastage · Comments`. This is the
per-session operational record operators fill — the analog of the vaccination
drive submission.

## 5. Parks & sheds
Parks: **CPT, CBE** (operational) + **Bangalore** (HQ). Sheds are named/coded
(e.g. `K0–K3`, `G1–G3`, plus named sheds like `Yashodha`, `Castro`), split by
kids/adults and species (Goat / Sheep). Sessions are coded (`S-1`, …). This
matches the Goat OS park→shed model already in the product.

## 6. Attendance DB structure (Attendance DB.xlsx — 68 tabs)
- **Employee Master** — the roster (Name, Gender, DOB, Designation, Location/park,
  Salary, DOJ). The authoritative person→role→park map (§1).
- **~60 monthly attendance tabs** (`October 21` … `February-26`) — daily presence
  records per month. Presence data, NOT role structure; not reproduced here.
- **Summary · By-Category · Assessment · Warehouses-Details · PAN Details** —
  aggregates + master data (warehouses = the park/store locations; PAN = payroll).

## 7. What this means for Goat OS (future scope)
- Role is `(tier, vertical, park)` — see `org-role-model.md`. The staff data
  confirms the ground tiers (Manager/AM) are park-resident and vertical-specific
  (Feeding Manager ≠ Health Manager ≠ Milk Manager), so a flat "operator" role is
  insufficient once >1 module ships.
- Every vertical follows the same **plan→logistics→execute→verify** weekly cadence
  and produces a **shed-session execution log** — the vaccination drive is one
  instance of a general execution+proof pattern the platform should generalize.
- The **video verification team** is a real daily-standing role across every
  vertical's Director — a first-class role to model, not vaccination-only.

## 8. Future: HRMS module source data
The `Staff-Timetable` sheet + `Attendance DB` (Employee Master + ~60 monthly
presence tabs + Summary/By-Category/Warehouses/PAN) are the **source data for the
future HRMS module** (People / HR vertical). When HRMS is built it should ingest:
person→designation→park→DOJ→salary (Employee Master), daily attendance/presence
(monthly tabs), role/duty definitions (per-role tabs), and the roster/shift +
week-off + backup structure (Goats-Team-v1). Keep this as the migration source;
do not hand-invent an HR schema — port from these sheets. Not built yet; noted
so the HRMS build starts from real data.
