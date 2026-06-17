# Goat OS

Goat OS is Mesha's full-stack operating system for managing goats, farm work,
proof, verification, devices, analytics, and AI-assisted decisions.

The goal is simple:

```text
No more scattered truth across Sheets, Firebase, WhatsApp, Slack, and local files.

Every goat, task, proof, health record, movement, sale, device reading, and decision
should eventually live in one governed Goat OS backend.
```

## What We Are Building

Goat OS is not just a dashboard. It is the core system of record for Mesha's
goat operations.

In short:

- Every goat gets one permanent Goat OS passport.
- RFID, old tag, breed, sex, age, status, location, ownership, and custody are
  cleaned and connected.
- Dirty records do not silently become truth; they go into review.
- Every important action creates an audit trail.
- Field work becomes assigned tasks, not loose WhatsApp/Slack messages.
- Proof photos/videos get attached to the right goat or task.
- Dashboards read from clean backend data, not giant live Sheets.
- AI can suggest, but humans approve important identity decisions.

## Current Focus

We are currently building **Phase 1: Goat Passport and Herd Registry**.

This is the foundation phase.

Before vaccination, health, breeding, sales, devices, or analytics can be
trusted, Goat OS must first know:

```text
Which goat is this?
What is its permanent ID?
Which RFID/old tag belongs to it?
Is this a duplicate record?
Was this goat merged, corrected, retired, sold, or dead?
Can we prove where this fact came from?
```

Think of Phase 1 as the Aadhaar/passport layer for goats.

## Current Progress

### Built

The Phase 1 local identity spine is built and tested: import, apply, canonical
goat passport creation, search/read APIs, identity counters, auth/RBAC, and
local rehearsal all work against local Postgres.

Completed so far:

- Postgres database schema for goat identity.
- Goat passport identity model.
- RFID and old-tag identifier rules.
- Duplicate and conflict handling.
- Merge handling for duplicate goat records.
- Correction/candidate/review schema and contracts.
- Legacy RFID parser/import harness and safe import foundation.
- Clean RFID rows can be applied into canonical goat records.
- Local full-stack rehearsal for source discovery, dry-run, staging, masked
  anomaly/reviewer CSV exports, RFID apply, counter rebuild, backend API smoke,
  and admin-web build against Docker Postgres.
- Audit log and decision records.
- Domain events and outbox foundation.
- Local outbox relay foundation.
- Analytics count projection foundation.
- Rebuild and incremental counter update workers.
- Bootstrap bearer auth plus tenant-scope RBAC from `user_scope_grants`.
- Generated OpenAPI TypeScript client package and drift gate.
- Admin-web Phase 1 Mesha-style local demo surface with executable
  BigQuery/Sheets routes removed.
- Admin-web upgraded to the frozen Next 16 / React 19 / Tailwind 4 framework
  baseline before real screens were added.
- SSR-first Mesha admin-web shell with legacy-style dark sidebar, compact module
  navigation, KPI cards, charts, and dense tables for herd search, goat
  passport, identity counts, live conflicts/candidates/correction queues, and
  live Import Review summary/rows. Import Review now lists recent tenant import
  runs so operators do not need to memorize UUIDs, while still allowing an older
  run id to be pasted when needed. The UI includes backend-owned CSV download
  buttons for all messy Import Review rows and the current row filter, with
  server-side token handling. The UI also
  exposes already-built Phase 1 actions for correction request create/resolve,
  candidate reject, conflict reject/merge, field-check requests,
  identifier-dispute marking, and goat identifier add/retire through
  server-side actions. Non-Phase-1 legacy modules and undefined review/fix
  actions stay disabled or honest placeholders.
- Local dev token/grant helpers plus auth smoke for backend and admin-web client plumbing.
- Legacy BigQuery is now the temporary upstream for current herd
  reconciliation/backfill until operators move daily updates into Goat OS mobile
  and backend workflows. The local XLSX importer remains a parser/regression
  harness only; private workbook exports are no longer source of truth for
  Phase 1 data correction. The intended current-data path is
  `legacy BigQuery -> BQ sync/reconciliation job -> Goat OS Postgres -> backend
  APIs -> admin dashboard`. Dashboards must never read BQ directly. A future
  admin "Sync with BQ" control should trigger the same backend job that scheduled
  local/dev/stg/prod syncs use, with RBAC, audit, idempotency, and visible
  freshness status. `backend/cmd/bq-reconcile` is the committed replay path for
  current lifecycle/location reconciliation plus BQ-backed attribute fill/review: it
  consumes read-only legacy BQ
  event and latest-location exports, dry-runs by default, applies only with
  `--execute`, audits each changed goat, and must be followed by
  identity-counter rebuilds. BQ-derived
  reconciliation updates now correct matched Goat OS passports for lifecycle,
  park/current location, and safe shed assignment where the match is
  deterministic. It may fill blank local sex/breed values from deterministic BQ
  evidence and may normalize same-meaning breed labels, but it does not overwrite
  nonblank passport sex/breed when BQ disagrees. Those disagreements, plus BQ
  self-contradicting sex/breed evidence, open Data Quality conflicts for review.
  When an import run is supplied, it can fill sole-reason `blank_gender` import
  rows from deterministic BQ RFID evidence.
  `--backfill-missing` is currently fail-closed: the legacy dashboard aggregate
  proves count gaps, and the BQ event/latest-location exports reconcile existing
  matched passports, but they do not provide one deterministic current identity
  row per missing passport. Missing-passport creation stays blocked until an
  accessible per-goat current identity export/bridge is available; creating
  passports directly from aggregate counts or event-history keys would be fake
  data.
  The one supported way to create missing passports is the explicit,
  deterministic `bq-reconcile --backfill-candidates-csv <path>` path. It reads a
  pre-vetted safe old-tag candidate CSV (one unique farm/old-tag/scope row each),
  creates old-tag-only passports (`identity_state='clean'`, Mesha custodian, no
  RFID), maps status to a supported lifecycle (`active->alive`, `sold->sold`,
  `dead->dead`, `inactive->inactive`), and should be paired with
  `--locations-json <latest-location-export>` so stale `Inactive` candidates
  from `census_plus_bq_unique_farm` can be corrected from later Shifting/Death
  evidence without resurrecting Sold goats. It sets breed/sex/park/shed only
  from deterministic candidate/latest-location fields, writes
  `goat.old_tag_backfill_created` audit rows for created goats, and can repair
  the lifecycle of already-created `legacy_bigquery` old-tag backfill goats with
  a row-version bump plus `goat.old_tag_backfill_lifecycle_repaired` audit row.
  It is idempotent on replay (the partial-unique active old_tag index is the
  backstop). Rows whose farm has no active park location fail closed
  (`unknown_park`) rather than creating a location-less passport. It is dry-run
  by default; `--execute` is gated to a local/dev local database (same guard as
  `rebuild-identity-counters`) because it bypasses the `goat_identity_events`
  stream — so event-driven counter freshness/analytics egress can't see these
  goats. Run the full `rebuild-identity-counters` (not the incremental updater)
  afterward; promotion beyond local/dev needs a production-safe capture +
  counter-sync path first.
  BQ dashboard shed
  taxonomy is now seeded under the existing CBE/CPT park locations so matched
  goats can carry specific shed current locations without breaking old-tag
  park-scope identity rules.
- Phase 1 local end-to-end proof has passed against the local backend and
  Mesha-style SSR admin-web. Current local DB has 2668 goat passports (1219 from
  the RFID import plus 1449 created by the deterministic safe old-tag backfill)
  and 4 import-review rows from the RFID import run. A BQ reconciliation pass and
  the old-tag backfill both run through `backend/cmd/bq-reconcile`. The current
  local DB has `tenant_lifecycle` counters `alive=2088`, `sold=544`, `dead=36`,
  and `inactive=0`; BQ
  lifecycle is reduced conservatively per identifier. Shifting or Abortion
  evidence without terminal Sale/Death is proof-of-life, Death is sticky, Sale
  is reversed only by a later Purchase, and later non-purchase activity such as
  Shifting, Birth, or Abortion after a terminal event opens a review conflict
  instead of silently reviving the goat. BQ attribute reconciliation now keeps
  nonblank passport sex/breed unchanged when BQ disagrees, opens review
  conflicts instead of silently choosing a side, and only filled the former
  blank-gender staging rows plus same-meaning breed-label normalization. RFID evidence is
  evaluated separately from reused/scoped old-tag evidence for lifecycle, while
  passport attribute checks look across all matched BQ evidence so RFID/old-tag
  sex or breed contradictions become review conflicts instead of hidden clean
  passports. Current
  disagreements and terminal-after-activity cases are surfaced as 232 open Data
  Quality conflicts for operator review. Goat identity state is 2465 `clean` and
  203 `needs_review`; these review states are the intended safety net for
  BQ/passport disagreements. A follow-up BQ
  location pass seeded 154 CBE/CPT shed locations. A current local DB
  measurement against `goats` joined to `locations` found 1702 goats with
  shed-level current locations, 631 park-only goats, and 335 goats with no
  current location.
  Overview, herd search, goat passport with live identity timeline, identity
  counts, live Import Review, and live correction-request queue reads rendered
  against the final run. Earlier UI proof rendered honest empty states before BQ
  conflicts existed; current post-BQ proof should render the live Data Quality
  conflict queue for lifecycle and attribute review conflicts, while candidates
  and correction requests remain honest empty states unless populated.
- Local Docker storage runbook plus read-only report and guarded Goat OS temp
  volume cleanup tooling.
- Contract validation and migration validation.
- Docker-backed backend tests for core invariants.
- Dev Google Cloud bring-up through Layer 1 apply is in progress for
  `goatos-dev`: context gates are verified, required APIs are enabled, a
  dev-only budget alert exists, the Terraform state bucket is bootstrapped, and
  the foundation apply has created the non-SQL Layer 1 resources. Cloud SQL is
  being brought up in a running dev posture for raw Cloud Run dashboard
  verification. No Cloud Run services/jobs, images, migrations, or legacy
  imports are live yet.

In short:

```text
The backend identity/reporting/event foundation is built and green.
```

### Not Finished Yet

The local Phase 1 identity/admin-review spine is complete; Phase 1 is **not
production-launch-ready yet**.

Done in the Phase 1 closeout (see
`docs/phases/phase-01-goat-passport/CLOSEOUT-SCOPE.md`):

- Candidate approve: attach-identifier and merge-goats outcomes (backend +
  client). The candidate-queue admin-web form drives the merge (same-goat)
  outcome; attach-identifier is driven from the goat passport identifier panel.
- Import Review row actions (reject / fix sex-breed / reapply) — backend +
  admin-web UI.
- Provider-agnostic JWKS/asymmetric bearer verification mode.
- Deployment runbook + per-env config.

Still pending before Phase 1 is production-launch-ready:

- Production identity-provider provisioning behind the JWKS mode: real IdP
  endpoint, signing keys, sessions, key rotation, revocation, and secret
  management.
- Real production event publishing (Pub/Sub egress) and the outbox publisher
  worker deploy.
- Applying the approved `goatos-dev` Layer 1 foundation Terraform plan, then
  pushing images, populating secrets out-of-band, deploying Cloud Run
  services/jobs, running migrations, and importing real legacy data under
  explicit approval gates.
- Deployment/provisioning of shared/staging/prod under `vgoats.com`.

Intentionally blocked / deferred (not unfinished Phase 1 code):

- Conflict `create_goat` and approve-to-create: blocked until the
  operator-entered goat-creation field set is product-approved.
- `POST /admin/import-runs`, `POST /admin/goats`,
  `PATCH /admin/goats/{goat_id}`: deferred to the Phase 2+ admin workflow.
- Legacy Sync real executor: production sync follow-up.

Immediate next order:

```text
1. Run the live admin-web visual smoke for the candidate approve + Import Review
   row-action UI (make dev-local, then smoke:visual:live) and inspect the
   screenshots; it is the one remaining Phase 1 UI QA step.
2. Make the product decision for conflict create_goat (required operator fields,
   primary identifier evidence, breed/species/status/sex, ownership/custody/
   location, duplicate checks); implement only after that contract is written.
3. Provision production auth (IdP/JWKS + secrets), event egress, and cloud
   deploy under vgoats.com without calling the local dev-token demo shippable.
```

Important:

```text
Local identity spine is built.
User-facing product is not complete yet.
```

## Local Dev Start

Use this command for day-to-day local UI work:

```bash
make dev-local
```

It starts or reuses the local API on `127.0.0.1:8080`, seeds the local
`ceo_internal` grant idempotently, mints a fresh server-side bearer token, and
starts Mesha admin-web on `127.0.0.1:3300`. If the browser shows
`401 invalid_bearer_token`, restart through this command instead of reusing an
old shell token.

## Local Development Storage

Daily Goat OS development runs locally with Docker Postgres, tests, and small
synthetic data. GCP is not required for normal coding. Future `goatos-dev`
Cloud SQL is for explicit cloud rehearsal, and future `goatos-stg` Cloud SQL is
where the persistent 1M-goat benchmark belongs. Local 1M tests must be
temporary: create an explicit temp volume, run the test, export the summary, and
delete the temp volume.

Use the local Docker storage runbook before large local tests:

```text
docs/runbooks/local-docker-storage.md
```

Use the local full-stack rehearsal runbook before touching real RFID exports:

```text
docs/runbooks/local-full-stack-rehearsal.md
```

On Docker Desktop for Mac, deleting Docker containers/images/volumes frees space
inside Docker's Linux VM first. The host-side `Docker.raw` or VM disk image may
still need Docker Desktop reclaim/reset workflow before macOS shows the space as
available.

Later Goat OS cloud defaults are Mumbai-first (`asia-south1`), never US by
default.

## What "Without Sheets Or Firebase" Means

Sheets and existing files can still be used as **input sources** during
migration.

But they should not remain the long-term truth.

The target architecture is:

```text
Old Sheets / XLSX / Firebase / Slack records
        ↓
Controlled import and review
        ↓
Postgres Goat OS backend
        ↓
APIs, frontend, mobile, dashboards, analytics, devices
```

So the system can still read old data, but the clean official truth should live
in Goat OS.

## Product Phases

### Phase 1: Goat Passport And Herd Registry

Status: **active implementation**

Goal:

```text
Create the trusted identity layer for every goat.
```

Includes:

- Goat passport.
- RFID and old tag cleanup.
- Duplicate detection.
- Merge/correction decisions.
- Basic goat search/read APIs.
- Import of existing goat records.
- Identity audit trail.
- Identity analytics counters.

Current status:

```text
Local import/apply/read/counter spine is built.
Frontend screens, production auth, production event egress, and cloud deploy are
still pending.
```

### Phase 2: SOP Forms, Task Engine, And Shifting

Status: **planned next product workflow**

Goal:

```text
Replace the first Slack/App Script operating workflow with assigned Android
tasks, versioned SOP forms, proof, verification, and backend-owned domain
events.
```

Includes:

- SOP definition/versioning.
- Admin web SOP configuration and preview.
- Task generation and assignment.
- Operator assignment.
- Android/operator task view.
- Shifting SOP as the first implementation.
- Repeat-for-each-goat batch submission.
- Conditional required fields and approval gates.
- Photo/video proof.
- Park head and central verification.
- Goat location/movement history update after approval.

This is the first real field-work module after the goat passport foundation.
Vaccination remains a natural next SOP on the same engine, but Shifting is the
first walking skeleton because it is daily, barebones, and directly proves
current-location truth.

### Phase 3: Health, Treatment, Death, And Verification

Goal:

```text
Track sick goats, treatment, quarantine, recovery, death, and proof.
```

Includes:

- Sick goat reporting.
- Diagnosis.
- Treatment sessions.
- Medicine and withdrawal periods.
- Recovery follow-ups.
- Death/abortion records.
- Verification queue.

### Phase 4: Workforce, Park Operations, And Daily Control

Goal:

```text
Know who is responsible for each task, park, shed, and goat operation.
```

Includes:

- Roles.
- Rosters.
- Shift assignment.
- Absence/backfill.
- Escalations.
- Park head visibility.

### Phase 5: Growth, Feed, Weight, And Movement

Goal:

```text
Track goat growth, feeding, weight, and movement history.
```

Includes:

- Weight records.
- Feed records.
- Movement/shifting.
- Growth tracking.
- Feed exposure history.

This connects later to hardware feeding panels.

### Phase 5B: Crop, Fodder, Farmer Network, And Feed Supply

Goal:

```text
Track the feed supply side that supports goat operations.
```

Includes:

- Fodder/crop source.
- Farmer network.
- Supply planning.
- Feed inventory inputs.

### Phase 6: Breeding, Pregnancy, Kidding, And Genetics

Goal:

```text
Track reproduction, lineage, kidding, and genetic quality.
```

Includes:

- Breeding events.
- Pregnancy.
- Kidding.
- Parentage.
- Genetics/data-quality warnings.

### Phase 7: Procurement, Inventory, And Cost

Goal:

```text
Track procurement, arrival, inventory, source batch, and cost.
```

Includes:

- Procurement load proof.
- Arrival proof.
- Source batch history.
- Cost attribution.

### Phase 8: Sales, Allocation, Meat Yield, And Exit

Goal:

```text
Know which goat can safely be promised, sold, allocated, dispatched, or replaced.
```

Includes:

- Booking/allocation.
- Double-book prevention.
- Sale readiness.
- Replacement/substitution.
- Dispatch/exit proof.
- Meat yield and final outcome.

### Phase 9: Devices, R&D, And AI Assistance

Goal:

```text
Bring hardware, sensors, and AI into Goat OS without letting them become unverified truth.
```

Includes:

- Feeding panel/device integration.
- Sensor data.
- AI suggestions.
- AI guardrails.
- Human approval for risky decisions.

### Phase 10: Command Center, Analytics, And AI Analyst

Goal:

```text
Give leadership a governed command center for operations, risk, performance, and decisions.
```

Includes:

- Executive dashboards.
- Operational risk views.
- Promise safety.
- Analytics.
- AI analyst assistance.

## What The CEO Should Know Today

Current status:

```text
We are not just making screens.
We are building the trusted backend foundation first.
```

The current build has focused on the hardest base layer:

- Clean goat identity.
- Safe imports.
- Auditability.
- Merge/correction logic.
- Event pipeline.
- Reporting counters.
- Backend validation.

This is the right order because vaccination, health, breeding, sales, devices,
and AI all depend on correct goat identity.

## Immediate Next Steps

Recommended next execution order:

1. Candidate approve (attach/merge) and Import Review row reject/fix/reapply are
   implemented; run the live admin-web visual smoke for them, then make the
   conflict create_goat field-set product decision before implementing it.
2. Keep later app/admin creation work separate: import-run create and admin
   goat create/update.
3. Add production identity-provider integration later: JWKS/asymmetric token
   verification, login/session handling, key rotation, revocation, and secret
   management.
4. Prepare production deploy/event work separately: cloud deployment and event
   egress.
5. Start Phase 2 SOP/task workflow with Shifting as the first production SOP.
6. Later connect hardware feeding panel data under Phase 5/9.

## Build Principle

Goat OS follows one rule:

```text
If the fact matters, it must be traceable, reviewable, and stored in the backend.
```

That means:

- No silent merges.
- No hidden spreadsheet truth.
- No AI auto-approving canonical facts.
- No dashboard counts from raw giant table scans.
- No production decisions from unverified dirty data.

## Engineering Status

Backend stack:

```text
Go backend
Postgres database
OpenAPI contracts
sqlc typed SQL
goose-style SQL migrations
explicit wiring
no ORM
no Firebase as source of truth
```

Current validation includes:

```text
backend tests
contract drift checks
migration validation
sqlc generation checks
query plan checks
Docker-backed Postgres integration tests
```

## One-Line Summary

Goat OS Phase 1 is building the trusted goat identity foundation.

The backend foundation is strong and progressing well, and the Mesha admin-web
local demo surface now renders the live identity spine plus defined Phase 1
actions. The product is not user-ready until the remaining review/write
workflows, production auth, and production deployment pieces are completed.
