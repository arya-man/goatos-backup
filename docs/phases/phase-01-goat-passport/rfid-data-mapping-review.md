# RFID Data Mapping Review

Status: reviewed from the local post-apply anomaly report.

This report summarizes the first real Shape-2 RFID data rehearsal after
staging, apply, and final anomaly-report generation. It is analysis only: no
canonical import/apply behavior, reference tables, or policy rows changed in
this slice.

Safety scope:

- Local-only rehearsal; no GCP, Cloud SQL, Google Sheets API, or frontend direct
  source access.
- Generated CSV reports remain ignored local artifacts and are not committed.
- No raw RFID values, raw old-tag values, row-level source data, workbook paths,
  media URLs, screenshots, or individual names are included here.
- Source labels below are grouped labels only and are used for mapping review.

## Aggregate Outcome

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

Projected yield after only the approved plain `F2`/`K2` status mappings:

| Bucket | Count |
| --- | ---: |
| Current canonical goats created | 145 |
| Additional rows expected to clear status mapping | 437 |
| Projected canonical goats after F2/K2 mapping | 582 |
| Non-goat rows expected to stay blocked | 190 |
| Remaining Sirohi breed review rows | 8 |
| Remaining blank old-tag suffix policy rows | 435 |
| Remaining blank gender rows | 4 |
| Remaining duplicate same-scope old-tag rows | 4 |

## Status Mapping Review

Grouped source Tag/status labels:

| Source Tag/status label | Count |
| --- | ---: |
| `F2` | 237 |
| `K2` | 200 |

The current status reference data already contains display status codes `F2`
and `K2`. Proposed `legacy_status_mappings` rows:

| source_system | raw_label | normalized_raw_label | lifecycle_status | reproductive_status | growth_cohort_tag | management_stage | health_status | sex_override | display_status_code | review_required | confidence | notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `legacy_rfid_db` | `F2` | `f2` | `alive` | NULL | `F2` | NULL | NULL | NULL | `F2` | false | high | Plain F2 maps to the F2 growth cohort; source Gender remains sex truth. |
| `legacy_rfid_db` | `K2` | `k2` | `alive` | NULL | `K2` | NULL | NULL | NULL | `K2` | false | high | Plain K2 maps to the K2 growth cohort; source Gender remains sex truth. |

Sex override decision:

- `sex_override` stays NULL for both labels.
- Neither plain `F2` nor plain `K2` clearly carries sex.
- These mappings do not reduce the `blank_gender` bucket.

## Species And Breed Review

Grouped source Breed labels:

| Source Breed label | Count | Proposed classification | Recommendation |
| --- | ---: | --- | --- |
| `Anantapur Sheep` | 190 | non-goat/exclusion | Keep out of goat creation. The current reference model treats this label as sheep, so it must resolve through breed/species semantics before any apply behavior changes. |
| `Sirohi` | 8 | goat breed alias candidate | Human review should approve whether this becomes a canonical goat breed row or an alias to an existing canonical breed. Do not auto-alias in this slice. |

Source-integrity signal:

- `Anantapur Sheep` accounts for 190 of the 198 breed review rows.
- A goat-passport import source containing this many non-goat rows is not just a
  breed-alias gap; it is a source-integrity issue.
- The current species gate is doing the right thing by keeping these rows out of
  goat creation.
- Later implementation should decide whether non-goat rows are classified during
  source discovery/staging or left to the apply-stage breed/species gate. Either
  way, the pipeline must not be loosened to admit them as goats.

Implementation note for the later build slice:

- The real model is `breeds` plus `breed_aliases`.
- `breed_aliases.normalized_alias` is the lookup key.
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

1. Add approved `legacy_status_mappings` rows for plain `F2` and `K2`.
2. Add approved breed/species decisions:
   - keep `Anantapur Sheep` out of goat creation through breed/species
     semantics and decide whether that classification belongs at discovery,
     staging, or apply;
   - review and add `Sirohi` as a goat breed/alias only after human approval.
3. Implement the blank old-tag suffix RFID-only creation policy if approved.
4. Keep blank gender and duplicate same-scope old tags blocked pending source
   correction or an explicit reviewed policy.
