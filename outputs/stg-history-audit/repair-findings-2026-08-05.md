# GoatOS STG Vaccination Repair Findings - 2026-08-05

## Boundary

- No STG database writes have been done.
- This note documents the current forensic findings and the local repaired comparison only.
- Secrets, DB passwords, signed URLs, and auth tokens are intentionally not included.

## Local Comparison Databases

- Local Postgres port used for isolated comparison: `127.0.0.1:15635`.
- Raw current STG clone: `goatos_stg_latest_raw_20260805`.
- Clubbed repaired local clone: `goatos_stg_latest_clubbed_20260805`.
- Frontend/API validation was against the clubbed local clone, not by mutating STG.

## Live STG CPT Adult ET+TT Problem

Current STG still has the collapsed CPT true-adult ET+TT history:

- `et_tt_adult_w1`: `84` on `2026-06-30` + `237` on `2026-07-01` = `321`.
- `et_tt_adult_w2`: `324` all on `2026-07-24`.

This is not the expected history for the CPT adult work.

## Clubbed Repaired Local Facts

The local clubbed DB reconstructs CPT true-adult ET+TT as:

- `et_tt_adult_w1`: `85` on `2026-06-30` + `239` on `2026-07-01` = `324`.
- `et_tt_adult_w2`: `114` on `2026-07-24` + `163` on `2026-07-25` + `47` on `2026-07-26` = `324`.

The three W1 animals restored from pre-collapse are:

- `G-000106`
- `G-000254`
- `G-000255`

Proof/closure data for the repaired CPT adult W2 history is preserved locally:

- `4` SOP submissions.
- `6` proof refs.
- Accepted by verifier flow.
- Closure/verification remains associated with the repaired history instead of being replaced by fake per-animal proof.

## Aug 5 And Future Preservation Checks

Current STG was pulled again after the latest Aug 5 work. Raw current STG and the clubbed local DB matched for the post-collapse/current data checks:

- Vaccination completions administered on or after `2026-08-05`: `137`, matching raw and clubbed.
- Future obligations due on or after `2026-08-05`: matching raw and clubbed.
- SOP submissions submitted on or after `2026-08-05`: `15`, matching raw and clubbed.
- SOP submission items for Aug 5 submissions: `137`, matching raw and clubbed.
- SOP tasks for Aug 5 submissions: `15`, matching raw and clubbed.
- Proof refs for Aug 5 submissions: `15` submissions with `36` proof refs, matching raw and clubbed.
- Weighing rows are preserved:
  - `weighing_observations`: `317`.
  - `weighing_shed_observations`: `11`.

Live STG Aug 5 vaccination summary observed during the read-only check:

- `CBE / Sumathi 1 / et_tt_adult_w2 / recorded / 2026-08-05`: `76`.
- `CBE / Sumathi 2 / et_tt_adult_w2 / recorded / 2026-08-05`: `58`.
- `CBE / Yashoda / et_tt_kid_7w / recorded / 2026-08-05`: `3`.

## Validation Pass Summary

The local clubbed DB passed the repair validation script:

- CPT adult W2 split restored.
- CPT adult W1 restored to `324`.
- Pre-only goats restored.
- Video task/proof closure preserved.
- SOP submission items accepted.
- Future CPT adult obligation hash matched current STG.
- Weighing observation hashes matched current STG.
- Foreign-key integrity passed.

Validation script:

`docs/runbooks/cpt-adult-vaccination-history-repair-2026-08-05/validate.sql`

## Repair SQL

Repair SQL path:

`docs/runbooks/cpt-adult-vaccination-history-repair-2026-08-05/repair.sql`

Guardrails:

- Requires staging tables under `forensic_repair`.
- Requires explicit `-v apply_repair=yes`.
- Should be run only after a fresh STG backup and final sign-off.
- Does not directly mutate Pub/Sub or sweeper state. It repairs the DB rows those systems read from.

## UI Changes Summary

Local UI/backend changes prepared so the repaired data is understandable:

- Cohort matrix separates true Adults from F2/Fattening.
- Adults means true adult cohorts only, such as Non-Pregnant and Buck.
- F2 is not treated as Adults.
- Cell details open in a right-side drawer using the existing drawer pattern.
- Proof/closure details show accepted task, verifier/closure facts, and shed-level videos when available.
- Historical rows without proof show a no-proof state instead of inventing evidence.
- Deferred/canceled summary card remains simple and opens details in the drawer.
- Raw dose codes are mapped to readable labels for CEO-facing UI.

## Shed Partition Finding

The partition data is not absent, but it is stored differently from what the UI makes obvious.

Animals stay attached to the parent physical shed in `goats.shed_id`. Partition membership lives in `goat_shed_partitions`.

Example evidence from the local clubbed DB:

- `CBE / Castro`: `202` animals on parent shed, with `202` partition rows labelled `1`, `2`, `3`.
- `CBE / Castro 1`, `Castro 2`, `Castro 3`: inactive child location rows with `0` animals directly attached.
- `CPT / Castro`: `64` animals on parent shed, with `64` partition rows labelled `1`, `2`.
- `CPT / Castro 1`, `Castro 2`: inactive child location rows with `0` animals directly attached.

Layman meaning:

- The system did not put goats inside separate child shed records like `Castro 1`.
- It put goats inside parent `Castro`, then stored each goat's partition separately.
- Vaccination and weighing can still work if their queries read `goat_shed_partitions`.
- The confusing part is that UI dropdowns show shed names, while partitions are not exposed as first-class shed choices.

Risk:

- If any workflow only reads `goats.shed_id`, it sees all Castro goats under `Castro`.
- If a workflow also reads `goat_shed_partitions`, it can correctly split Castro into partition `1`, `2`, `3`.
- The DB is not proven globally broken from this finding alone, but the UI/model distinction must be documented and tested for every partitioned shed.

## Next Repair Decision

Before writing to STG:

1. Take a fresh STG backup.
2. Re-run the raw current STG pull.
3. Re-run the clubbed repair locally.
4. Re-run validation hashes for Aug 5 and future rows.
5. Confirm partitioned-shed workflows use `goat_shed_partitions` where needed.
6. Apply guarded repair SQL only after explicit approval.

## 2026-08-06 Recheck After Partition PR Review

PR 27 was reviewed against the clubbed repair concern. The polluted local proof
database `goatos_partition_proof` must not be used as preservation evidence,
because local startup/seed-closeout changed future CPT adult obligations there.

The untouched clubbed baseline remains:

- Database: `goatos_stg_latest_clubbed_20260805` on local port `15635`.
- Alive goats: `1670`.
- Alive goats missing `goat_shed_partitions`: `0`.
- Aug 5+ vaccination completions: `137`.
- Weighing observations: `317`.
- Weighing shed observations: `11`.

The full repair validator still passes on `goatos_stg_latest_clubbed_20260805`:

- CPT adult ET+TT W1 remains `85` on `2026-06-30` plus `239` on `2026-07-01`.
- CPT adult ET+TT W2 remains `114` on `2026-07-24`, `163` on `2026-07-25`,
  and `47` on `2026-07-26`.
- Video proof closure remains `4` accepted submissions and `6` proof refs.
- Future CPT adult obligation hash remains unchanged.
- Weighing hashes remain unchanged.
- FK integrity passes.

Clean migration-only check:

- A fresh DB was cloned from `goatos_stg_latest_clubbed_20260805`.
- Only PR 27 migrations `000110`, `000111`, and `000112` were applied.
- The full repair validator passed.
- The partition catalog seeded `130` rows, including empty partition
  `Coimbatore / Yashoda 5` from the location alias evidence.

Conclusion: PR 27 migrations do not inherently damage the clubbed CPT adult
repair. Any future STG repair must still start from a fresh backup and must not
use the polluted `goatos_partition_proof` clone as the baseline.

## 2026-08-06 Recheck After PR Merge To Main

Latest fetched `origin/main`: `514db6825c8c78cbf4368d029936cb41f173559a`.

Clean latest-main migration check:

- Fresh DB cloned from untouched repaired baseline:
  `goatos_stg_latest_clubbed_20260805`.
- Clone name: `goatos_main_cleancheck_025019` on local port `15635`.
- Applied current main migrations `000108` through `000113`.
- Full CPT adult repair validator passed on the migrated clone.

Preserved historical CPT adult rows on latest-main migrated clone:

- ET+TT adult W1: `85` on `2026-06-30`, `239` on `2026-07-01`.
- ET+TT adult W2: `114` on `2026-07-24`, `163` on `2026-07-25`,
  `47` on `2026-07-26`.
- Video proof closure: accepted task, `4` accepted submissions, `6` proof refs.

Raw/current STG clone vs repaired baseline vs latest-main migrated clone:

- Aug 5+ vaccination completions: `137` in all three DBs.
- Future CPT obligation instances: `1539` in all three DBs.
- Weighing observations: `317` in all three DBs.
- Weighing shed observations: `11` in all three DBs.

Merged code recheck:

- `source_partition_label` is now carried through the shifting write path on
  latest `main`; the pre-merge review gap is closed in code.
- `shed_partitions` catalog exists after `000112` and has `130` rows.

Deployment/repair conclusion from local evidence:

- Current `main` code and migrations are compatible with the repaired clubbed
  data.
- The manual STG repair is eligible only after a fresh live STG backup and fresh
  live STG pull immediately before the write.
- The intended repair remains narrowly scoped to historical CPT adult ET+TT
  rows/proof closure and should not touch Aug 5+ vaccination rows, future
  obligations, weighing observations, or shed weighing observations.
