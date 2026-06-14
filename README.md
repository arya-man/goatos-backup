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
  live Import Review summary/rows when an import run id is provided. The UI now
  includes backend-owned CSV download buttons for all messy Import Review rows
  and the current row filter, with server-side token handling. The UI also
  exposes already-built Phase 1 actions for correction request create/resolve,
  candidate reject, conflict reject/merge, and goat identifier add/retire
  through server-side actions. Non-Phase-1 legacy modules and undefined
  review/fix actions stay disabled or honest placeholders.
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
  current lifecycle/location correction plus BQ attribute-conflict surfacing: it
  consumes read-only legacy BQ
  event and latest-location exports, dry-runs by default, applies only with
  `--execute`, audits each changed goat, and must be followed by
  identity-counter rebuilds. BQ-derived
  reconciliation updates now correct matched Goat OS passports for lifecycle,
  park/current location, and safe shed assignment where the match is
  deterministic. It fills safe missing sex values from BQ and marks BQ-vs-Goat
  OS gender/breed disagreements as Data Quality conflicts rather than silently
  overwriting passport fields. BQ dashboard shed
  taxonomy is now seeded under the existing CBE/CPT park locations so matched
  goats can carry specific shed current locations without breaking old-tag
  park-scope identity rules.
- Phase 1 local end-to-end proof has passed against the local backend and
  Mesha-style SSR admin-web. Current local DB has 1215 goat passports and 8
  import-review rows from the RFID import run. A BQ reconciliation pass now runs
  through `backend/cmd/bq-reconcile`. The current local DB has
  `tenant_lifecycle` counters `alive=1109`, `sold=71`, and `dead=35`; BQ
  lifecycle is reduced conservatively per identifier. Shifting or Abortion
  evidence without terminal Sale/Death is proof-of-life, Death is sticky, Sale
  is reversed only by a later Purchase, and later non-purchase activity such as
  Shifting, Birth, or Abortion after a terminal event opens a review conflict
  instead of silently reviving the goat. BQ attribute reconciliation now flags
  matched-passport gender/breed disagreements as `status_mismatch` review
  conflicts; cosmetic Anantapur Sheep/Anantapur label drift is reported but not
  treated as a real breed conflict. RFID evidence is
  evaluated separately from reused/scoped old-tag evidence; current
  disagreements and terminal-after-activity cases are surfaced as 54 Data
  Quality `status_mismatch` conflicts for operator review. A follow-up BQ
  location pass seeded 154 CBE/CPT shed locations and updated 860
  deterministically matched goats to shed-level current locations; 83 matched
  goats remain park-only because BQ had no safe shed, and 272 existing passports
  that cannot be joined to BQ by RFID or scoped old tag remain unchanged pending
  identifier reconciliation.
  Overview, herd
  search, goat passport with live identity timeline, identity counts, live
  Import Review, and live correction-request queue reads rendered against the
  final run. The real local DB had no conflicts, candidates, or correction
  rows, so Data Quality rendered honest empty states while backend
  conflict/candidate/correction list coverage proves populated read paths
  separately.
- Local Docker storage runbook plus read-only report and guarded Goat OS temp
  volume cleanup tooling.
- Contract validation and migration validation.
- Docker-backed backend tests for core invariants.

In short:

```text
The backend identity/reporting/event foundation is built and green.
```

### Not Finished Yet

Phase 1 is **not shippable to users yet**.

Still pending before Phase 1 is usable end-to-end:

- Production identity-provider/login integration: JWKS/asymmetric token
  verification, sessions, key rotation, revocation, and secret management.
- The remaining typed `not_implemented` endpoints are:
  `POST /admin/import-runs`, `POST /admin/goats`, and
  `PATCH /admin/goats/{goat_id}`.
- Correction request list APIs and goat timeline reads are live Phase 1B
  surfaces. Defined correction/candidate/conflict/identifier actions are wired
  in admin-web. Source breed/category import rows remain in review until an
  explicit breed/category mapping policy is approved. Admin goat create/update
  APIs, import-run create, and messy-row fix/approve actions remain deferred.
- Candidate approve and conflict `create_goat` routes exist, but they are not
  the same blocker. Candidate approve can be split into non-create outcomes
  that reuse existing identifier-attach or merge invariants, while
  approve-to-create remains blocked on the conflict `create_goat` contract.
  Conflict `create_goat` itself still needs the operator-entered creation field
  set before code may mint a goat from a conflict.
- Real production event publishing.
- Remaining dirty-data actions: candidate approve attach/merge, Import Review
  row reject/fix/re-apply, and the create-dependent paths gated by conflict
  `create_goat`.
- Deployment setup for shared/staging/prod environments.

Immediate Phase 1B order:

```text
1. Define and implement the non-create candidate approve outcomes as their own
   canonical mutation slice: attach identifier and merge goats; return a typed
   blocker for approve-to-create until `create_goat` is defined.
2. Define and implement Import Review row actions that do not require new
   product semantics: row_version, terminal reject with audit,
   fix-with-evidence for known row-local fields, and re-apply through the
   existing RFID apply path when safe.
3. Make the product decision for conflict `create_goat`: required operator
   fields, primary identifier evidence, breed/species/status/sex rules,
   ownership/custody/location requirements, and duplicate checks. Implement it
   only after that contract is written.
4. Prepare production auth, event egress, and deploy without calling the local
   dev-token demo shippable.
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

### Phase 2: SOP Tasks And Vaccination

Status: **planned next product workflow**

Goal:

```text
Replace loose vaccination tracking with assigned tasks, proof, and verification.
```

Includes:

- Vaccination schedules.
- Task generation.
- Operator assignment.
- Android/operator task view.
- Photo/video proof.
- Park head and central verification.
- Goat timeline update after approval.

This is the first real field-work module after the goat passport foundation.

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

1. Keep the local Phase 1 proof reproducible while moving remaining workflow
   stubs into Phase 1B slices: candidate approve attach/merge, Import Review
   row reject/fix/re-apply, and the conflict create_goat field-set decision.
2. Keep later app/admin creation work separate: import-run create and admin
   goat create/update.
3. Add production identity-provider integration later: JWKS/asymmetric token
   verification, login/session handling, key rotation, revocation, and secret
   management.
4. Prepare production deploy/event work separately: cloud deployment and event
   egress.
5. Start Phase 2 vaccination task workflow.
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
