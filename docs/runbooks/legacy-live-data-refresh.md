# Legacy Live Data Refresh

This runbook is for keeping the old Google Sheets/BigQuery evidence usable
while Goat OS admin-web is being rebuilt cleanly.

The old dashboard/runtime UI and old import review screens stay removed from
the active product. Do not bring those routes, components, or backend runtime
surfaces back just to inspect data. Use this only as an ops-only replay/export
bridge for dev data refreshes and reviewer CSVs.

## Sources

Legacy source project:

```text
BigQuery project: goatos-sheets
BigQuery location: US
```

Live source sheets used by the replay tooling:

```text
RFID source of truth: 1FulMrlb8_AGwL5nFwoORACnbDstSFMg-GCPaKIZnnF8
Census DB:            1tye3uknlVMoPIiYk2pdkwy9PLQwo5m8wdFIsc7yI5Ho
Goats DB:             1R648AutCSXS247DZb7dc07dDgyue6_R4mZDd93oW3M8
```

The legacy dashboard headline count comes from:

```text
goatos-sheets.farm.daily_summary_dev
```

## Authentication

Use the Goat OS Google context from
`docs/runbooks/google-cloud-environments.md`.

Drive exports require a Drive-scoped gcloud token. If Sheets/Drive API calls
fail with `ACCESS_TOKEN_SCOPE_INSUFFICIENT`, re-auth with:

```bash
gcloud auth login --enable-gdrive-access --no-launch-browser --brief
gcloud config set account ravi@mesha.sg
gcloud config set project goatos-dev
```

Verify before reads or writes:

```bash
gcloud auth list --format="table(account,status)"
gcloud config list --format="text(core.account,core.project)"
gcloud organizations list --format="table(displayName,name,directoryCustomerId)"
gcloud projects describe goatos-dev --format="json(projectId,name,parent)"
```

Expected:

```text
Account: ravi@mesha.sg
Project: goatos-dev
Org:     vgoats.com / 563962826703
Folder:  goat-os / 188649904255
```

## Historical Replay Bridge

The legacy replay/import command code was intentionally pruned in:

```text
e90f0a2 chore: prune legacy Goat OS runtime surfaces
```

Use the previous known-good replay commit only in a throwaway worktree:

```bash
rm -rf /tmp/goatos-legacy-sync-3e6548f
git -C /path/to/goatos worktree add \
  --detach /tmp/goatos-legacy-sync-3e6548f 3e6548f
```

Run the clean replay locally first:

```bash
cd /tmp/goatos-legacy-sync-3e6548f
GOATOS_REPLAY_ORACLE_CONTAINER=__skip_oracle__ ./tools/replay/live-replay.sh
```

This replay pulls live Drive sheets and live BigQuery, applies the old RFID/BQ
identity pipeline into a throwaway Postgres container, checks the legacy
dashboard active count, and verifies same-live idempotency.

Do not copy the old command code back into `main`.

## Dev Sync Rule

Do not blindly run the historical commands against `goatos-dev`.

First compare the current dev DB against a clean live replay. Incremental sync
is unsafe when dev already contains a stale or partial backfill state: a
candidate-only apply can overshoot the legacy dashboard count even though a
clean replay matches it.

Required checks before any dev write:

```text
1. Live BQ latest dashboard count from daily_summary_dev.
2. Current goatos-dev counts from Postgres goats.
3. Clean local live replay final counts.
4. Dev-vs-clean-replay delta:
   - rows present in clean replay but missing in dev
   - rows present in dev but missing in clean replay
   - common rows whose status/location/breed/sex/identity_state changed
5. Dry-run old-tag backfill against dev.
```

Only apply to `goatos-dev` when the operator explicitly approves the write.

If a candidate-only dry-run would overshoot, do not run only the candidate
backfill and call it synced. Use the full corrective path:

```text
1. Export a pre-sync backup of goats, goat_identifiers, identity_conflicts,
   identity_conflict_goats, and identity_conflict_source_records.
2. Apply live BQ event reconciliation first.
3. Apply deterministic live Sheets/BQ old-tag backfill for rows present in the
   clean replay but missing in dev.
4. Mark rows present in dev but missing in the clean replay as inactive and
   retire their active identifiers. Do not hard-delete them.
5. Apply targeted common-row lifecycle/location/state corrections that were not
   covered by the historical command.
6. Export a post-sync active identity snapshot, excluding inactive/merged rows.
7. Compare the post-sync active snapshot against the clean live replay with
   `tools/replay/compare-replay-to-oracle.py`.
8. Regenerate the reviewer CSV from Cloud SQL after parity passes.
```

The sync is acceptable only when the post-sync active snapshot has:

```text
missing_in_replay = 0
extra_in_replay = 0
changed_common_keys = 0
```

Do not target a same-day `daily_summary_dev` row when it is partial. For
example, a row with `CBE=0`, `CPT=0`, and only procurement populated is not a
valid GoatOS active-count target. Use the latest complete row with CBE and CPT
counts populated.

## Reviewer Fix Sheet

This legacy live-data refresh runbook is not the clean-slate V1 herd-identity
path. V1 does not accept reused tags, unknown sex, or park-scoped identifier
reuse. Any source conflict must be fixed before accepted herd animals are
seeded.

For reviewer handoff, export a CSV from `identity_conflicts` with both the
legacy evidence and fillable reviewer columns.

Required reviewer columns:

```text
reviewer_decision
correct_status
correct_breed
correct_sex
correct_rfid
correct_old_tag
correct_park
correct_shed
identifier_already_owned_yes_no
source_reference
evidence_note
reviewed_by
reviewed_at
```

Allowed `reviewer_decision` values:

```text
KEEP_GOATOS
UPDATE_GOATOS_SOURCE_FIXTURE_ONLY
IDENTIFIER_ALREADY_OWNED_REJECT_ROW
MARK_SOLD
MARK_DEAD
MERGE_DUPLICATE
CANNOT_DECIDE
```

Instructions for the reviewer:

- Do not delete rows or rename columns.
- Fill only the reviewer columns.
- For identifier conflicts, confirm whether the RFID or source tag belongs to
  the same animal. If the value belongs to any other current or historical
  animal, reject/fix the row before GoatOS seed; never reuse the value.
- For breed/sex mismatches, provide the corrected value or choose `KEEP_GOATOS`.
- For legacy self-conflicts, choose the canonical value from source evidence or
  use `CANNOT_DECIDE`.
- Add `source_reference` whenever possible: sheet name, row number, photo/proof
  link, or operator note.
- Send the completed CSV back for validation. Engineering should apply accepted
  fixes only through a controlled dev refresh/import path; reviewers should not
  update SQL directly.
