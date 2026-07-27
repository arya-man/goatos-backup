# Weighing E2E Seed Fixture

Status: seed/E2E contract plus backend service regression lane for the Weighing
v1 implementation.

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

## Remaining Integration Notes

The remaining closeout step is a real DB seed/import command plus mobile E2E
driver. The mobile E2E should point Android at the laptop backend and drive the
same ids through Room/outbox/proof/RFID paths. The fixture verifier must
continue to run before DB writes so a missing scenario is caught before a
partial seed creates misleading evidence.
