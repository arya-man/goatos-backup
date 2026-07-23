-- +goose Up
-- Re-scope proof artifacts from the old contract (scope_type='task', subject_type='shed')
-- to the new shed scope (scope_type='shed', scope_id=subject_id) to ensure that shed-level
-- shed-level proof videos are counted correctly by the shed readiness queries.
--
-- The old contract stored proof rows with:
--   scope_type='task' (parent sop_task_id) and subject_type='shed' (the shed_id)
-- The new contract scopes them directly:
--   scope_type='shed' and scope_id=subject_id (the shed_id)
--
-- This UPDATE is idempotent and lock-safe (no DDL, no constraints added). Only rows that need
-- re-scoping are updated (filtered by the WHERE clause); re-running is harmless.
--
-- seed-migration-guard:ignore owner=ravi issue=shed-proof-scope-review-followup reason=no-seed-impact: a fresh seed writes shed proofs at scope_type='shed' already (client sends shed scope), so this back-compat re-scope matches zero fresh-seeded rows; it only repairs pre-existing task-scoped shed proofs on already-populated envs expiry=2026-10-31

UPDATE public.proof_artifacts
   SET scope_type = 'shed',
       scope_id   = subject_id
 WHERE scope_type = 'task'
   AND subject_type = 'shed'
   AND subject_id IS NOT NULL
   AND scope_id <> subject_id;

-- +goose Down
-- Re-scoping from task scope to shed scope is not safely reversible because the original
-- task scope_id (sop_task_id) is not stored on the proof_artifacts row. A best-effort
-- restore would require joining through intermediate tables and would be error-prone.
-- This down is a no-op; existing shed-scoped proofs will remain and continue to be counted
-- correctly by the shed readiness logic.
