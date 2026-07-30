# Current Active RBAC Roles

This is the operational role list for current STG/mobile/admin work. Do not
grant roles outside this list unless a feature explicitly ships support for
that role in backend permissions, Android navigation, seed docs, and tests.

## Used Roles

| Role key | Current meaning | Current scope rule | Current users |
| --- | --- | --- | --- |
| `ceo_internal` | CEO/CXO/founder visibility and override | `tenant` | founder/builder cohort |
| `operator` | Ground execution and scanning for the modules explicitly granted to that person | `park` only for real field users | Amit, Darshan, Sagar, Pramod, Kumar Sharath |
| `pc_director` | Preventive Care Director; Vaccination visibility/action across parks | `tenant` when both parks are needed | Chandrakant |
| `growth_director` | Growth Director; Weighing visibility/action across parks | `tenant` when both parks are needed | Dinakar |
| `verifier` | Video verification review | `tenant` unless narrowed by future verification assignment rules | Jyothi / verifier users |

## Dormant Catalog Roles

`org_role_catalog` also contains composite tier/vertical roles such as
`director_preventive_care`, `director_breeding`, `manager_feed`, `head_health`,
and `am_growth`.

Those rows are **catalog scaffolding only right now**:

- no active user is granted them in STG/local seed data
- Android and seed docs should not treat them as live personas
- do not use them as shortcuts for current mobile access
- if one is activated later, the change must update backend permissions,
  Android role handling, seed/runbook docs, and tests in the same commit

`pc_director` is the current live role for Preventive Care Director. It can open
Vaccination cards, scan/capture, submit, and use vaccination close flows. It must
not get Weighing.

`growth_director` is the current live role for Growth Director. It can open
Weighing tasks across parks, scan/capture, submit, monitor videos, and reopen a
completed weighing shed bucket. It must not get Vaccination.

## Hard Rule

Real operators must never get tenant-scoped `operator` grants. Operators are
park-scoped and module-scoped: a CBE weighing-only operator must not get
Vaccination execution just because their role is `operator`. If a director needs
to scan across both parks, use the director role for that feature:
`pc_director` for Vaccination, `growth_director` for Weighing. Do not make the
`operator` grant tenant-wide.

Android must not decide this from role labels. `/app/bootstrap` owns nav and
feature flags:

- `vaccination_execute=true` means shed cards may enter Vaccination Scan/Submit.
- `weighing_execute=true` means Weighing opens the field execution UI instead
  of the monitor/video display UI.
- `false` means display/review/monitor only.

FCM follows the same action hierarchy. Field completion flows upward to the
owning director/CEO/review side; rework/reopen flows downward to the assigned
operator and keeps the owning director/CEO in the loop. Vaccination uses PC
Director as the owning director. Weighing uses Growth Director as the owning
director.
