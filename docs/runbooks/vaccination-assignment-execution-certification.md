# Vaccination Assignment Execution Certification

## Purpose

This runbook certifies vaccination scheduling, operator execution, proof durability, CEO Calendar,
and live-tracker behavior as one lifecycle. A green list-card test is not sufficient evidence.

## Identity Contract

- `vaccination_drive_assignments.assignment_id` is the execution-drive identity.
- `vaccination_drive_assignment_members` is authoritative membership.
- A member obligation may originate from a different `obligation_instances.batch_id` than the
  assignment's `batch_id`.
- `task_id` identifies SOP/proof execution metadata. It does not define assignment membership.
- `rule_id` and vaccine labels are obligations inside an assignment. They must not split a card.
- Canceled assignments and canceled members never appear in execution, Calendar, or CEO totals.
- Legacy rows without assignments retain explicit typed batch/catch-up fallback identities.

## Mandatory Disposable Fixtures

Seed these in the disposable OCI database. Never mutate STG to create test fixtures.

1. Two animals, one ET+TT obligation each, one assignment, obligations from two source batches,
   only one source batch carrying an SOP task.
2. Two animals with two vaccines each in one assignment.
3. Two animals with three vaccines each in one assignment.
4. An unrelated same-day assignment in the same park with Z1/Z3 and a different shed.
5. A canceled assignment member alongside active members.
6. A birth-age ET+TT obligation whose recovery date precedes its DOB+28 rule floor.

For fixtures 1-3, the operator list must show one card and two animals. The roster must contain
exactly the two active assignment members. Vaccine multiplicity changes dose counts and labels,
not animal counts or card count.

## Backend Gates

- Calendar emits separate events for unrelated assignments on the same park/day.
- Calendar labels, sheds, counts, progress, details, and targets are scoped by the same assignment
  identity. Event identity remains stable if the assignment date moves.
- Scan roster resolves active assignment members without requiring source-batch equality.
- CEO operator status reads obligation/completion truth at distinct-animal grain.
- Live tracker preserves vaccine-lane identity for legacy/unmembered rows.
- Recovery/reconcile never moves due dates before birth-age, post-arrival, or previous-completion
  source floors.
- Kernel cancellation never overrides an explicit active assignment/manual date.
- Worker logs contain separate recovery and effective-cohort summaries and no SQL exception.

## Android Gates

- Card tap carries assignment identity into navigation, network request, Room scope, and
  `ScanViewModel`.
- Missing task metadata on an otherwise executable assignment does not route to read-only record.
- A successful HTTP 200 with an unexpectedly empty canonical assignment roster is a contract
  failure and cannot erase a previously known non-empty Room roster.
- The scan screen shows the exact animal RFIDs from `goat_identifiers`, not `goats.display_id`.
- One, two, and three vaccines per animal keep one animal row while exposing all obligations.

## Proof Failure Gates

- The captured original is durably retained before processing starts and saved to Gallery.
- Processing retry uses that same captured file. It never asks the operator to repeat an injection.
- After the configured processing retry, the original file is queued for upload.
- Navigation away, process death, offline state, and app restart preserve the retry row.
- Android emits capture, processing, retry, upload, registration, reconciliation, and terminal
  failure events with bounded context and exception class/message.
- Backend emits upload completion/reconciliation outcomes without logging signed URLs or secrets.
- Remote video is never prepared or streamed without an explicit user action.

## Real E2E Gate

Run only after focused tests, local CI, final rebase, and independent judge sign-off.

1. Install the signed `prodDebug` build on the maintainer-visible Android profiles only.
2. Infinix operator: verify one assignment card, two animals, correct ET+TT labels, RFID roster,
   capture, proof retry/failure behavior, submit, and final closure.
3. Poco CEO: verify no write path, no duplicate/mixed card, correct animal count, shed, vaccine
   labels, progress, and completion.
4. Admin web: verify Calendar and live tracker on the real routes and fail on `backend_down`,
   `Admin-web contract unavailable`, `The board could not be loaded`, or `Weights could not be loaded`.
5. Read back DB membership, completions, events, proof artifact state, and adjacent assignments.
6. Visually inspect every screenshot before presenting it as evidence.

## Promotion Rule

Do not push to main, merge, or deploy from a patch, focused unit tests, static judge review, or a
Cloud Build result alone. Promotion requires the repository-local landing receipt after the final
edit and final rebase. Deployment requires exact revision readback and real DB/web/mobile smoke.
