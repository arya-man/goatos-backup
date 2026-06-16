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

### 4. Legacy Changed After Human Review

Examples:

- a reviewer already approved the Mesha sex value, then a later legacy row says
  a different sex
- a correction request was resolved, then a newer legacy export disagrees with
  that resolved Goat OS value

Required review:

- Treat the recorded Goat OS decision as canonical while the new evidence is
  reviewed.
- Open the Legacy Sync run detail and inspect the Source & Correction Log for
  source ID, source window, previous decision timestamp, old Goat OS value, and
  new legacy value.
- Confirm whether the later source row belongs to the same physical goat or is
  identifier reuse, a wrong bridge, or a source correction that should now be
  approved in Goat OS.

Do not overwrite the passport silently. If the new legacy value should win,
approve a new Goat OS correction with evidence; that decision becomes the next
canonical audited state.

## Evidence To Record

Every decision should include:

- conflict ID or source row ID
- sync run ID and source ID when the item came from Legacy Sync
- source system (`legacy_bigquery`, Sheet name, or import run)
- source window or date range checked
- identifier checked (RFID or old tag with farm/scope)
- legacy row or event date range checked
- reason another reviewer can follow

## Bulk Review

The Data Quality queue supports reviewing many conflicts at once. Filter by
review group, tick the conflicts you have already investigated, then apply one
decision to the whole selection. A reason is required and is recorded on every
conflict in the batch; each resolved conflict gets its own audit row sharing a
single bulk request id.

Decisions and where they apply:

- **Keep Mesha passport** (`keep_passport_value`): keep the Mesha passport value
  and close the selected conflicts as reviewed. No goat fields change. Valid for
  the legacy self-conflict, sex mismatch, breed mismatch, and sex+breed mismatch
  groups.
- **Use legacy value** (`use_legacy_value`): overwrite the Mesha passport sex or
  breed with the single clean legacy value, then close the conflicts. Valid only
  for the legacy sex/breed/value mismatch groups. It is rejected for legacy
  self-conflicts (which carry more than one legacy value) and for any breed that
  is not an approved canonical breed; resolve the breed catalog first in that
  case.
- **Acknowledge lifecycle flag** (`acknowledge_lifecycle_flag`): acknowledge the
  reused-tag / lifecycle flag and close the selected conflicts. No goat fields
  change. Valid only for the lifecycle / reused-tag group.

Batch rules:

- All-or-nothing: every selected conflict must still be open and at the expected
  row version. If any has changed since the page loaded, none are changed —
  reload and retry.
- Selection is per page. There is no select-all-across-pages and no spreadsheet
  import or export.
- After a bulk change, the response flags that identity counters need a rebuild.
  Counters are not rewritten inside the request; run the counter rebuild out of
  band so dashboards catch up.

## Forbidden Shortcut

Do not close BQ self-contradictions with no human explanation just because the
Mesha passport is left unchanged. Leaving the passport unchanged is safe as a
temporary system behavior; closing the review item still requires a recorded
reason.

Bulk **Keep Mesha passport** is allowed for self-conflicts only because it forces
a reason on every conflict and records an audit row — that reason is the human
explanation. Never reach for **Use legacy value** to make a self-conflict go
away: the system blocks it precisely because a self-contradiction has no single
clean legacy value to trust.
