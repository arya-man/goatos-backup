# Data Quality Legacy Review Guide

This guide is for reviewers who know the legacy Sheets and BigQuery data but
are new to Goat OS/Mesha passports.

## Principle

Legacy BQ and Sheets are source evidence. They are not an automatic overwrite.
When legacy rows disagree with the Mesha passport, keep the passport unchanged
until a human reviewer records a decision with evidence.

If BQ/Sheets contradict themselves, do not auto-close the conflict. A
self-contradiction often means one of these:

- reused old tag
- wrong RFID bridge
- duplicate goat identity
- legacy row attached to the wrong animal
- manual edit or old Sheet row that needs context

## What To Look Up

Use the identifier and date shown in Data Quality. Search both legacy BQ and
the source Sheets around that identifier.

BQ event export fields used by reconciliation:

- `goat_id`
- `farm_goat_id`
- `farm`
- `event`
- `date`
- `gender`
- `breed`
- `current_shed`
- `src_shed`
- `dst_shed`

Mesha workbench fields to compare:

- display ID
- RFID
- old tag
- scope/farm
- breed
- sex
- lifecycle
- location
- legacy reason
- legacy value
- source/evidence ID

## Review Groups

### 1. Legacy Self-Conflict

Examples:

- BQ says `female|male` for the same matched identifier.
- BQ says `Beetal|Mixed` for the same matched identifier.

Required review:

- Find all legacy rows for the shown RFID or old tag.
- Check whether the rows are one goat with a bad entry, two goats mixed
  together, or a reused/manual old tag.
- Check farm/shed/date order and whether the identifier moved between animals.

Do not auto-resolve these. If source rows are mixed, mark the identifier
disputed or request field check. Reject only when a reviewer can prove the
conflict is a harmless false positive and records the evidence.

### 2. Legacy vs Mesha Attribute Mismatch

Examples:

- Mesha says `female`; clean BQ says `male`.
- Mesha says one breed; clean BQ says another breed.

Required review:

- Verify the latest clean legacy row belongs to the same physical goat.
- Compare RFID, old tag, farm, shed, date, gender, and breed together.
- Do not trust a single field by itself.

If legacy is right and the passport field must change, create or approve a
correction request with the source row evidence. If the identifier bridge is
wrong, mark the identifier disputed or request field check.

### 3. RFID vs Old-Tag Lifecycle Mismatch

Examples:

- RFID history says `dead`; old-tag history later says `alive`.
- RFID says `sold`; old tag has later non-purchase activity.

Required review:

- Treat RFID as the stronger identity signal, but still inspect old-tag history.
- Look for old tag reuse, manual old-tag edits, or rows attached to the wrong
  animal.
- Check sale/death dates and any later shifting/feed/health activity.

Keep the conflict open until the old-tag side is explained. If the old tag was
reused, dispute or retire the identifier. If the RFID lifecycle is wrong,
request field check or create a correction with evidence.

## Evidence To Record

Every decision should include:

- conflict ID or source row ID
- source system (`legacy_bigquery`, Sheet name, or import run)
- identifier checked (RFID or old tag with farm/scope)
- date range checked
- reason another reviewer can follow

## Forbidden Shortcut

Do not bulk-resolve BQ self-contradictions just because the Mesha passport is
left unchanged. Leaving the passport unchanged is safe as a temporary system
behavior; closing the review item requires a human explanation.
