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

We are currently building the **Vaccination Process Integrity Slice**.

```text
Admin Config + SOP/protocol rules
        ↓
PHC vaccination operations
        ↓
Parks vaccination execution context
        ↓
Control Tower gap summary
```

The identity spine remains the foundation, but the active product build is not
the old dashboard/import-review track. The current slice asks one question:

```text
Is the vaccination process intact for each park/shed, and if not, who owns the
next action?
```

## Current Progress

### Built

The local foundation is built and tested against Postgres:

- Goat passport identity read model and contextual goat drilldown.
- RFID / old-tag identifier attach and retire flows with audit/idempotency.
- Auth/RBAC grant plumbing, local dev token helpers, and auth-session audit.
- Audit log and decision records.
- Domain events and outbox foundation.
- Local outbox relay foundation.
- Protocol definitions, protocol versions, source-backed publish gate, and SOP
  proof-policy skeletons.
- Vaccination obligation generation, due-window Action Center rows,
  verification queue, accept/reject verification, stock reserve/consume hooks,
  and goat vaccination passport aggregation.
- Admin-web current surface: Control Tower shell, PHC/Vaccination, Admin Config,
  Protocol Adherence, contextual Goat Passport, and mock-fidelity checks.
- Bootstrap bearer auth plus tenant-scope RBAC from `user_scope_grants`.
- Generated OpenAPI TypeScript client package and drift gate.
- Legacy BigQuery migration/replay tooling has been removed from the active
  runtime direction. Historical exports may still be useful as audit/reference
  material, but current product work must not rebuild a BigQuery-backed
  dashboard or import-review loop unless the scope is explicitly reopened.
- The current active admin-web direction is the connected Admin Config + PHC
  Vaccination + Parks vaccination execution slice, with Control Tower
  summarizing process gaps instead of legacy dashboard parity.
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
The identity spine and vaccination engine foundation are built.
The active build is Admin Config + PHC Vaccination + Parks vaccination context.
```

### Built — Phase 1A Protocol & Vaccination Backend (local, tested)

On top of the identity spine, the Phase 1A protocol/obligation engine and the
PHC vaccination module are built and green against local Postgres (full
`go test ./...`, migration, sqlc, and query-plan gates). This is **backend +
APIs only — not shipped** (see pending list below).

- Generic obligation engine: protocol definitions/versions/rules, a
  source-backed publish gate (only `vaccinations_db`/`phc`/`vet`
  approved values can be published — manual/unsourced values stay draft), and
  per-goat obligation generation (SM-1), cancel-on-exit (SM-3), shed-shift
  re-scope (SM-2, minimal), drive sweep → batch → SOP task → FEFO stock reserve
  (SM-4), completion + verification + stock consume/release (SM-5), and booster
  scheduling (SM-7). Idempotent throughout; in-process event dispatch behind
  Pub/Sub-ready handler interfaces.
- Two-phase SOP verification: dose recorded at submit, verified at review; a
  task-level SOP verify/rework fans out to one vaccination outcome per recorded
  completion.
- Live impact preview: real eligible/catch-up/obligation/batch counts and doses
  required-vs-available with stock/expiry warnings (no mock math).
- HTTP APIs behind the app boundary (tenant-scoped, paginated, indexed,
  query-plan-checked): protocol config + publish, vaccination impact-preview,
  Action Center (due obligations), Verification queue (awaiting-review
  completions), and the Goat Passport read (history + next-due + last dose).

No production vaccine schedule values exist yet — the module ships the engine
and a SOP execution/proof skeleton only; real rules require source-backed PHC
values before publish.

### Not Finished Yet

The local foundation is strong, but the product is **not production-launch-ready
yet**.

Still pending for the current vaccination slice:

- Production identity-provider provisioning behind the JWKS mode: real IdP
  endpoint, signing keys, sessions, key rotation, revocation, and secret
  management.
- Parks vaccination execution layer: park/shed/stage/defer context, owner
  chain, blockers, and SOP/proof state around each vaccination drive.
- Action Center computed work-state model below the UI: due, overdue, blocked,
  proof-pending, verification-pending, rejected, deferred, owner-missing, and
  completed/recent states from real workflow sources.
- Real source-backed PHC vaccine rule values before any production publish.
- Real Pub/Sub verification/booster egress (today the local path is in-process).
- Real production event publishing (Pub/Sub egress) and the outbox publisher
  worker deploy.
- Applying the approved `goatos-dev` Layer 1 foundation Terraform plan, then
  pushing images, populating secrets out-of-band, deploying Cloud Run
  services/jobs, running migrations, and importing real legacy data under
  explicit approval gates.
- Deployment/provisioning of shared/staging/prod under `vgoats.com`.

Intentionally not active now:

- Old Import Review, conflict/candidate queues, correction queues, counts,
  mortality, legacy sync, and BigQuery-backed dashboard parity.
- Generic Parks dashboard pages unrelated to vaccination execution.
- Standalone Action Center and full Control Tower products before the operating
  Admin/PHC/Parks surfaces are real.

Immediate next order:

```text
1. Build Parks vaccination execution context around the PHC vaccination rows:
   park, shed, stage, defer status, owner chain, blocker, SOP/proof state.
2. Complete the Action Center work-state model underneath PHC/Parks without
   turning it into a standalone product yet.
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

## Execution Roadmap

The old phase ladder is no longer the active build plan. The current roadmap is
ordered around the vaccination process-integrity slice:

```text
1. Admin Config / SOP Policy
   Source-backed protocol rules, proof policy, publish/versioning, and impact
   preview.

2. PHC Vaccination Operations
   Vaccination obligations, drives, proof, verification, missed/deferred
   handling, and goat vaccination passport history.

3. Parks Vaccination Execution
   Park, shed, animal stage, defer status, blocker, owner chain, SOP/proof
   status, and verification status around each vaccination drive.

4. Action Center Work-State Model
   Due, overdue, blocked, proof-pending, verification-pending, rejected,
   deferred, owner-missing, and completed/recent states exposed from real
   workflow sources.

5. Control Tower Summary
   Process intact/not intact, where, severity, owner, and next action. Control
   Tower summarizes broken or at-risk vaccination process only; it is not a
   generic KPI dashboard.

6. Production Hardening
   Real IdP/JWKS auth, Pub/Sub event egress, Cloud Run deploy, observability,
   query-plan gates, and source-backed PHC rule values before production
   publish.
```

Later verticals such as feed direction, health, breeding, procurement, sales,
devices, analytics, and AI analyst remain real Goat OS scope, but they should
not pull the current build away from Admin Config + PHC Vaccination + Parks
vaccination execution.

## What The CEO Should Know Today

Current status:

```text
We are not just making screens.
We are building the operating workflow first.
```

The current build is focused on the vaccination process-integrity layer:

- Admin Config and source-backed protocol rules.
- PHC vaccination obligations, drives, proof, and verification.
- Parks vaccination context: park, shed, stage, defer state, owner, blocker.
- Action Center work states underneath the operating screens.
- Control Tower summary only after the underlying gaps are real.

This is the right order because Control Tower should summarize real operating
gaps, not decorate incomplete workflows.

## Immediate Next Steps

Recommended next execution order:

1. Build Parks vaccination execution context around the current PHC vaccination
   rows.
2. Finish the Action Center work-state backend model without introducing a
   separate standalone Action Center product yet.
3. Keep Admin Config category-driven while wiring vaccination SOP/proof policy
   from source-backed protocol versions.
4. Add production identity-provider integration later: JWKS/asymmetric token
   verification, login/session handling, key rotation, revocation, and secret
   management.
5. Prepare production deploy/event work separately: cloud deployment and event
   egress.

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

Goat OS is now focused on the vaccination process-integrity slice.

The identity and vaccination foundations are strong, but the product is not
user-ready until Parks execution context, the full work-state model, production
auth, event egress, and deployment are complete.
