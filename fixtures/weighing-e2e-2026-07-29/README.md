# Weighing E2E Seed Fixture

Status: seed/E2E contract, backend service regression lane, and runnable local
DB seed importer for the Weighing v1 implementation.

Business date: `2026-07-29`.

This fixture describes the local DB and mobile E2E scenario that must stay
materialized by the Weighing backend/mobile implementation. It is intentionally
machine-readable so the seed lane can fail loudly when required coverage drifts.

## Files

| File | Meaning |
|---|---|
| `weighing-seed.json` | Canonical seed scenario covering roles, campaign, selected sheds/partitions, animals, proof/outbox cases, mobile route expectations, and scale profile. |
| `weighing-seed.test.mjs` | Node test wrapper for the fixture verifier. |
| `tools/dev/validate-weighing-e2e-fixture.mjs` | Strict verifier used by local QA and future seed closeout wiring. |
| `backend/cmd/seed-weighing-e2e` | Idempotent local DB importer for this fixture. |
| `backend/internal/weighing/app/service_test.go` | Service regression that drives the fixture's core create/publish/execute/RBAC/idempotency/category path. |

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
go run ./cmd/seed-weighing-e2e -dry-run -fixture ../fixtures/weighing-e2e-2026-07-29/weighing-seed.json
```

Import into a migrated local database:

```bash
cd backend
DATABASE_URL=postgres://... go run ./cmd/seed-weighing-e2e -fixture ../fixtures/weighing-e2e-2026-07-29/weighing-seed.json
```

The remaining closeout step is the mobile E2E driver. It should point Android
at the laptop backend after this seed import and drive the same ids through
Room/outbox/proof/RFID paths. The fixture verifier must continue to run before
DB writes so a missing scenario is caught before a partial seed creates
misleading evidence.
