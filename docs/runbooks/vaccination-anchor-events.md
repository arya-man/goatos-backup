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
