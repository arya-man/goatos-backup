# Vaccination Anchor Events Runbook

Vaccination anchors are explicit base dates for missing or unknown prior vaccine history. Use them when operations knows a vaccine campaign happened for a cohort, but individual administration rows were never captured.

## Rules

- Create anchors from Preventive Care -> Vaccination plan -> Anchor campaign.
- Do not insert `vaccination_anchor_events` manually except break-glass SQL after maintainer approval.
- Anchors override earlier DOB catch-up rows for the same vaccine cohort.
- Future boosters and revaccinations chain from the anchor through `protocol_rule_lineage`, so a later protocol publish must not break the chain.
- Pre-anchor open obligations are canceled only when they are strictly before the anchor date.
- Same-day obligations are preserved.
- Canceled rows include `scheduled`, `due`, `in_progress`, and `deferred`, and detached drive memberships are pruned with status events and outbox rows.
- Drive creation still respects the operator cap of 200 animals and should keep full sheds or parent partitions together where possible.

## Safe Creation Flow

The admin flow validates before insert:

- `vaccine_code` must map to an active vaccination protocol rule.
- `dose_code`, when supplied, must belong to that vaccine.
- The selected scope must resolve to at least one live animal.
- Species eligibility is checked from rule eligibility.
- When age enforcement is enabled, birth-age rules exclude animals younger than the configured offset on the anchor date.
- Repeated create requests are idempotent and do not duplicate anchors.
- Animal details shown in the UI use RFID/preferred identifiers, not internal goat ids.

## Business Timing Reference

- PPR: 16 weeks, live, goat and sheep, revaccinate every 3 years.
- FMD: 12 weeks, killed, goat and sheep, revaccinate every 9 months.
- HS: 12 weeks, killed, goat and sheep, revaccinate every 1 year.
- Blue Tongue: sheep only, killed, 16 weeks plus booster at 19 weeks, revaccinate every 1 year.
- Goat Pox: goat only, live, 16 weeks, revaccinate every 1 year.
- Sheep Pox: sheep only, live, 16 weeks, revaccinate every 1 year.
- Z1+Z3: goat and sheep, killed bacterial/toxoid, 4 weeks plus booster at 7 weeks, revaccinate every 6 months.

## 2026-09-01 OCI Validation Snapshot

This snapshot was taken from the targeted OCI validation database, not from a full staging dump.

User-set anchors in effect:

- PPR: 2026-09-08, tenant scope, suppress earlier open rows, chain future rows.
- FMD: 2026-09-08, tenant scope, suppress earlier open rows, chain future rows.
- HS: 2026-09-08, tenant scope, suppress earlier open rows, chain future rows.
- Sheep Pox: 2026-09-04, Coimbatore park scope, adult sheep campaign.
- Blue Tongue: 2026-09-22, Coimbatore park scope, adult sheep campaign.
- Z1+Z3: 2026-10-15, tenant scope, suppress earlier open rows, chain boosters/repeats.

OCI cleanup performed:

- Moved 13 active 2026-09-01 DOB-rule rows to 2026-09-04: 5 Channapatna Sheep Pox and 8 Coimbatore Goat Pox.
- Canceled 584 stale unbatched PPR/HS rows that were still open after the PPR/FMD/HS 2026-09-08 anchors.
- Canceled 31 earlier noisy unbatched PPR/Blue Tongue rows before this snapshot.

OCI invariant audit after cleanup:

- Active 2026-09-01 vaccine rows: 0.
- Duplicate same animal/vaccine/date rows: 0.
- Z1+Z3 before 2026-10-15: 0.
- PPR/FMD/HS before 2026-09-08: 0.
- Wrong-species Blue Tongue, Goat Pox, Sheep Pox rows: 0.
- Deferred September rows: 16. These are not active scan cards; inspect health/ICU state before rescheduling.

Active September schedule after cleanup:

| Date | Park | Vaccine | Dose code | Count | Age weeks | Sheds | Reason |
| --- | --- | --- | --- | ---: | --- | --- | --- |
| 2026-09-02 | Channapatna | Blue Tongue | `blue_tongue_adult_w2` | 84 | 110.4-137.9w | Gandhi | Booster from prior BT dose + 21 days |
| 2026-09-02 | Coimbatore | Goat Pox | `goat_pox_adult_w1` | 154 | 16.6-133.9w | Godel 1, Godel 2, Mandela 1, Yashoda | Adult catch-up/campaign |
| 2026-09-03 | Channapatna | Blue Tongue | `blue_tongue_adult_w2` | 145 | 16.7-138.0w | Godel 1, Mandela 2, Old Yashoda | Booster from prior BT dose + 21 days |
| 2026-09-03 | Channapatna | Goat Pox | `goat_pox_kid_16w` | 1 | 16.7w | Mandela 1 | DOB rule: Goat Pox at 16 weeks |
| 2026-09-04 | Channapatna | Goat Pox | `goat_pox_kid_16w` | 1 | 16.9w | Yashoda | DOB rule: Goat Pox at 16 weeks |
| 2026-09-04 | Channapatna | Sheep Pox | `sheep_pox_kid_16w` | 5 | 16.9w | Mandela 1, Mandela 2 | DOB rule: Sheep Pox at 16 weeks |
| 2026-09-04 | Coimbatore | Goat Pox | `goat_pox_kid_16w` | 8 | 16.9w | Godel 1, Yashoda | DOB rule: Goat Pox at 16 weeks |
| 2026-09-04 | Coimbatore | Sheep Pox | `sheep_pox_adult_w1` | 22 | 16.9-64.1w | Godel 1, Godel 2, Mandela 1, Yashoda | CBE adult Sheep Pox anchor/campaign |
| 2026-09-08 | Channapatna | FMD | `fmd_kid_12w` | 121 | 17.4w | Mandela 1, Mandela 2, Yashoda | Sep 8 FMD anchor/cohort |
| 2026-09-08 | Channapatna | HS | `hs_kid_12w` | 121 | 17.4w | Mandela 1, Mandela 2, Yashoda | Sep 8 HS anchor/cohort |
| 2026-09-08 | Channapatna | PPR | `ppr_kid_16w` | 161 | 17.4w | Mandela 1, Mandela 2, Yashoda | Sep 8 PPR anchor/cohort |
| 2026-09-08 | Coimbatore | FMD | `fmd_kid_12w` | 47 | 17.4w | Godel 1, Godel 2, Yashoda | Sep 8 FMD anchor/cohort |
| 2026-09-08 | Coimbatore | HS | `hs_kid_12w` | 47 | 17.4w | Godel 1, Godel 2, Yashoda | Sep 8 HS anchor/cohort |
| 2026-09-08 | Coimbatore | PPR | `ppr_kid_16w` | 154 | 17.4w | Godel 1, Godel 2, Yashoda | Sep 8 PPR anchor/cohort |
| 2026-09-22 | Coimbatore | Blue Tongue | `blue_tongue_adult_w1` | 169 | 19.4-23.1w | Castro, Godel 2, Yashoda | CBE adult BT anchor/campaign |
| 2026-09-26 | Channapatna | FMD | `fmd_kid_12w` | 4 | 12.0w | Yashoda | DOB rule: FMD at 12 weeks |
| 2026-09-26 | Channapatna | HS | `hs_kid_12w` | 4 | 12.0w | Yashoda | DOB rule: HS at 12 weeks |
| 2026-09-26 | Coimbatore | FMD | `fmd_kid_12w` | 10 | 12.0w | Yashoda | DOB rule: FMD at 12 weeks |
| 2026-09-26 | Coimbatore | HS | `hs_kid_12w` | 10 | 12.0w | Yashoda | DOB rule: HS at 12 weeks |
| 2026-09-28 | Channapatna | Sheep Pox | `sheep_pox_kid_16w` | 48 | 20.3w | Castro, Godel 2 | DOB rule: Sheep Pox at 16 weeks |
| 2026-09-29 | Channapatna | Goat Pox | `goat_pox_kid_16w` | 1 | 20.4w | Mandela 1 | DOB rule: Goat Pox at 16 weeks |
| 2026-09-29 | Channapatna | Sheep Pox | `sheep_pox_kid_16w` | 90 | 20.4-24.0w | Castro, Godel 2, Mandela 1 | DOB rule: Sheep Pox at 16 weeks |
| 2026-09-30 | Channapatna | Sheep Pox | `sheep_pox_kid_16w` | 20 | 24.1-24.3w | Godel 2 | DOB rule: Sheep Pox at 16 weeks |

Follow-up OCI cleanup on 2026-09-01:

- Canceled 584 stale unbatched PPR/HS obligations that were left over from pre-anchor generation. These were not attached to drive batches, but they still appeared in schedule queries.
- After this cleanup, there are no active PPR/FMD/HS obligations before 2026-09-08 and no Sep 9-16 stale PPR/HS rows.
- Final invariant check: Sep 1 active = 0, duplicates = 0, pre-Oct-15 Z1+Z3 = 0, pre-Sep-8 PPR/FMD/HS = 0, wrong-species BT/Goat Pox/Sheep Pox = 0, missed September = 0, overdue September = 0.

Remaining unbatched September rows after final cleanup:

| Date | Park | Vaccine | Dose code | Count | Age weeks | Sheds | History check |
| --- | --- | --- | --- | ---: | --- | --- | --- |
| 2026-09-26 | Channapatna | FMD | `fmd_kid_12w` | 4 | 12.0w | Yashoda | No accepted FMD history |
| 2026-09-26 | Channapatna | HS | `hs_kid_12w` | 4 | 12.0w | Yashoda | No accepted HS history |
| 2026-09-26 | Coimbatore | FMD | `fmd_kid_12w` | 10 | 12.0w | Yashoda | No accepted FMD history |
| 2026-09-26 | Coimbatore | HS | `hs_kid_12w` | 10 | 12.0w | Yashoda | No accepted HS history |
| 2026-09-28 | Channapatna | Sheep Pox | `sheep_pox_kid_16w` | 48 | 20.3w | Castro, Godel 2 | No accepted Sheep Pox history |
| 2026-09-29 | Channapatna | Goat Pox | `goat_pox_kid_16w` | 1 | 20.4w | Mandela 1 | No accepted Goat Pox history |
| 2026-09-29 | Channapatna | Sheep Pox | `sheep_pox_kid_16w` | 90 | 20.4-24.0w | Castro, Godel 2, Mandela 1 | No accepted Sheep Pox history |
| 2026-09-30 | Channapatna | Sheep Pox | `sheep_pox_kid_16w` | 20 | 24.1-24.3w | Godel 2 | No accepted Sheep Pox history |

Do not mark the operational schedule fully complete until the owner decides whether these valid future DOB rows should remain unbatched for scheduler pickup or be pre-batched into operator drive cards.
