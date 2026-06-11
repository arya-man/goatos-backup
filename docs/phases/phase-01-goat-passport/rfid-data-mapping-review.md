# RFID Data Mapping Review

Status: reviewed from local post-apply anomaly reports; approved plain
`F2`/`K2` status mappings are implemented by migration `000010`, and approved
Sirohi plus goat-cross breed mappings are implemented by migration `000011`.

This report summarizes the first real Shape-2 RFID data rehearsal after
staging, apply, and final anomaly-report generation, then records local reruns
after the approved status and breed/species mapping migrations. No canonical
import/apply behavior changed.

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

The remaining `species_or_breed_requires_review` group is:

| Source Breed label | Count after 000011 | Classification status | Recommendation |
| --- | ---: | --- | --- |
| `Anantapur Sheep` | 344 | already-classified | Keep out of goat creation through breed/species semantics. |

Source-integrity signal:

- `Anantapur Sheep` accounts for all 344 remaining breed/species review rows.
- A goat-passport import source containing this many non-goat rows is not just a
  breed-alias gap; it is a source-integrity issue.
- The current species gate is doing the right thing by keeping these rows out of
  goat creation.
- Later implementation should decide whether non-goat rows are classified during
  source discovery/staging or left to the apply-stage breed/species gate. Either
  way, the pipeline must not be loosened to admit them as goats.
- Confirmed non-goat rows should not remain indefinitely in an actionable
  human-review queue. A later review-ops/apply-semantics slice should decide
  whether these rows become terminal `rejected` rows at apply time or are
  excluded earlier during source discovery/staging.

Implementation note for the later build slice:

- The real model is `breeds` plus `breed_aliases`.
- `breed_aliases.normalized_alias` is the lookup key.
- The current canonical goat row stores one `breed_id`, so a crossbreed label
  must not be aliased to only one parent breed.
- Exclusion must resolve through breed/species semantics, not a hidden freeform
  exclusion list or a goat alias.

## Blank Old-Tag Suffix

`blank_old_tag_suffix` accounts for 435 rows. Farm-level grouped counts:

| Farm context | Count |
| --- | ---: |
| `CBE` | 237 |
| `CPT` | 198 |

The ignored local grouped report contains 60 Farm/Shed/Partition context
groups. The largest context buckets have counts 94, 77, 54, 33, and 33. The
committed report intentionally avoids listing shed or partition labels that
could be operationally sensitive or person-like labels.

Policy options:

| Option | Description | Tradeoff |
| --- | --- | --- |
| A | Keep blocking as `needs_review`. | Safest for old-tag history, but it keeps 435 otherwise usable RFID-backed rows out of canonical Goat Passport. |
| B | Allow RFID-only goat creation and leave old_tag unresolved. | Fastest path to a canonical RFID foundation. Old-tag lookup/history remains incomplete until source cleanup or later correction. |
| C | Require source cleanup before import. | Highest old-tag fidelity, but slow/manual and blocks 36% of the workbook. |

Recommendation: choose option B for a later policy build, with guardrails:

- Only apply when RFID is valid and globally unique.
- Only apply when status, breed/species, and gender requirements pass.
- Create the goat and primary RFID identifier.
- Do not create an `old_tag` identifier for the blank-suffix row.
- Preserve unresolved old-tag evidence/reason on the import row and decision
  audit path.
- Keep duplicate, malformed, or ambiguous old-tag rows blocked.

No apply behavior changes in this slice.

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

1. Decide whether `Anantapur Sheep` should be classified during source
   discovery/staging or left to the apply-stage breed/species gate; either way,
   keep it out of goat creation.
2. Implement the blank old-tag suffix RFID-only creation policy if approved.
3. Keep blank gender and duplicate same-scope old tags blocked pending source
   correction or an explicit reviewed policy.
