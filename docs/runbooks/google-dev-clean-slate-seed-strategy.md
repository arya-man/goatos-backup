# Google Dev Clean-Slate Seed Strategy

This runbook is the handoff between the pre-Google correctness goal and the
`goatos-dev` deployment goal. Do not mutate Google Cloud from this runbook until
the pre-Google code gates below are green and the active GitHub/GCP boundaries
have been verified as Mesha/VGoats.

## Clear Call

`goatos-dev` starts as a clean-slate environment. It is not a legacy data
migration.

Preserve only the four approved user grants. Seed a small, representative PHC
vaccination slice that proves the operational kernel end to end:

```text
goat/shed source -> goat.created / shed config
  -> protocol generation
  -> obligation sweeper
  -> SOP task
  -> proof submission
  -> verification / rework
  -> stock reserve and consume
  -> Calendar / Action Center / Protocol Adherence read models
```

## Pre-Google Code Gates

Before deployment work starts, close or explicitly bound these items:

- SOP proof and verification must fail closed unless a valid submission,
  required proof, and completion fanout exist.
- Protocol publish must either materialize executable `protocol_rules` from
  `rule_dsl.schedule[]` or reject the version.
- Generation must honor lifecycle, animal stage, sex, breed, health,
  reproductive state, age band, and min/max age against the drive business date.
- Unsupported repeat policies must be rejected until recurrence is materialized.
- Sweepers must group by scope, protocol version, rule, due date, and due window.
- Sweeper execution binding must resolve SOP and stock from the published
  protocol/rule contract, with a visible block when binding is missing.
- FEFO reserve must validate lot expiry against the planned drive/admin date.
- Missed work must remain visible in command surfaces even outside default date
  projection windows.
- Exited goats must not retain active or missed rows that inflate adherence or
  escalation.
- Consumer replay must be replay-safe after handler success and must not silently
  lose finalization ownership.
- Batch stock reconciliation must continue past a bad batch and record the
  failed batch for retry/repair.

## Boundary Checks

Before any GitHub push or Google Cloud mutation, state and verify:

- repo remote: `vgoats/goatos`
- branch: `main`
- GitHub authority: Mesha/VGoats token path, not the active `gh` account
- Google account: `ravi@mesha.sg`
- Google organization: `vgoats.com`
- Google project: `goatos-dev`
- legacy project left untouched: `goatos-sheets`

If any active account, organization, folder, repo, or project points outside the
Mesha/VGoats boundary, stop and fix context before continuing.

## Archive Old Dashboard First

Before enabling the new dev environment, snapshot the old dashboard state:

- current URLs and DNS/custom-domain state
- deployment config and service names
- screenshots of key pages
- relevant logs and active alerts
- Cloud Monitoring baseline policy evidence and configured notification
  recipients, if any
- rollback notes and owner
- note that the old dashboard is archived/reference-only, not the canonical
  Goat OS execution surface

## Clean-Slate Environment Shape

1. Create or reset `goatos-dev` Cloud SQL cleanly.
2. Run schema migrations with a dedicated migration job. Do not use API startup
   migrations.
3. Create runtime secrets through Secret Manager or Cloud Run environment
   bindings only.
4. Configure real JWKS auth for dev.
5. Seed only approved grants and representative sample data.
6. Deploy API and admin-web first.
7. Enable workers in controlled order after API/admin smoke passes:
   outbox relay, domain consumer, obligation sweeper, calendar projector,
   reminder/escalation sweepers, notification dispatcher in dev-safe mode,
   inventory batch reconciler, and the domain processed-event retention
   sweeper.
8. Verify Cloud Monitoring baseline policies before enabling unattended worker
   schedules: Cloud Run errors, outbox relay dead letters, Pub/Sub DLQ backlog,
   and Cloud SQL CPU pressure. Configure approved recipients through private
   Terraform `monitoring_alert_email_addresses` and keep personal addresses out
   of committed files.

## Seed Ledger

Keep a committed or attached seed ledger for every run. The ledger must include
stable IDs, source fixture names, actor, timestamp, and the validation scenario
covered.

Minimum seed set:

- Tenant and four approved user grants.
- Parks, sheds, shed profiles, and `animal_stage_lookup` rows that prove shed
  profile stage can override stale goat stage.
- Operators and verifiers with explicit scopes.
- SOP template and published SOP version for vaccination proof and verification.
- PHC vaccination protocol version with approved source metadata, schedule rows,
  executable `protocol_rules`, SOP binding, and stock policy item binding.
- Vaccine inventory item and FEFO lots:
  eligible lot, expiring lot, expired lot, quarantined lot, and insufficient lot.
- Sample goats:
  eligible ET/K1 day-21 goat, age-ineligible goat, lifecycle-ineligible goat,
  location-ineligible goat, missing DOB goat, shifted goat, exited goat, sick or
  quarantined goat, recovered goat, stock-blocked goat, missed goat,
  proof-pending goat, rejected/rework goat, and completed/verified goat.
- Import fixtures for admin sheet flows:
  shed create, goat create, duplicate row replay, invalid shed/profile, invalid
  DOB, and unsupported lifecycle or stage.

## Dev Validation Flow

Run the validation through both UI creation and sheet import:

1. Create a shed in `/counts/herd` and confirm the backend row, profile, and
   stage lookup.
2. Import sheds from a sheet fixture and confirm duplicate replay is idempotent.
3. Create goats through UI and through sheet import.
4. Confirm `goat.created` generates obligations only for eligible goats.
5. Run the obligation sweeper and verify rule-aware batch grouping.
6. Confirm SOP task creation and proof submission.
7. Verify accept, reject/rework, resubmit, and final accept.
8. Confirm stock reserve uses planned date and stock consume happens only after
   accepted completion.
9. Confirm missed, blocked, deferred, rework, proof-pending, and completed states
   appear correctly in Calendar, Action Center, Protocol Adherence, and
   vaccination execution.
10. Confirm DLQ/retry visibility for forced worker failures.

## Google Readiness Gates

Do not call the dev deploy complete until there is evidence for:

- local backend focused tests for SOP, protocol, vaccination, obligation,
  inventory, calendar, and domainconsumer
- `git diff --check`
- migration job logs
- Cloud SQL connection and schema version
- API health and authenticated request
- admin-web authenticated smoke
- outbox relay and domain consumer processing a seeded event
- domain processed-event retention sweeper dry-run showing bounded old-row
  cleanup
- outbox DLQ operator job list-mode output
- Pub/Sub DLQ inspect subscription pull/ack proof in dev-safe mode
- sweeper and projector logs
- notification/DLQ dev-safe visibility
- screenshots for the vaccination slice and command surfaces
- seed ledger and rollback notes

## Future Real Migration Strategy

The later migration is a separate import/backfill program, not the dev seed and
not a runtime dependency on Sheets.

Use approved Sheets or BigQuery snapshots as immutable inputs. The migration job
must support dry-run, checksums, row-level ledger, idempotency keys,
same-key/different-payload conflict detection, bounded batches, retry from a
checkpoint, and a rollback or quarantine path for rejected rows.

The expected future order is:

1. Snapshot approved legacy sources.
2. Profile and classify source quality.
3. Map legacy fields to canonical Goat OS tables and event semantics.
4. Dry-run into staging tables.
5. Reconcile counts against legacy dashboards and source ledgers.
6. Promote clean rows into canonical tables through the same app/domain paths
   used by normal writes when feasible.
7. Generate obligations/read models through the operational kernel.
8. Produce a signed migration evidence pack before any production cutover.
