# ADR: Vaccination Shed Completion is an Acknowledgement, Not a Manual Medical Form

Status: accepted

Date: 2026-07-20

Decision owner: Goat OS product owner (vaccination workflow and clinical clarity)

## Decision

Vaccination shed completion is a final **acknowledgement** that every expected animal in a shed has been scanned and has proof media ready, NOT a manual medical form where operators fill in batch numbers, cold-chain verification, dose amounts, route/site, or adverse reaction data. The operator already does the real work at animal level: scans each goat's RFID and attaches one live camera proof clip per goat. Shed completion adds nothing clinically — it is administrative closure.

### Banned Field Keys (Do Not Reintroduce)

The following field keys are **permanently removed** from the vaccination SOP form_dsl and must never be reintroduced as fillable answers:

- `vaccine_lot_id` (vaccine batch picker)
- `cold_chain_verified` (boolean, used to block submission)
- `dose_ml_given` (numeric, operator-entered dose)
- `doses` (if it was a manual-entry field, distinct from obligation-level dose counts)
- `route_site` (dropdown for route/administration site)
- `administered_at` (date-time, now server-derived from submit time, never an operator answer)
- `adverse_reaction` (boolean, part of a manual reaction report form)
- `adverse_reaction_notes` (text, paired with adverse_reaction)

Related proof-capture keys that were already stripped in migration 000006 and remain gone:

- `shed_video`, `vial_lot_video`, `administration_video`, `extra_video_*` (batch-level proof videos)

## The Core Principle

A vaccination dose is a clinical event. Clinical data — what vaccine, what dose, what route, what time, what adverse outcome — must be **captured at the moment of administration** at the animal's side, not filled in after the fact by someone reviewing a list of animals. The operator captures this at animal level through:

1. **Scan** — identify the animal (RFID or old tag)
2. **Proof** — one live video clip from the operator's phone, recorded during or immediately after the event, showing the injection or delivery

Shed completion is NOT a recap form; it is a readiness checkpoint that confirms: "Every expected animal in this shed has been scanned and proofed. The shed is done."

Derived values like `administered_at` (the submit time, when the operator hit 'complete') and completion-level status (`dose_ml_given=NULL`, `route_site=NULL`, `adverse_reaction=false`, `cold_chain_verified` no longer gates anything) are **always server-side**, never from an operator answer.

## What Changed

### SOP Form (form_dsl)

**Before:** vaccination.drive and vaccination.session SOP versions included fields for vaccine_lot_id, cold_chain_verified, dose_ml_given, route_site, administered_at, adverse_reaction, adverse_reaction_notes. Submission was blocked if cold_chain_verified was false. Adverse reaction notes were required if adverse_reaction was true.

**After:** only `goat_ids` (per-animal scans) + per-goat `proof_policy` (proof required). No manual medical fields. Submission is enabled only when all expected animals are scanned AND all have proof.

### Submission Validation (Backend)

**Before:** SOP submission accepted answers for vaccine lot, cold chain, dose, route, time, adverse reaction from the form. These were used to populate vaccination_completions columns directly.

**After:** SOP submission accepts only the per-animal scan roster (already merged from draft captures) and proof references. Every field in vaccination_completions is either:
- **Derived server-side:** `administered_at` = submit/scan time, `status` = transaction status, `batch_id` = from the obligation, etc.
- **Defaulted/immutable:** `dose_ml_given` = NULL, `route_site` = NULL, `adverse_reaction` = false, `cold_chain_verified` left as DEFAULT false (no longer gates submission).

### Read API (ShedCompletionSummary)

A new read contract `GET /app/tasks/{task_id}/shed-completion-summary` surfaces:
- `shed_name` — human name (never raw UUID)
- `drive_name` — human drive label
- `expected_count` — animals expected in this shed for the drive
- `handled_count` — animals scanned
- `proof_ready_count` — animals with >=1 ready proof clip
- `vaccine_breakdown` — [{vaccine name, count}] for display
- `submit_enabled` — true only when handled_count == expected_count AND proof_ready_count == expected_count
- `blocking_reason` — human-readable reason if submit is not enabled, null otherwise
- `submit_state` — one of draft/submitted/verified/closed

The UI screens (Android, admin-web) render this summary + a Submit button. No form fields. When form_dsl has no fields, the UI renders only the summary.

### Adverse Reaction Handling (Unchanged)

Adverse reactions are NOT part of vaccination shed completion. If an operator observes an adverse event during vaccination, they file a separate **Problem Report** (the existing health/diagnosis workflow). Vaccination remains a simple event-capture: scan + proof. Problems are logged separately and tracked through the health SOP.

## Reinforces Existing Rules

This decision reinforces:

- **Proof is truth** — a live video clip from the operator is the only clinical record. Written answers after the fact are not clinical evidence.
- **Immutability** — vaccination data must not be editable after submission. Corrections require a new obligation/event, not a retry or update of the original completion.
- **Kernel correctness** — booster scheduling, protocol adherence, and gap analysis all depend on `administered_at` accuracy. Deriving it from submit time (when the operator finishes the animal batch) is the only non-lossy approach.

## Anti-Patterns Blocked by This Rule

- A "vaccine batch picker" field in the vaccination SOP that defaults to the reserved lot. The lot is implicit from the batch; it is never a form answer.
- A "cold chain verified" checkbox that the operator must click to unblock submission. Either collect proof that demonstrates it (a thermometer reading video, for example) or stop asking.
- A "dose given" field pre-filled with the protocol dose amount. The operator is administering a dose they already prepared; the actual amount is derived from stock consumption, not from a re-entry field.
- A "route / site" selector that defaults to "subcutaneous". Route is part of the protocol, not a per-dose operator decision. Clinical exceptions (allergic to subcutaneous, wound at site) are handled as problem reports.
- An "adverse reaction observed" checkbox. Clinical adverse events go through the problem-report path, not a form field on a vaccination task.
- A shed-level video field or vial-lot video field. Proof is per-animal, captured during administration, not a summary video after the fact.

## Migration Path

**Migration 000007** strips these fields from the vaccination.drive and vaccination.session SOP form_dsl and relaxes the vaccination_completions columns' NOT NULL constraints that depended on filled answers. The migration is:
- **Idempotent:** guarded on the presence of 'cold_chain_verified' in the SOP version's fields, so a clean install or a prior run is a no-op.
- **Lock-safe:** drops NOT NULL (catalog-only change), does not scan or lock the table.
- **Revertible:** the Down script re-adds the fields and constraints (lossy on answer data, but schema-safe).

**Baseline (000001) tail rewrite** also strips the same fields from the vaccination SOP initial form_dsl so a fresh clean install seeds the correct minimal form.

## Known Violations (Historical Context)

Early vaccination SOP implementations (prototypes and initial phases) included shed-level manual fields for batch, cold chain, dose, and route. These were attempts to create an "all-in-one" form where the operator recap the shed's work after the fact. This violated the principle: **proof captured during an event is the only clinical record.** The operator already captured per-animal proof; recapping it on a shed recap form is ceremony, not evidence.

This ADR ensures the pattern is not reintroduced and codifies the correct model: scan + proof at animal level, acknowledgement at shed level.

## CI Guard

**Guard script:** `tools/agent-hooks/check-no-vaccination-shed-form-fields.mjs` (or .sh)

The guard FAILS if any of the banned field keys (`vaccine_lot_id`, `cold_chain_verified`, `dose_ml_given`, `route_site`, `administered_at`, `adverse_reaction`, `adverse_reaction_notes`, `shed_video`, `vial_lot_video`, `administration_video`, `extra_video_*`) reappears in:
- Migration files (*.sql) as a field being re-added to a vaccination SOP form_dsl.
- Seed files (backend/cmd/seed-*) as a field being seeded into a vaccination SOP form_dsl.
- Taxonomy or enum definitions (vaccination_route_sites, vaccination_form_fields, etc.) if they are explicitly for vaccination SOP manual collection.

The guard ignores:
- vaccination_completions column definitions (columns may stay as nullable or with defaults, they are server-side values now).
- Protocol matrix metadata (e.g., route_site in the vaccination matrix definition is clinical metadata, not a form field).
- Past records in the completion history (old rows with these columns are immutable; the guard checks only the SOP form_dsl and new seeds/migrations).

Wired into `make ci-local` as part of the vaccination check suite.

## References

- Migration 000007: `backend/migrations/postgres/000007_r50_vaccination_shed_ack_dml.sql`
- Backend contract: `backend/internal/vaccination/domain/completion.go` — ShedCompletionSummary
- E2E helpers: `backend/tests/e2e/vaccination_sop_helpers_test.go` — no manual fields in submission
- Mock: `mock/goatos-dashboard-mock.html` — per-animal scan + acknowledgement summary
- Runbook audit: `docs/runbooks/vaccination-role-e2e-audit-2026-07-20.md` — workflow proof
