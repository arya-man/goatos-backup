# CPT Adult Vaccination History Repair - 2026-08-05

## Scope

This records the local validation repair for the CPT adult vaccination-history
collapse found after the 2026-08-05 STG seed.

The repair was validated locally only. Do not apply it to STG until the owner
has checked the UI backed by the clubbed validation database.

## Source Databases

- Pre-collapse STG backup: Cloud SQL backup `1785879718314`
  (`pre-cbe-cpt-vaccination-port-2026-08-05`, started
  `2026-08-04T21:41:58Z`).
- Local pre-collapse copy: `127.0.0.1:15634/goatos`.
- Local current-STG copy: `127.0.0.1:15633/goatos`.
- Local clubbed validation DB: `127.0.0.1:15635/goatos`.
- UI/API validation code: `origin/main`
  `7c0eadbc39d39f16c34bec99aa14fb5ac37ba16d`.
- Clubbed validation DB migration head before serving:
  `000107_notification_requeue_tracking`.

Default local DB and mobile throwaway DBs were not used as evidence and were not
modified.

## Repair Result

For CPT true adults (`Non-Pregnant` and `Buck`) and ET+TT adult booster
(`et_tt_adult_w2`), the clubbed DB has:

```text
2026-07-24 administered / 2026-07-24 completed = 114
2026-07-25 administered / 2026-07-25 completed = 163
2026-07-26 administered / 2026-07-26 completed = 47
total = 324
```

The current-STG copy had the collapsed completion date:

```text
2026-07-24 administered / 2026-07-24 completed = 114
2026-07-25 administered / 2026-07-24 completed = 163
2026-07-26 administered / 2026-07-24 completed = 47
```

The repair updates `obligation_instances.completed_at` to match the real
administration dates and sets accepted completion verification metadata to
Jyothi on `2026-07-27 18:30:00+05:30`.

## Restored From Pre-Collapse

These ET+TT dose-1 rows existed in pre-collapse STG and were missing from
current STG. They were restored into the clubbed DB:

```text
G-000106 et_tt_adult_w1 2026-06-30 accepted
G-000254 et_tt_adult_w1 2026-07-01 accepted
G-000255 et_tt_adult_w1 2026-07-01 accepted
```

After restore, CPT adult ET+TT dose 1 is:

```text
2026-06-30 = 85
2026-07-01 = 239
total = 324
```

The old pre-collapse batch linkage was restored for the 114 ET+TT booster rows
that pre-collapse STG actually had. The July 25 and July 26 booster rows do not
have recoverable old batch ids in the pre-collapse backup.

## Video / Review Metadata

SOP task:

```text
02e44a4f-0cd3-4f1e-8d5e-395f43dd2505
```

Clubbed DB state:

```text
task state: accepted
verified_by: 2050cd6e-e02b-5681-be7d-4a78a508102e (Jyothi)
verified_at: 2026-07-27 13:00:00+00
closed_by: Chandrakant
closed_at: 2026-07-27T18:30:00+05:30
```

The video proof refs were preserved:

```text
4 submissions
6 proof refs total
submission proof counts: 1, 1, 1, 3
submitted range: 2026-07-25 to 2026-07-27
```

The clubbed repair accepts the four submissions and 209 submission items for
that task. It does not delete or rewrite the proof refs.

## Future Data And Weighing

Current-STG and clubbed-local hashes matched for CPT adult future obligations
from 2026-08-05 onward:

```text
edc24db38c29da33114b631d037a3d96b84fd3c1a29b56c6f256ce2b0a7ed420
```

Current-STG and clubbed-local weighing data matched:

```text
weighing_observations: 317 rows, identical hash
weighing_shed_observations: 11 rows, identical hash
```

## Integrity Checks

Compared with current-STG copy, clubbed-local has:

```text
vaccination_completions +3
obligation_instances +3
obligation_batches +1
broken vaccination -> obligation FK = 0
broken batch FK = 0
broken rule FK = 0
```

The read-only validator is:

```text
docs/runbooks/cpt-adult-vaccination-history-repair-2026-08-05/validate.sql
```

It passed against the clubbed validation DB on 2026-08-05 after migrating the
DB to `000107_notification_requeue_tracking`.

Prepared guarded repair SQL:

```text
docs/runbooks/cpt-adult-vaccination-history-repair-2026-08-05/repair.sql
```

It refuses to run unless called with `-v apply_repair=yes`, and it requires the
pre-collapse staging tables under `forensic_repair` to already be loaded from
the pre-collapse CSV exports. Run the validator before and after any approved
STG repair.

## Caveat

The exact per-animal assignment for the 163 goats on July 25 and the 47 goats on
July 26 is reconstructed from current-STG accepted completion dates and a
deterministic display-id order, not from one proof item per animal. The system
has shed-level video proof, and the submission item total is 209, not 324, so a
complete 324-row item-to-video mapping cannot be recovered from the database
alone.

## Local Validation URL

The validation UI was run from clean `origin/main` code against the clubbed DB:

```text
API: http://127.0.0.1:18080
admin-web: http://127.0.0.1:13300
page: http://127.0.0.1:13300/vaccination?scope_mode=park&park=00000000-0000-4000-8000-000000003002
```
