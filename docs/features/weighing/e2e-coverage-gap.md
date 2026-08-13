# Weighing E2E Coverage Gap (honest status)

Status: **Weighing has NO end-to-end proof of the capture -> proof-upload ->
verification-queue -> verdict -> completion path.** This document records that
gap explicitly so a later author knows exactly what is missing and what to
build. This is a documentation-only closure of DO-NOT-MERGE blocker 7
("Weighing E2E lane honesty") — it does not add the missing E2E test itself.
That remains open follow-up work.

## What actually exists today

- `fixtures/weighing-seed-2026-07-29/weighing-seed.json` — a hand-authored
  seed scenario (roles, campaign, selected sheds/partitions, animals,
  proof/outbox cases, mobile route expectations, scale profile).
- `fixtures/weighing-seed-2026-07-29/weighing-seed-validation.test.mjs` and
  `tools/dev/validate-weighing-fixture.mjs` — a schema/shape validator
  that checks the fixture JSON itself has the required coverage fields. It
  does not call any backend API, mobile code, or durable consumer.
- `backend/cmd/seed-weighing-fixture` — an idempotent local DB importer that can
  materialize the fixture into a local Postgres database for manual/dev use.
  Importing data is not the same as proving the production write/read paths
  that operate on that data behave correctly end to end.
- `backend/internal/weighing/app/service_test.go` — a Go service-level
  regression test that drives create/publish/execute/RBAC/idempotency/category
  calls through the real service layer. This is real production-path proof at
  the Go service-call layer, but it is not an end-to-end test: it does not go
  through the HTTP API, the mobile app, Room/outbox, RFID capture, or any
  durable event consumer/sweeper.

Per `grep -rn "weighing" backend/tests/e2e` there are **zero** weighing E2E Go
tests in this repo, and no `story_*_weighing_*_test.go` file exists anywhere.
`make check-e2e-kernel-integrity` currently passes only because there is
nothing labeled "weighing E2E" for it to catch — this is a vacuous pass, not a
real one.

## Production paths that remain unproven end to end

None of the following are exercised together, start to finish, by any test in
this repo:

1. **Mobile capture** — an operator scanning an RFID/tag on the Weighing
   Android flow and recording a weight/observation locally (Room + outbox),
   including the free-flow rules in
   `fixtures/weighing-seed-2026-07-29/README.md` ("Weighing V1 Review
   Boundaries": no `matchTag()` requirement, no expected-animal roster,
   duplicate scanned identifiers allowed across shed buckets).
2. **Proof upload** — the per-animal or per-shed/partition proof artifact
   actually being uploaded through the real proof/media pipeline (retry on
   failure, removable uploaded-but-unsubmitted proof) rather than being
   asserted as JSON in a fixture.
3. **Verification queue** — a submitted Weighing observation/category
   actually landing in the real verification queue/read model that a
   verifier UI or API would show, not a fixture-declared expectation.
4. **Verdict** — an approve/rework verdict on a Weighing verification item
   actually being applied by the real verdict-processing code path
   (`backend/internal/weighing/adapters/postgres/verification_verdict.go` and
   whatever service/consumer owns it), including idempotency/replay behavior
   under concurrent or duplicate verdicts.
5. **Completion** — the resulting completion/observation state actually
   being durable, idempotent under duplicate-scan replay, and visible through
   whatever canonical read path (mobile, admin-web if enabled, or API) is
   supposed to show it as done — end to end from the original scan.
6. **Roll-forward/delay handling** — the "delayed roll-forward beyond the
   campaign week" scenario the fixture *declares* as covered is only checked
   as a JSON shape today; no test drives an actual delayed submission through
   the real scheduling/roll-forward code.
7. **Scale path** — the fixture's "5k+ scale query path expectations for
   paged details and indexed RFID lookup" are asserted as fixture metadata,
   not exercised against a real 5k+ row database with query-plan proof.

## What a real Weighing E2E test needs to drive

A future E2E story for Weighing must, at minimum:

- Start from the same class of seed facts this fixture already describes
  (tenant, herd animal/RFID, location, workforce, campaign/selected
  shed-partition config) — seeding *inputs* is fine.
- Then drive the actual production code path for each of the six items above:
  real HTTP API calls (or the real mobile app driving those APIs), the real
  proof/media upload pipeline, the real verification-queue read model, the
  real verdict-processing service/consumer, and the real completion/read path
  — not a second seeded JSON readback standing in for any of them.
- Assert on canonical-read output (the same query path production screens
  use), not on fixture-internal state.
- Follow the AGENTS.md E2E rule: "Obligations, batches, completions,
  verification outcomes, notifications must be produced by the same service,
  API, durable event consumer, sweeper, or canonical-read query path used in
  production."

## What this pass did NOT do

- Did not write the real E2E test described above. That is explicitly out of
  scope for this pass (see `context/repo-audits/weighing-phase1-2-do-not-merge-blockers.md`,
  blocker 7 annotation).
- Did not change `backend/cmd/seed-weighing-fixture/main.go`,
  `backend/cmd/seed-weighing-fixture/main_test.go`, or
  `tools/dev/validate-weighing-fixture.mjs` — those files still reference
  the old `fixtures/weighing-e2e-2026-07-29/...` path and/or still carry "e2e"
  in their own filename/identifiers. See the blocker-doc annotation for the
  exact file:line references that still need a follow-up edit outside this
  pass's scope lock.
