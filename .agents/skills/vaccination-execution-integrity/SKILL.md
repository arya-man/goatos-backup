# Vaccination Execution Integrity

Use this skill for vaccination scheduling, drive assignment, operator cards, scan execution,
proof capture/upload, live tracker, CEO reporting, recovery repair, or worker/sweeper changes.

## Required Invariants

- Card identity is the operator-day operational assignment/location, never an arbitrary source
  task, batch, vaccine rule, or proof row.
- Counts shown as animals are distinct animals. Dose/administration counts must be named as such.
- One animal may have one, two, or three vaccine obligations without splitting its shed card.
- A manual drive/date assignment survives publish, reconcile, recovery, and sweeper passes.
- Recovery cannot move work before birth-age, post-arrival, previous-completion, or clinical-gap
  floors. Cancellation and repair use lifecycle events/outbox, never a bare due-date update.
- Completed proof reconciliation is cycle-bound and idempotent, while legacy uploads remain safe.
- A captured vaccination video remains durable when the user leaves the screen. Processing failure
  saves the existing original to Gallery; retry reuses that file; a second processing failure may
  upload that original. Never require re-recording an already administered vaccine.
- Firebase/client telemetry and backend logs include proof, task, obligation-cycle, processing
  stage, retry attempt, exception class, and terminal upload/reconciliation outcome.
- The kernel worker is min 1 / max 2 in STG. Advisory locks, not a permanently warm duplicate,
  enforce singleton stage execution.

## Mandatory Fixtures

Reproduce the production-shaped mixed-source assignment: two animals, one operational assignment,
same shed/partition/date/operator, obligations originating in different batches, and only one
source task. Add one-, two-, and three-vaccine-per-animal cases. Assert one card, distinct-animal
counts, correct vaccine labels, a non-empty scan roster, and complete closeout.

## Proof Gates

1. Run focused Go integration tests against disposable Postgres for execution grouping, worker SQL,
   recovery floors, proof reconciliation, CEO aggregates, and legacy lane resolution.
2. Run Android unit tests for card/list-to-scan identity and proof processing/gallery/retry events.
3. Run `make mobile-guard`, `make mobile-guard-audit`, and affected `make ci-local` jobs.
4. Have independent judges review the last month of related commits and the current sync contract.
5. Only after sign-off, run the exact fixture end to end on the authorized operator and CEO devices,
   visually inspect screenshots, and verify DB, API, Firebase/client events, backend logs, and GCS.
6. Do not merge or deploy from focused tests alone. Record exact SHA and evidence in the active
   progress document, then run the repository landing receipt after the final rebase.
