# Weighing Seed Fixture (development/contract seed — NOT an E2E test)

Status: a development/contract seed fixture, a JSON-schema/coverage-field
validator over that fixture, a backend service-level regression input, and a
runnable local DB seed importer for the Weighing v1 implementation.

## What this fixture IS

- A canonical, hand-authored JSON scenario (roles, campaign, selected
  sheds/partitions, animals, proof/outbox cases, mobile route expectations,
  scale profile) that documents the scenario coverage Weighing v1 is expected
  to support.
- A schema/shape validator (`weighing-seed-validation.test.mjs` ->
  `tools/dev/validate-weighing-fixture.mjs`) that fails loudly if the
  fixture file itself drifts from the declared coverage fields.
- An idempotent local DB seed importer (`backend/cmd/seed-weighing-fixture`) that
  can materialize this scenario into a local Postgres instance for manual/dev
  use.
- An input to one backend service-level regression test
  (`backend/internal/weighing/app/service_test.go`) that drives real
  create/publish/execute/RBAC/idempotency/category service calls using data
  shaped like this fixture.

## What this fixture is NOT

- It is **NOT an end-to-end test**. Validating `weighing-seed.json`'s shape
  proves the JSON file matches its own schema — it does not invoke the
  weighing backend API, mobile app, durable event consumers, sweepers, or any
  production code path.
- It is **NOT production-path proof**. No part of this fixture or its
  validator drives capture -> proof-upload -> verification-queue -> verdict ->
  completion through the real service/API/consumer path used in production.
- It is **NOT merge evidence** for a "Weighing E2E lane" claim. Per
  `AGENTS.md`, a seeded readback is not E2E; a real E2E story for Weighing does
  not exist yet. See `docs/features/weighing/e2e-coverage-gap.md` for the
  specific unproven production paths and what a real E2E test still needs to
  drive.

Business date used by the scenario: `2026-07-29`.

## Files

| File | Meaning |
|---|---|
| `weighing-seed.json` | Canonical seed scenario covering roles, campaign, selected sheds/partitions, animals, proof/outbox cases, mobile route expectations, and scale profile. Not itself proof of anything beyond its own shape. |
| `weighing-seed-validation.test.mjs` | Node test wrapper that runs the schema/shape validator against `weighing-seed.json`. This is a validator, not a behavioral test. |
| `tools/dev/validate-weighing-fixture.mjs` | Strict schema/shape verifier used by local QA and future seed closeout wiring. (Lives outside this fixture directory; its own filename still carries the retired "e2e" naming — see the blocker doc for the escalation.) |
| `backend/cmd/seed-weighing-fixture` | Idempotent local DB importer for this fixture. |
| `backend/internal/weighing/app/service_test.go` | Service-level regression that drives the fixture's core create/publish/execute/RBAC/idempotency/category path through the real service layer (this is the closest thing to production-path proof that exists today, but it is a Go service test, not a browser/mobile E2E). |

## Coverage

- Normal individual-animal flow for an expected kid in the selected shed.
- Wrong-shed scan that keeps expected/original and actual/current shed visible.
- Missing/unavailable expected animals classified from current herd truth.
- Off-page RFID scan that updates the scan feed without loading all animals.
- Duplicate scan/idempotency replay that does not increment completion twice.
- Per-animal proof upload retry and removable uploaded-but-unsubmitted proof.
- Per-shed/partition category with shed proof and no animal latest-weight update.
- RBAC personas: CEO/CXO plan, Dinakar monitor-only, Amit execute.
- Delayed roll-forward beyond the campaign week.
- 5k+ scale query path expectations for paged details and indexed RFID lookup.

## Weighing V1 Review Boundaries

This fixture exercises a deliberately free-flow, tag-first Weighing slice. The
selected shed/partition is an empty bucket for captured Weighing evidence, not
a Herd Register or Vaccination roster. Do not review it as a Vaccination-style
workflow:

- scanned identifiers are valid Weighing subjects even when they are not mapped
  to Herd Register goat UUIDs;
- there is no expected animal count or expected animal list per shed for
  individual Weighing submit;
- duplicate scanned identifiers across different shed buckets are allowed in
  V1;
- mobile capture must not require `matchTag()` before recording weight/proof;
- submit closes only the submitted scanned identifiers with completed
  proof-backed observations, never all unscanned expected-roster animals;
- synced/offline state is allowed to restore by scanned identifier instead of
  goat UUID;
- Weighing evidence stays under Weighing tables/storage and must not mutate goat
  identity, goat location, Vaccination assignment, or Herd Register truth;
- admin-web `/weighing` is intentionally hidden until product approval, while
  mobile weighing and backend APIs remain active;
- fixture `location_type` values must use the API enum (`shed`, `cohort`,
  `pen`); shed/partition is business grouping text, not a fourth enum value.

## Remaining Integration Notes

Validate without DB writes:

```bash
cd backend
go run ./cmd/seed-weighing-fixture -dry-run -fixture ../fixtures/weighing-seed-2026-07-29/weighing-seed.json
```

Import into a migrated local database:

```bash
cd backend
DATABASE_URL=postgres://... go run ./cmd/seed-weighing-fixture -fixture ../fixtures/weighing-seed-2026-07-29/weighing-seed.json
```

NOTE: the two `-fixture` paths above still point at `backend/cmd/seed-weighing-fixture/main.go`,
which lives outside this fixture directory (forbidden edit path for this
pass) and still hard-codes `fixtures/weighing-e2e-2026-07-29/...` as its
default/fallback path. Until that file is updated, running the importer
without `-fixture` will look for the OLD directory name and fail to find it.
See `docs/features/weighing/e2e-coverage-gap.md` and blocker 7 in
`context/repo-audits/weighing-phase1-2-do-not-merge-blockers.md` for the exact
file:line references that still need updating.

The remaining closeout step is a real end-to-end test: driving the same
scenario through capture -> proof-upload -> verification-queue -> verdict ->
completion via the actual backend API, durable event consumers, and (for full
production-path proof) the Android mobile app against the laptop backend
through Room/outbox/proof/RFID paths — none of that exists today. The schema
validator here must continue to run before any DB seed writes so a missing
scenario field is caught before a partial seed creates misleading evidence,
but it is not a substitute for that end-to-end test.
