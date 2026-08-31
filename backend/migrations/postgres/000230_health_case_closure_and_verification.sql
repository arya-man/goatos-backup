-- +goose Up
-- Health: clinical case closure + treatment-evidence verification stamps
-- (2026-08-29 health completeness audit, items 1 and 2; renumbered 000222 -> 000224 on the health-v1 rebase).
--
-- 1) CLOSURE. health_cases has allowed 'recovered' / 'continued' / 'referred' / 'canceled' since
--    000098, but no write path ever produced them: the only way a case left 'active' was the
--    animal dying, so the open-case count grew monotonically and ErrProtocolInUse became
--    permanently sticky for any disease ever diagnosed. POST /app/health/cases/{id}/close
--    (permission health.diagnose) now records the clinical outcome. Closing a case cancels its
--    remaining unworked sessions, which needs a session state that is "stopped by a clinical
--    decision", distinct from 'canceled_death' (stopped because the animal died) -- hence the new
--    'canceled' session status. Sessions already completed keep their history untouched.
--
-- 2) VERIFICATION. Treatment completions now enqueue a verification item (categories
--    health_adults / health_kids, registered in verificationcatalog since 000098-era wiring but
--    never produced). The verifier's approve stamps verified_by/verified_at on the session; a
--    reject flips it to the existing 'rework' status (which previously had no writer) so the
--    operator re-does and re-films the session. No completion gate: the treatment was already
--    given, so this is post-task evidence review (the shifting model), never a rollback.

ALTER TABLE public.health_treatment_sessions
  DROP CONSTRAINT health_treatment_sessions_status_check;
ALTER TABLE public.health_treatment_sessions
  ADD CONSTRAINT health_treatment_sessions_status_check
  CHECK (status IN ('scheduled','due','in_progress','completed','rework','held_death_review','canceled_death','canceled'))
  NOT VALID;
ALTER TABLE public.health_treatment_sessions
  VALIDATE CONSTRAINT health_treatment_sessions_status_check;

ALTER TABLE public.health_treatment_sessions
  ADD COLUMN verified_by uuid,
  ADD COLUMN verified_at timestamptz;

ALTER TABLE public.health_cases
  ADD COLUMN closed_by uuid,
  ADD COLUMN closed_at timestamptz,
  ADD COLUMN close_note text,
  ADD COLUMN closure_idempotency_key text,
  ADD COLUMN closure_fingerprint text;

CREATE UNIQUE INDEX health_cases_closure_idempotency_uq
  ON public.health_cases (tenant_id, closure_idempotency_key)
  WHERE closure_idempotency_key IS NOT NULL;

-- +goose Down
-- Forward-only operational module. Dropping closure/verification history is unsafe.
SELECT 1;
