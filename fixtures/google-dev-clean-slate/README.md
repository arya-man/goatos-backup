# Google Dev Clean-Slate Vaccination Seed Pack

Purpose: provide a small, synthetic seed pack for `goatos-dev` validation after
the pre-Google correctness gates are closed. This is not a legacy migration and
must not be treated as production PHC data.

Run the fixture verifier before using the pack:

```bash
make verify-google-dev-seed-fixtures
```

## Use Order

1. Apply schema migrations with the dedicated `goatos-dev-migrate` job.
2. Seed only the five approved dashboard email grants with
   `make seed-dev-email-grants`.
3. Seed the source-derived ET/K1 protocol baseline with
   `backend/cmd/seed-vaccination-trigger` using the explicit dev Cloud SQL
   guard from `docs/runbooks/google-dev-clean-slate-seed-strategy.md`.
4. In the admin web Counts / Herd Register screen, import `sheds.csv` through
   `Import sheds`.
5. Apply `post-import-shed-profiles.sql` with `psql` to bind the imported
   sample sheds to `animal_stage_lookup` and special operational flags.
6. Apply `post-import-inventory-lots.sql` with `psql` to create active,
   expiring, expired, quarantined, and low-stock vaccine lots.
7. Import `goats.csv` through `Import sheet`; do not insert these goats with
   ad hoc SQL. This keeps `goat.created` events and idempotency behavior real.
   The main file is the accepted golden herd: 50 complete, trusted sample goats
   with DOB, shed, stage, sex, and origin populated.
8. Preview `invalid-goats.csv` through `Import sheet` and confirm every row is
   blocked with a clear row-level reason. Do not commit this file.
9. Run the worker/API validation sequence from the runbook and record actual
   generated UUIDs in the deployment evidence ledger.

Example guarded SQL helper invocation:

```bash
psql "$DATABASE_URL" \
  -v tenant_id="$GOATOS_TENANT_ID" \
  -f fixtures/google-dev-clean-slate/post-import-shed-profiles.sql
```

## Scenario Intent

The CSV rows are intentionally scenario labels, not real farm data. The stable
selectors are RFID and shed code; generated UUIDs are captured during Goal 2 as
evidence after UI/API import.

Required scenario coverage and invalid-import probes are listed in
`seed-ledger.json` and enforced by
`tools/dev/verify-google-dev-seed-fixtures.py`.

## Future Migration

Future real migration remains a separate import/backfill job from approved
Sheets or BigQuery snapshots with dry-run, checksums, row ledger, and rollback
notes. Do not grow this fixture pack into a runtime Sheets dependency.
