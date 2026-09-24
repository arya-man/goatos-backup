-- +goose Up
-- seed-fixture-guard:ignore: Health treatment step proof tables are runtime evidence rows, not vaccination HRMS seed-source tables.
-- ONE VIDEO PER TREATMENT STEP (maintainer decision 2026-09-23).
--
-- A treatment session carried ONE proof: `health_treatment_sessions.proof_ref`, a single video for
-- the whole morning card. On the day this was written the Fever card's day-1 morning held TWELVE
-- steps and SIX separate injections. Nobody films twelve steps in one continuous take while
-- handling an animal, and a verifier watching that clip cannot tell whether all six injections
-- were given -- so the proof could not prove the thing it exists for.
--
-- Each step now carries its own video. The session is submitted ONCE, when every step has one, and
-- the whole set reaches the verifier as ONE item (morning session, tag + disease) whose media she
-- steps through one clip at a time. The grain of the REVIEW is unchanged; only the evidence inside
-- it got finer.
--
-- CAPTURED_BY IS PER STEP, and that is the point of the row rather than a column on the step: the
-- maintainer's own case is that DIFFERENT PEOPLE do different steps of one session. A single
-- proof_ref on the session could only ever record one of them.
--
-- The session's own `proof_ref` is KEPT and is not written by this path. An older APK still
-- completes with one session video and is still reviewed; nothing in flight breaks.
BEGIN;

CREATE TABLE IF NOT EXISTS public.health_session_step_proofs (
  tenant_id uuid NOT NULL,
  health_session_id uuid NOT NULL,
  health_session_step_id uuid NOT NULL,
  proof_ref text NOT NULL,
  captured_by uuid NOT NULL,
  captured_at timestamp with time zone NOT NULL DEFAULT now(),
  idempotency_key text NOT NULL,
  created_at timestamp with time zone NOT NULL DEFAULT now(),
  updated_at timestamp with time zone NOT NULL DEFAULT now(),

  -- ONE proof per step. A re-shoot REPLACES it rather than appending, so the set the verifier
  -- receives is always exactly one clip per step and never a pile of attempts.
  CONSTRAINT health_session_step_proofs_pk
    PRIMARY KEY (tenant_id, health_session_step_id),
  CONSTRAINT health_session_step_proofs_ref_check
    CHECK (btrim(proof_ref) <> ''),
  CONSTRAINT health_session_step_proofs_step_fk
    FOREIGN KEY (health_session_step_id)
    REFERENCES public.health_session_steps (health_session_step_id)
    ON DELETE CASCADE
);

-- The read that matters is "every proof for this session, in step order", run on every card open
-- and again at submit.
CREATE INDEX IF NOT EXISTS health_session_step_proofs_session_idx
  ON public.health_session_step_proofs (tenant_id, health_session_id);

COMMENT ON TABLE public.health_session_step_proofs IS
  'One live-camera video per treatment step. Submitted once per session; reviewed as one verifier item carrying every clip.';
COMMENT ON COLUMN public.health_session_step_proofs.captured_by IS
  'WHO filmed this step. Per step because different people may do different steps of one session.';

CREATE TABLE IF NOT EXISTS public.health_session_step_proof_attempts (
  tenant_id uuid NOT NULL,
  health_session_id uuid NOT NULL,
  health_session_step_id uuid NOT NULL,
  idempotency_key text NOT NULL,
  request_fingerprint text NOT NULL,
  proof_ref text NOT NULL,
  captured_by uuid NOT NULL,
  captured_at timestamp with time zone NOT NULL DEFAULT now(),

  -- Durable retry memory. A later re-shoot replaces the live row above, but an old transport retry
  -- replays this attempt instead of restoring the old clip.
  CONSTRAINT health_session_step_proof_attempts_pk
    PRIMARY KEY (tenant_id, health_session_step_id, idempotency_key),
  CONSTRAINT health_session_step_proof_attempts_ref_check
    CHECK (btrim(proof_ref) <> ''),
  CONSTRAINT health_session_step_proof_attempts_step_fk
    FOREIGN KEY (health_session_step_id)
    REFERENCES public.health_session_steps (health_session_step_id)
    ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS health_session_step_proof_attempts_session_idx
  ON public.health_session_step_proof_attempts (tenant_id, health_session_id);

COMMENT ON TABLE public.health_session_step_proof_attempts IS
  'Idempotency ledger for treatment step proof writes. Keeps stale retries from restoring an older clip after a re-shoot.';

COMMIT;

-- +goose Down
BEGIN;
DROP INDEX IF EXISTS public.health_session_step_proof_attempts_session_idx;
DROP TABLE IF EXISTS public.health_session_step_proof_attempts;
DROP INDEX IF EXISTS public.health_session_step_proofs_session_idx;
DROP TABLE IF EXISTS public.health_session_step_proofs;
COMMIT;
