# Goat OS Org / Role Model — current + future scope

Status: reference for role×module access design. Derived from the live
`Staff-Timetable` sheet (Hirarchy, Park-Departments, per-role tabs) and the
`Attendance DB` Employee Master. This is the real-world org the RBAC must model;
it is NOT all built yet — it captures the target so the foundation is built right.
Do not build the whole thing from this; it is the shape + future scope.

## The real org is a 3-axis matrix: TIER × VERTICAL × PARK

### Tiers (top → bottom)
`CEO / CxO  →  Director  →  Head (Ops-Head)  →  Manager  →  Assistant Manager (AM)`

- **CEO / CxO** — the platform-owner cohort. The 5 founder emails
  (`ravi@`, `manohark@`, `manju@`, `abhishek@`, `aryaman@` mesha.sg) are all
  treated as CEO/CxO: full visibility + approve/reject, no ground execution.
- **Director** — owns a vertical (e.g. Preventive Care Director, Health Director,
  Breeding Director). Oversight + approval, sets SOPs, not ground execution.
- **Head (Ops-Head)** — vertical head at the park level (Health-Ops-Head,
  Feeding-Ops-Head, Growth-Head, Farm-Goats-Head). Oversight + approval.
- **Manager** — runs a vertical's day-to-day at a park (Feeding Manager, Health
  Manager 1/2, Milk Manager, Backup Manager, Crops Manager). Ground-adjacent:
  supervises AND can execute/capture.
- **Assistant Manager (AM)** — the ground executor / "operator" (Feeding AM,
  Milking AM, Cleaner AM, Breeding/Health AM, Crops AM, Manure AM, Harvestor AM).
  Physically in the shed doing the work.

### Verticals / departments (the "modules") — 9
`Procurement · Preventive Care · Breeding · Health · Growth · Infrastructure ·
Feed · Milk · Sales`

- A vertical contains one or more **modules** (workflows). Example: **Preventive
  Care → Vaccination** (the one built), plus Deworming, Bio-Security, Feed/Water
  Testing, Shed Sanitization, Fire Safety (from the PC-Director responsibilities).
- Future modules land under their vertical; the app must show a person only the
  module(s) their vertical/assignment grants.

### Park scope
Every person is scoped to a **park** (Employee Master `Location`: CBE, CPT,
Bangalore, …). A "Feeding Manager, CPT" is distinct from "Feeding Manager, CBE".

## So a role is `(tier, vertical, park)` + module assignment
Not a single flat string. "Operator" is not one role — it is an AM (or Manager)
**in a specific vertical, at a specific park**. A Vaccination AM ≠ a Feeding AM;
one person may hold more than one module.

## Vaccination capture vs verify — the business mapping
- **Capture (record the proof videos, RFID-scan goats, submit the drive)** =
  GROUND tiers only: **Assistant Manager (AM) + Manager**, in the Preventive
  Care / Vaccination assignment, at their park. They are physically present.
- **Approve / reject (review the proof videos)** = **Head + Director + CEO/CxO**,
  plus the dedicated **Video Verification Team** (the PC-Director SOP requires
  execution SOPs be "double verified by meeting the video verification team every
  day"). These roles do NOT go to the ground and record.
- A CEO/Director/Head recording a vaccine going into a goat is not a real
  scenario — the earlier permission matrix that granted `TaskExecute` (capture)
  to `park_head`/`pc_director`/`ceo_internal` is business-wrong and must be
  corrected: capture affordance is ground-only.

## Current RBAC vs this model (the gap)
Today `backend/internal/permissions/permissions.go` has **4 flat roles**:
`operator`, `park_head`, `pc_director`, `ceo_internal`. That is a vaccination-only
simplification of the matrix above:

| This model | ~ maps to today | gap |
| --- | --- | --- |
| AM / Manager (ground, per vertical) | `operator` | no vertical/module scope; Manager tier not distinct |
| Head (Ops-Head) | `park_head` | park-scoped, not vertical-scoped |
| Director | `pc_director` | one director role, not per-vertical |
| CEO / CxO | `ceo_internal` | ok (5 founders) |
| Video Verification Team | — (uses `VaccinationVerify`) | not a distinct role yet |

Missing to reach the target:
1. **Role carries vertical + module + park**, not just a tier string.
2. **Mobile nav is module-grant-driven** — the operator sees only their
   vertical's module(s) (Vaccination now; Feed/Deworming/etc. later), derived
   from `department_module_grants` (mig `000148`) — NOT the current hardcoded
   `operatorNavigation = [vaccination, calendar, alerts]`.
3. **Capture gated to ground tiers (AM/Manager)**; approve/reject to
   Head+/Director+/CEO + Video Verification Team.
4. **Park scope** already exists in grants (`ActiveGrant.ScopeType/ScopeID`);
   keep it as a hard filter.

## Scope note
Vaccination ships correctly today on the flat 4-role RBAC because it is the only
module. The matrix above is the near-term foundation to build BEFORE the 2nd
module (Deworming / Feed) so a new vertical does not require rewiring roles/nav.

## Truth table — who can DO what (target model)

Role → job:

| Role | Job |
| --- | --- |
| Operator / Assistant Manager (AM) | ground execution — do the task, capture proof (RFID scan + record videos), submit |
| Manager (per vertical, per park) | run the vertical's daily ops, supervise AMs, can also capture, manage local roster |
| Verifier (NEW — the Video Verification Team) | watch the uploaded media, approve/reject + mandatory reason. Only that. |
| Head (Ops-Head) | park/vertical oversight + standards; act on verified items |
| Director (per vertical) | own the vertical — plan/logistics/oversee execution, set SOPs, act, penalise |
| CEO / CxO (5 founders) | full authority |

Capability matrix (✅ yes · ❌ no · ~ partial/scoped):

| Capability | Operator/AM | Manager | Verifier | Head | Director | CEO/CxO |
| --- | :--: | :--: | :--: | :--: | :--: | :--: |
| Execute + capture proof (ground) | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ |
| Verify media (approve/reject + reason) | ❌ | ❌ | ✅ | ❌ | ❌ | ✅ override |
| Record count events — birth / death / shifting (`counts.write`) | ✅ | ✅ | ❌ | ❌ † | ❌ | ✅ |
| View tenant-wide census — herd register + counts breakdown (`counts.read`) | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ ‡ |
| Act on verdict (accept/escalate/penalise) | ❌ | ~ local | ❌ | ✅ | ✅ | ✅ |
| Manage roster / operators | ❌ | ~ local | ❌ | ✅ | ✅ | ✅ |
| Plan + own SOPs (weekly cadence) | ❌ | ❌ | ❌ | ~ park | ✅ | ✅ |
| Publish SOP / protocol | ❌ | ❌ | ❌ | ❌ | ~ SOP | ✅ |
| Mobile surface | Capture (own module) | Capture (vertical) | standalone Verifier section | read-only overview | read-only overview | read-only |
| Web surface | — | roster/local | Verification queue | act + oversight | plan/act/config | everything |

Hard rules: capture = ground only (Operator + Manager); verify = Verifier only
(human video team); act = Head/Director/CEO. Separation of duty — nobody both
captures and verifies the same work.

### Counts authority — capture ≠ census (maintainer decision 2026-07-18)

Counts splits into **two different authorities**, and they are two different
permissions:

- **`counts.write`** — recording the count-moving field events (birth, death,
  shifting) as ground truth. It follows the same capture-is-ground-only rule as
  proof capture: held by the AM and Manager tiers, never by the Head or Director
  tiers, and never by the Verifier (separation of duty).
- **`counts.read`** — the tenant-wide census (herd-register summary + counts
  breakdown). This is an **oversight** authority, not a capture one. A person who
  records births and deaths for their own park does **not** thereby get a whole-herd
  population view. It is deliberately split off `goat.read` and narrower than it:
  `goat.read` is held by nearly every role, so reusing it would have made the census
  effectively public. Enforced on the routes (`GET /counts/breakdown`,
  `GET /herd-register/summary`), so a hidden census page is unreachable, not merely
  invisible.

† **Flat-role divergence (a known gap-table case, not a contradiction).** The flat
`park_head` role *does* hold `counts.write`, while the target-model **Head tier does
not** — the flat roles keep their broader vaccination-only-era sets per the gap table
above. Treat the Head column as the target; `park_head` is the legacy flat role.

‡ In today's flat RBAC, `counts.read` is held by exactly **`admin` + `ceo_internal`**.
No composed tier role holds it. `pc_director` and `verifier` hold **neither** counts
permission, so the Counts module is omitted from their nav entirely. `pc_director`
previously held `counts.write` and lost it in this decision — which moves the flat
Director role *into* alignment with the target `Director` column above (capture is not
a Director affordance), closing part of gap item 3. The per-role page matrix and how
nav composes from it: `docs/decisions/role-module-nav-composition.md`.
