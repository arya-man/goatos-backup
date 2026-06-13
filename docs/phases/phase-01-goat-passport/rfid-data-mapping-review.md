# RFID Data Mapping Review

Status: reviewed from local post-apply anomaly reports; approved plain
`F2`/`K2` status mappings are implemented by migration `000010`, approved
Sirohi plus goat-cross breed mappings are implemented by migration `000011`,
and guarded RFID-only blank-suffix apply is implemented by migration `000012`
plus `rfid-apply --allow-rfid-only-blank-suffix`.

This report summarizes the first real Shape-2 RFID data rehearsal after
staging, apply, and final anomaly-report generation, then records local reruns
after the approved status, breed/species, and blank-suffix policy migrations.

Safety scope:

- Local-only rehearsal; no GCP, Cloud SQL, Google Sheets API, or frontend direct
  source access.
- Generated CSV reports remain ignored local artifacts and are not committed.
- No raw RFID values, raw old-tag values, row-level source data, workbook paths,
  media URLs, screenshots, or individual names are included here.
- Source labels below are grouped labels only and are used for mapping review.

## Initial Aggregate Outcome

| Metric | Count |
| --- | ---: |
| Total source rows | 1,223 |
| Canonical goats created | 145 |
| Rows still needing review | 1,078 |
| Rows in error | 0 |
| Updated goats | 0 |
| Conflicts created | 0 |

Final review reasons:

| Reason code | Count | Stage |
| --- | ---: | --- |
| `unknown_status_mapping` | 437 | apply |
| `species_or_breed_requires_review` | 198 | apply |
| `blank_old_tag_suffix` | 435 | staging |
| `blank_gender` | 4 | staging |
| `duplicate_old_tag_same_scope` | 4 | staging |

## Rerun After Plain F2/K2 Mapping

Migration `000010_phase_1_rfid_plain_status_mappings.sql` added only the two
approved plain `F2`/`K2` rows. A fresh local Shape-2 rehearsal was run after the
migration.

State counts:

| State | Before | After | Delta |
| --- | ---: | ---: | ---: |
| `created_goat` | 145 | 383 | +238 |
| `needs_review` | 1,078 | 840 | -238 |
| `error` | 0 | 0 | 0 |

Reason-code delta:

| Reason code | Before | After | Delta |
| --- | ---: | ---: | ---: |
| `unknown_status_mapping` | 437 | 0 | -437 |
| `species_or_breed_requires_review` | 198 | 397 | +199 |
| `blank_old_tag_suffix` | 435 | 435 | 0 |
| `blank_gender` | 4 | 4 | 0 |
| `duplicate_old_tag_same_scope` | 4 | 4 | 0 |

Result:

- `unknown_status_mapping` cleared completely.
- `created_goat` rose by 238 rows.
- 199 rows redistributed to the downstream breed/species review bucket, which
  is expected because apply checks status before breed/species.
- `error` stayed 0.

## Status Mapping Review

Grouped source Tag/status labels:

| Source Tag/status label | Count |
| --- | ---: |
| `F2` | 237 |
| `K2` | 200 |

The current status reference data already contains display status codes `F2`
and `K2`. The approved `legacy_status_mappings` rows now implemented by
migration `000010` are:

| source_system | raw_label | normalized_raw_label | lifecycle_status | reproductive_status | growth_cohort_tag | management_stage | health_status | sex_override | display_status_code | review_required | confidence | notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `legacy_rfid_db` | `F2` | `f2` | `alive` | NULL | `F2` | NULL | NULL | NULL | `F2` | false | high | Plain F2 maps to the F2 growth cohort; source Gender remains sex truth. |
| `legacy_rfid_db` | `K2` | `k2` | `alive` | NULL | `K2` | NULL | NULL | NULL | `K2` | false | high | Plain K2 maps to the K2 growth cohort; source Gender remains sex truth. |

Sex override decision:

- `sex_override` stays NULL for both labels.
- Neither plain `F2` nor plain `K2` clearly carries sex.
- These mappings do not reduce the `blank_gender` bucket.

## Species And Breed Review

Migration `000011_phase_1_rfid_breed_cross_mappings.sql` implements the
approved goat breed and goat-cross mappings. It does not change apply logic:
rows recover only through the normal breed alias resolution path.

Phase 1 crossbreed simplification:

- The current schema stores one `breed_id` per goat.
- Approved goat crosses are represented as canonical breed rows for Phase 1.
- Richer parentage or compound-breed modeling is deferred.
- Crossbreed labels are not collapsed to one parent breed.
- Reverse-order source labels point to the same canonical unordered crossbreed
  row where both source orderings appear.

Implemented breed/crossbreed source labels from the post-F2/K2 grouped report:

| Source Breed label | Count before 000011 | Classification status | Canonical Phase 1 target |
| --- | ---: | --- | --- |
| `Sirohi` | 19 | implemented | `Sirohi` active goat breed |
| `Beetal x Malai` | 4 | implemented | `Beetal x Malai` active goat crossbreed |
| `Malai x Beetal` | 4 | implemented | `Beetal x Malai` active goat crossbreed |
| `Beetal x Sojat` | 12 | implemented | `Beetal x Sojat` active goat crossbreed |
| `Boer x Beetal` | 1 | implemented | `Boer x Beetal` active goat crossbreed |
| `Boer x Malai` | 1 | implemented | `Boer x Malai` active goat crossbreed |
| `Boer x Sirohi` | 1 | implemented | `Boer x Sirohi` active goat crossbreed |
| `Boer x Sojat` | 2 | implemented | `Boer x Sojat` active goat crossbreed |
| `Malai x Sojat` | 8 | implemented | `Malai x Sojat` active goat crossbreed |
| `Sojat x Malai` | 1 | implemented | `Malai x Sojat` active goat crossbreed |

`000011` recovered 53 rows from the breed/species review bucket.

Rerun after approved breed/crossbreed mapping:

| State | After F2/K2 | After 000011 | Delta |
| --- | ---: | ---: | ---: |
| `created_goat` | 383 | 436 | +53 |
| `needs_review` | 840 | 787 | -53 |
| `error` | 0 | 0 | 0 |

Reason-code delta:

| Reason code | After F2/K2 | After 000011 | Delta |
| --- | ---: | ---: | ---: |
| `unknown_status_mapping` | 0 | 0 | 0 |
| `species_or_breed_requires_review` | 397 | 344 | -53 |
| `blank_old_tag_suffix` | 435 | 435 | 0 |
| `blank_gender` | 4 | 4 | 0 |
| `duplicate_old_tag_same_scope` | 4 | 4 | 0 |

The historical post-000011 `species_or_breed_requires_review` group was:

| Source Breed label | Count after 000011 | Classification status | Recommendation |
| --- | ---: | --- | --- |
| `Anantapur Sheep` | 344 | source `Breed` value | Business-confirmed Mesha goat breed/category; migration 000015 maps it active and it should not block passport creation. |

Source-category signal:

- `Anantapur Sheep` accounted for all 344 remaining breed/species review rows
  before migration 000015.
- The source sheet uses `Breed` as an operational breed/category field; the
  label is Mesha business data, not a reason to reject or hide the row.
- The correct apply behavior is to resolve existing approved aliases or create
  review-status catalog aliases for unknown nonblank source Breed labels while
  still allowing passport creation. Blank/missing Breed remains reviewable.

Implementation note for the later build slice:

- The real model is `breeds` plus `breed_aliases`.
- `breed_aliases.normalized_alias` is the lookup key.
- The current canonical goat row stores one `breed_id`, so a crossbreed label
  must not be aliased to only one parent breed.
- Any source-category decision must resolve through explicit breed/category
  semantics, not a hidden freeform list or a guessed goat alias.

## Blank Old-Tag Suffix

`blank_old_tag_suffix` accounts for 435 rows. These rows were blocked during
staging, so they have not yet passed apply-time status, breed/species, RFID
conflict, or idempotency gates.

CSV evidence from the sensitive local reviewer pack:

| Check | Count | Decision note |
| --- | ---: | --- |
| Rows in bucket | 435 | Maximum possible additional goats is 435. |
| Present RFID | 435 | Every row has an RFID value. |
| Unique RFID within this bucket | 435 | True global uniqueness is still apply-time only. |
| Known Gender value | 435 | No blank/unknown Gender inside this bucket. |
| Current mapped status label | 52 | The other 383 rows would still need status mapping or review. |
| Current active goat breed/species mapping | 270 | Historical pre-000015 count; source breed/category labels now resolve during apply, with unknown labels held in catalog review status. |
| Current status + goat breed + gender gates pass | 27 | Realistic immediate yield before DB conflict checks. |
| Likely still review under option B | 408 | Historical pre-000015 estimate; source breed/category review no longer applies to nonblank Breed labels. |

Exact yield can only be confirmed by the later implementation dry-run because
RFID conflicts against already-created goats are checked only when rows traverse
apply.

Farm-level grouped counts:

| Farm context | Count |
| --- | ---: |
| `CBE` | 237 |
| `CPT` | 198 |

The ignored local grouped report contains 60 Farm/Shed/Partition context
groups. The largest context buckets have counts 94, 77, 54, 33, and 33. The
committed report intentionally avoids listing shed or partition labels that
could be operationally sensitive or person-like labels.

Suffix derivation review:

- Farm has two safe context groups, but Farm is not itself proof of the missing
  old-tag scope/park suffix.
- Partition is not deterministic: 9 of 12 partition values appear under more
  than one Farm context.
- Shed is not deterministic: 5 of 16 shed values appear under more than one Farm
  context.
- Shed plus Partition is still not deterministic: 7 of 53 combinations appear
  under more than one Farm context.
- Farm/Shed/Partition together is deterministic only because Farm is included,
  which would make the derivation circular rather than evidence that Partition
  proves the suffix.
- Existing old-tag values in this bucket are low-actionability without suffix:
  263 distinct old-tag values across 435 rows, with repeated old-tag groups that
  would still collide if a guessed Farm suffix were used.

C-lite is rejected for now. A wrong derived suffix would create an active
old-tag identity in the wrong scope, which is worse than preserving unresolved
old-tag evidence and creating only the globally scoped RFID identifier.

Policy options:

| Option | Description | Tradeoff |
| --- | --- | --- |
| A | Keep blocking as `needs_review`. | Safest for old-tag history, but it keeps 435 otherwise usable RFID-backed rows out of canonical Goat Passport. |
| B | Allow RFID-only goat creation and leave old_tag unresolved. | Fastest path to a canonical RFID foundation. Old-tag lookup/history remains incomplete until source cleanup or later correction. |
| C | Require source cleanup before import. | Highest old-tag fidelity, but slow/manual and blocks 36% of the workbook. |
| C-lite | Derive suffix from Farm/Shed/Partition only when provably one-to-one. | Rejected for this workbook because Partition and Shed are not one-to-one with suffix context. |

Implemented policy: option B is available behind the explicit
`rfid-apply --allow-rfid-only-blank-suffix` flag, with guardrails.

Pre-implementation estimate under option B:

- Maximum possible additional goats: 435.
- Estimated immediate additional goats from CSV evidence: 27.
- Estimated remaining review rows even under B: 408.
- The implementation dry-run superseded this estimate with exact DB-gate
  results.

Implementation dry-run and real apply with migration `000012`:

| Result | Count |
| --- | ---: |
| Total staged rows | 1223 |
| `created_goat` | 711 |
| `needs_review` | 512 |
| `error` | 0 |
| Additional goats over post-`000011` baseline | 275 |

The `--allow-rfid-only-blank-suffix` dry-run and real apply both scanned 1215
candidate rows, applied 711, routed 504 to review, and had 0 errors. The
remaining 8 rows were the original blank-gender and duplicate-old-tag staging
reviews; they were not apply candidates and remained blocked.

Historical post-RFID-only, pre-000015 open-review reason occurrences:

| Reason code | Count |
| --- | ---: |
| `species_or_breed_requires_review` | 504 |
| `blank_old_tag_suffix` | 160 |
| `blank_gender` | 4 |
| `duplicate_old_tag_same_scope` | 4 |

Reason-code counts are occurrences, not distinct row counts. The 160 remaining
`blank_old_tag_suffix` rows also failed the old breed/species review gate, so
they appeared in both buckets. Migration 000015 makes those nonblank Breed labels
eligible for passport creation instead; only blank/missing Breed should emit
`species_or_breed_requires_review`.

Why B is a policy problem, not just source cleanup:

- All 435 rows have present RFID and are unique within the bucket.
- Old-tag suffix is the only staging-time blocker for this bucket.
- The existing evidence model already preserves the unresolved old-tag source
  data in `legacy_import_rows.raw_payload`, `normalized_payload`, the import
  run/source record trail, and the decision/audit evidence path.
- Old-tag identifiers are optional in the model; RFID can be the primary
  canonical identifier when old-tag scope is unresolved.
- Requiring source cleanup first remains safest for old-tag fidelity, but it
  delays the canonical RFID foundation for rows that otherwise have usable RFID
  identity.

- Only apply when RFID is valid and globally unique.
- Only apply when status, breed/species, and gender requirements pass.
- Only bypass `blank_old_tag_suffix` when it is the sole staging review reason;
  keep rows with any other staging reason blocked.
- Create the goat and primary RFID identifier.
- Do not create an `old_tag` identifier for the blank-suffix row.
- Preserve unresolved old-tag evidence/reason on the import row and decision
  audit path.
- Keep duplicate, malformed, or ambiguous old-tag rows blocked.
- Do not derive an old-tag scope from Farm, Shed, or Partition in this slice.

Implemented tests:

- A row with only `blank_old_tag_suffix`, valid unique RFID, mapped status,
  active goat breed, and known Gender creates a goat plus primary RFID.
- That row creates no `old_tag` identifier and does not run old-tag scoped
  uniqueness as a creation requirement.
- Rows with unmapped status, blank/missing Breed, blank/unknown Gender, RFID
  conflict, same-scope old-tag conflict, or any additional staging review reason
  remain in `needs_review`. Nonblank source Breed labels such as Anantapur Sheep
  are not blockers after migration 000015; unknown labels stay review-status in
  the catalog until operator review.
- Replay/idempotency does not create duplicate goats or duplicate RFID
  identifiers.
- The decision record, import row, and audit/evidence trail preserve unresolved
  old-tag source evidence without emitting raw row data in committed docs.
- Dry-run reports the exact created/review redistribution before mutation.
- Created RFID-only rows are removed from open anomaly/reviewer reports.

Implementation notes:

- Default `rfid-apply` remains unchanged and only scans `pending` rows.
- The opt-in flag uses a separate widened static sqlc query and a partial
  `(import_run_id, row_number, legacy_row_id)` index over pending rows plus
  coarse blank-suffix candidates.
- The Go transaction re-locks the row and applies only if the complete reason
  set is exactly `blank_old_tag_suffix`; rows with any additional staged reason
  remain in review.
- Created RFID-only rows create no `old_tag` identifier and do not derive scope
  from Farm/Shed/Partition.
- At staging/production scale, build the supporting large-table index
  concurrently/out-of-band rather than inside a single long transaction.

Risks and rollback:

- Risk: creating RFID-only goats may temporarily reduce old-tag search coverage.
  Mitigation: no old-tag identifier is created, and unresolved evidence remains
  attached to the import row/decision trail for later correction.
- Risk: some rows may still fail downstream gates after unblocking. Mitigation:
  implementation dry-run runs first and treats redistribution to existing
  review buckets as expected, not as an error.
- Rollback before apply is a normal code revert. Rollback after apply requires
  correction/merge/retire workflows for created goats; do not use destructive
  deletes.

The post-implementation local rehearsal records the exact result: 275
additional RFID-only goats over the post-`000011` baseline, with no errors.

## Blank Gender

`blank_gender` accounts for 4 rows.

Recommendation:

- Keep blocking until source cleanup or a written sex-inference policy exists.
- Do not infer sex from status labels unless a label clearly carries sex and the
  mapping is explicitly reviewed.
- The proposed plain `F2` and `K2` status mappings do not set `sex_override`, so
  they do not resolve these rows.

## Duplicate Old Tag In Same Scope

`duplicate_old_tag_same_scope` accounts for 4 rows in two hashed old-tag/scope
groups:

| Safe old-tag/scope ref | Scope | Count |
| --- | --- | ---: |
| `sha256:bc65c9aefef47794` | `park:CPT` | 2 |
| `sha256:c88b30c7178b63a3` | `park:CPT` | 2 |

Recommendation:

- Keep blocking.
- Resolve by source review/correction or by an explicit conflict/merge workflow.
- Do not auto-resolve duplicate same-scope old tags during import.

## Next Data Slices

1. Keep migration 000015 and the source Breed policy proof in every fresh local
   rehearsal: `Anantapur Sheep` and other nonblank source Breed labels must
   create goat passports when all other gates pass. Unknown labels stay
   review-status in the catalog. The old 504 breed/species review occurrences
   are historical.
2. Keep blank gender and duplicate same-scope old tags blocked pending source
   correction or an explicit reviewed policy.
