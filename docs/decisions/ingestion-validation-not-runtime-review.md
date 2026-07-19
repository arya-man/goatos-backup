# ADR: Ingestion Validation Not Runtime Review/Repair

Status: accepted

Date: 2026-07-19

Decision owner: Goat OS product owner (data integrity and seeding policy)

## Decision

A data-integrity contradiction — a state that is impossible when clean ingestion feeds the system — **must be caught and REJECTED at the INGESTION boundary** with the offending rows highlighted to the operator, and **never persisted**. Building a runtime "review/reconcile/repair" screen or queue to cope with such dirty data is a banned anti-pattern. It treats a seeding or classifier bug as a product feature.

## The Core Principle

Data-integrity contradictions fall into two classes:

1. **Impossible-with-clean-data contradictions** — a state that CANNOT occur if the source data is valid and the classification logic is correct. Examples:
   - An animal classified as `stage='kid'` but with `age_days >= 182` (past the kid cutoff).
   - An animal with `species='goat'` but bearing a vaccination row tagged for `species='sheep'`.
   - An animal with `location_id` pointing to a location in park A, but `shed_id` pointing to a shed in park B.

2. **Legitimately-rare states** — a state that is theoretically possible but extremely rare or indicates a genuine business exception (e.g., an animal with a missed vaccination after recovery from clinical deferral, or a legitimate supply disruption). These **do** get runtime handling: a screen, workflow, or operator queue.

The rule targets the first class ONLY. Contradictions in the second class remain operator-facing features.

## The Example: Stage and Age

The canonical example is the `stage` / `age` contradiction.

**Source of truth:** an animal's age is computed from its birth date and the current business date (India timezone). **Stage is a pure function of age:**
- `stage = 'kid'` when `age_days < 182`.
- `stage = 'adult'` when `age_days >= 182`.

An animal with `stage = 'kid'` but `age_days >= 182` is a logical impossibility if the birth date is correct.

**How it happens (the mistake):**

1. **Seed or bulk import** reads a source record with wrong/stale birth date or misclassified source stage tag.
2. **Bad seed logic** hand-fills `stage` column without deriving it from the age; stores the contradiction.
3. **Live system** sees the contradiction and can no longer make correct inferences (is the animal actually a kid? Is the birth date wrong?).
4. **Downstream:** operators create a runtime "stage review" queue to manually fix these rows. This queue becomes a regular operational task. It never goes away because the seed pipeline keeps ingesting bad data instead of rejecting it.

**Why this is wrong:** The queue is not a feature; it is a patch over dirty data. The correct fix is at the ingestion validator: derive stage from age, reject or fail-with-report if there is a contradiction.

## What This Requires

At every ingestion point — seed, sheet/bulk import, individual create API, procurement intake — implement:

1. **Validate-and-Reject Logic:**
   - Identify the source-of-truth field (birth date ⇒ age, location ⇒ park/shed).
   - Derive the dependent field from it (age ⇒ stage).
   - Compare the derived value against the input. If they contradict:
     - **Reject the row** with a clear error message naming the field, the expected value, and the actual value.
     - **Fail-with-report:** highlight all offending rows to the operator upfront. Do not ingest some rows and leave others for a runtime queue.

2. **No Runtime Review Queue:**
   - Do NOT create a `<entity>_review_item` / `*_review` table for operator triage.
   - Do NOT add an admin screen `/admin/reviews` for a validator bug.
   - Do NOT add a sweeper that marks certain rows for manual inspection.
   - Any such queue that exists SOLELY because ingestion ingests dirty data is a code smell and a maintainer violation.

3. **Proof and Tests:**
   - Add an integration test that verifies out-of-range / contradictory input is rejected at the ingestion boundary (not silently accepted and stored).
   - In E2E, never seed rows that would fail the validator — use clean source data.
   - If an import tool has a "reconcile stale rows" mode, that is a separate reconciliation SERVICE (not a queue), used sparingly for known schema migrations or legacy system cutover, and audited like any production operation.

## Reinforces Existing Rules

This decision reinforces the `AGENTS.md` seeds principle: "classify first from independent evidence, then persist the source date as the selected rule family's history anchor" (see `docs/runbooks/vaccination-seed-source-date-contract.md`).

It also reinforces the backend review rule "Config validation — expose or enforce, never ignore" (`.agents/skills/goatos-code-review/references/backend.md`): a field exposed in the admin UI/API must be either enforced by the business logic or rejected if out of range. Authored config and ingested data follow the same validate-or-reject pattern.

## Known Violations (Historical Context)

The **stage/age review queue** (previously `RecordStageReviewItem` / `stage_review_item` tables and operations) was the direct anti-pattern this ADR bans. It existed because:
- Seed logic ingested animals with misclassified source stage tags.
- The generator did not validate stage against age.
- An operator workflow was built to "fix" the rows after the fact.

The correct fix was to reject the misclassified source at seed time and alert the operator during import, not to persevere them and build a runtime queue.

This ADR ensures the pattern is not reintroduced for stage/age or any other impossible-with-clean-data contradiction.

## Anti-Patterns Blocked by This Rule

- `RecordStageReviewItem`, `stage_review_item`, or equivalent `*_review_item` tables created to hold ingestion-validation failures.
- An endpoint like `POST /admin/reviews/{review_id}/resolve` that allows an operator to override or correct a row that failed ingestion validation.
- A sweeper that marks rows for "review" based on a contradiction that the ingestion validator should have caught.
- A "reconcile" screen in the admin UI for a problem that should be rejected at import time.

**Exception:** A reconciliation SERVICE (e.g., a tool to reconcile legacy system data during a controlled cutover) is not a ban violation — it is a separate operational task with clear scope, ownership, and sunset date. It is NOT a standing queue for ingestion failures.

## References

- `docs/runbooks/vaccination-seed-source-date-contract.md` — seed classification and source-date contract.
- `.agents/skills/goatos-code-review/references/backend.md` — config validation rule and ingestion validation checkpoint.
- `AGENTS.md` — "classify first from independent evidence" principle.
- Machine guard: `tools/agent-hooks/check-no-mismatch-review-queue.mjs` — flags new review/reconcile queues for impossible-state contradictions.
