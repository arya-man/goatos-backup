# Current Active RBAC Roles

This is the operational role list for current STG/mobile/admin work. Do not
grant roles outside this list unless a feature explicitly ships support for
that role in backend permissions, Android navigation, seed docs, and tests.

## Used Roles

| Role key | Current meaning | Current scope rule | Current users |
| --- | --- | --- | --- |
| `ceo_internal` | CEO/CXO/founder visibility and override | `tenant` | founder/builder cohort |
| `operator` | Ground execution and scanning | `park` only for real field users | Amit, Darshan, Sagar, Pramod, Kumar Sharath |
| `pc_director` | Preventive Care Director visibility/action plus vaccination/weighing execution | `tenant` when both parks are needed | Chandrakant, Dinakar |
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

`pc_director` is the current live role for Preventive Care Director. Business
meaning is the same as "Director - Preventive Care", but the active code path is
the legacy flat key `pc_director`. In the mobile app, this role can open
Vaccination and Weighing execution cards, scan/capture, and submit because it
holds `task.execute` and `weighing.execute`.

## Hard Rule

Real operators must never get tenant-scoped `operator` grants. If a director
also needs to scan across both parks, use the `pc_director` role plus explicit
task/shed assignment where the workflow requires an owner. Do not make the
`operator` grant tenant-wide.

Android must not decide this from role labels. `/app/bootstrap` owns nav and
feature flags:

- `vaccination_execute=true` means shed cards may enter Vaccination Scan/Submit.
- `weighing_execute=true` means Weighing opens the field execution UI instead
  of the monitor/video display UI.
- `false` means display/review/monitor only.
