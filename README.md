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

The Phase 1 backend foundation is now largely built and tested.

Completed so far:

- Postgres database schema for goat identity.
- Goat passport identity model.
- RFID and old-tag identifier rules.
- Duplicate and conflict handling.
- Merge handling for duplicate goat records.
- Correction request flow.
- Candidate review/reject flow.
- RFID workbook staging and safe import foundation.
- Clean RFID rows can be applied into canonical goat records.
- Audit log and decision records.
- Domain events and outbox foundation.
- Local outbox relay foundation.
- Analytics count projection foundation.
- Rebuild and incremental counter update workers.
- Contract validation and migration validation.
- Docker-backed backend tests for core invariants.

In short:

```text
The backend identity/reporting/event foundation is built and green.
```

### Not Finished Yet

Phase 1 is **not shippable to users yet**.

Still pending before Phase 1 is usable end-to-end:

- Auth/RBAC: real login, roles, and permissions.
- Frontend screens for admin users.
- Real production event publishing.
- Running the real private RFID/source workbooks safely.
- Operational review screens for dirty data.
- Deployment setup for shared/staging/prod environments.

Important:

```text
Backend foundation is built.
User-facing product is not complete yet.
```

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
Backend foundation mostly built.
Frontend/auth/data-run still pending.
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

1. Finish Auth/RBAC deploy gate.
2. Build Phase 1 frontend screens.
3. Run controlled real RFID/source data import.
4. Add review screens for dirty/duplicate goat records.
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

The backend foundation is strong and progressing well, but the product is not
user-ready until auth, frontend screens, and production deployment pieces are
completed.
