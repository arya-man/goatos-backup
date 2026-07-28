// Package tasks is the birth/death follow-up workflow engine (maintainer decision 2026-07-27;
// canonical source docs/decisions/birth-death-workflows.md).
//
// A birth applying (goat.created with origin_type=birth) opens its workflow at approval. A death
// report opens its workflow immediately while the goat is still alive, so the operator can upload
// both mandatory videos before the existing admin approval. Admin acceptance applies the death and
// releases the proof pair to Verify; rejection cancels the staged workflow without changing counts.
// Each per-goat workflow is stamped from a CODE-DEFINED template (domain.TemplateBirthKid /
// TemplateBirthMother / TemplateDeath) and is the outstanding SOP work list the mobile
// /counts/birth and /counts/death modules open on.
//
// Layout (hexagonal):
//
//	domain                     — templates, action state machine, card recompute, cursors (pure)
//	ports                      — the storage seam
//	app                        — list/detail/answer/complete services + the event consumers
//	adapters/postgres          — workflow_instances / workflow_actions persistence (migration 000034)
//	adapters/http              — GET/POST /app/workflows* (CountsWrite)
//	adapters/verificationbridge — death evidence -> generic verification enqueue (death_evidence)
//
// The death trail hands BOTH mandatory videos to the generic Verification module as ONE
// death_evidence item; the verifier's approve/rework verdict routes back through the
// verification.verdict.* consumers (registered once in internal/eventwiring).
package tasks
