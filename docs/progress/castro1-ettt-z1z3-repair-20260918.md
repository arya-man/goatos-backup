# CBE Castro ET+TT / Z1+Z3 Repair - 2026-09-18

## Scope
- Keep ET+TT and Z1+Z3 as separate vaccine identities.
- Clean bad Z1+Z3 rules carrying ET+TT source metadata.
- Seed accepted ET+TT pre-arrival history for CBE Castro 1/2/3 source animals so the next ET+TT revac can chain from dose 2.

## Live Readback Before Patch
- `vaccination_source_facts`: ET+TT `first` and `booster`, `Done - date unknown`, `source_date` null, `disposition=unresolved` for CBE Castro 1/2/3.
- Linked real goats via `procurement_load_goats.animal_identifier_1`: Castro 1 = `63`, Castro 2 = `75`, Castro 3 = `66`; one Castro 3 source animal is not linked.
- Accepted ET+TT completions/evidence/history before repair: 0.
- Current published V9 had Z1+Z3 rules carrying `source_dose_code=et_tt_*` and `matrix_row_id=real-seed-et_tt`.

## Intended Dates
- ET+TT dose 1: `2026-06-26` IST (maintainer-provided date for CBE C1/C2/C3).
- ET+TT dose 2 / course complete: `2026-07-20` IST (maintainer-provided date for CBE C1/C2/C3).
- Next ET+TT revac target under the active 182-day rule: `2027-01-18` IST.

## Pending
- Land the migration in git/main after any required local CI.

## Completed Locally / STG
- Added migration `000345_castro1_ettt_history_z1z3_identity_repair.sql`.
- Focused tests passed:
  - `go test ./internal/protocol/app -run 'TestPublishMatrixRejectsZ1Z3WithETTTDoseCode|TestPublish'`
  - `go test ./migrations/postgres -run Test`
- STG dry-run matched expected counts.
- STG first manual apply used the earlier Castro 1-only date assumption and must be corrected by the durable migration:
  - Earlier C1 rows used `2026-06-06` / `2026-06-27`.
  - The migration removes existing `castro1-ettt-history:*` repair rows and replaces them with `cbe-castro-ettt-history:*` rows at `2026-06-26` / `2026-07-20`.
- Expected durable migration outcome:
  - Bad Z1+Z3 rows with ET+TT source metadata: `0`.
  - Active published ET+TT rules restored: `5`.
  - ET+TT accepted pre-arrival history: `408` rows for `204` linked animals.
  - Dose history split: `204` `et_tt_kid_4w`, `204` `et_tt_kid_7w`.
  - ET+TT revac readback: `204` animals, active `et_tt_revac`, `182` day offset, next revac date `2027-01-18` IST.
  - When the CBE Castro source facts are present, migration postconditions fail loudly unless the linked goat count is `204`, accepted history count is `408`, all history rows bind to published ET+TT rules, and bad Z1+Z3 ET+TT metadata count is `0`; empty CI/query-plan databases can still apply the migration chain.
