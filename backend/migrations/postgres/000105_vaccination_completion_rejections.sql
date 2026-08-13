-- +goose Up
-- +goose NO TRANSACTION

-- Rejected vaccination completions move OUT of vaccination_completions and into this table.
--
-- Why a move rather than a status flag (maintainer decision 2026-08-04): proof is captured one
-- video per animal, so a verifier now rejects ONE animal while the rest of the shed stands. The
-- rejected animal must become outstanding work again -- it has to reappear on the operator's scan
-- screen as something to redo. Leaving a 'rejected' row in vaccination_completions kept the animal
-- looking handled on every read that only asks "is there a completion for this obligation", which
-- is exactly why a rejected animal stayed green on the scan screen. Removing the row makes the
-- animal outstanding by DEFAULT, everywhere, without each read having to remember to special-case
-- a rejected status.
--
-- Nothing is destroyed. The row is copied here first, in the same transaction as the delete, with
-- the verdict that caused it. This table is the clinical record of "we injected this animal and
-- the proof was refused" -- which is a different fact from "this never happened", and the two must
-- never be confused. Every column of the original row is preserved so the completion can be
-- reconstructed exactly, and rejected_at/rejected_by/rejection_reason record the verdict itself.
CREATE TABLE IF NOT EXISTS public.vaccination_completion_rejections (
  rejection_id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  -- The ORIGINAL completion_id, preserved. Not a foreign key: the row it pointed at is gone by
  -- design, and an FK would make the move impossible.
  completion_id             uuid NOT NULL,
  tenant_id                 uuid NOT NULL,
  obligation_id             uuid NOT NULL,
  batch_id                  uuid,
  goat_id                   uuid NOT NULL,
  sop_submission_item_id    uuid,
  vaccine_inventory_lot_id  uuid,
  doses                     integer,
  dose_ml_given             numeric,
  route_site                text,
  adverse_reaction          boolean NOT NULL DEFAULT false,
  adverse_reaction_problem_id uuid,
  cold_chain_verified       boolean NOT NULL DEFAULT false,
  administered_at           timestamptz,
  -- The status the row carried when it was moved, kept verbatim for reconstruction.
  original_status           text NOT NULL,
  verified_by               uuid,
  verified_at               timestamptz,
  rejection_reason          text,
  withdrawal_until_date     date,
  recorded_by               uuid,
  original_idempotency_key  text NOT NULL,
  original_row_version      integer NOT NULL,
  original_created_at       timestamptz NOT NULL,
  original_updated_at       timestamptz NOT NULL,
  rejected_at               timestamptz NOT NULL DEFAULT now(),
  rejected_by               uuid,
  created_at                timestamptz NOT NULL DEFAULT now()
);

-- Replay safety. A redelivered verdict must not archive the same completion twice, and the move
-- is the ONLY writer here. With the completion row already gone, a replayed reject finds nothing
-- to move; this constraint is the second line of defence if it ever does.
CREATE UNIQUE INDEX IF NOT EXISTS vaccination_completion_rejections_completion_uidx
  ON public.vaccination_completion_rejections (tenant_id, completion_id);

-- The read path that matters: "does this animal/obligation have a rejection?" -- the shed card's
-- Sent-back state and the rejected count are derived from it now that no rejected row survives in
-- vaccination_completions.
CREATE INDEX IF NOT EXISTS vaccination_completion_rejections_obligation_idx
  ON public.vaccination_completion_rejections (tenant_id, obligation_id, rejected_at DESC);

CREATE INDEX IF NOT EXISTS vaccination_completion_rejections_goat_idx
  ON public.vaccination_completion_rejections (tenant_id, goat_id, rejected_at DESC);

-- +goose Down
DROP INDEX IF EXISTS vaccination_completion_rejections_goat_idx;
DROP INDEX IF EXISTS vaccination_completion_rejections_obligation_idx;
DROP INDEX IF EXISTS vaccination_completion_rejections_completion_uidx;
DROP TABLE IF EXISTS public.vaccination_completion_rejections;
