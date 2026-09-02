-- +goose Up
-- 000242_pen_reconciliation.sql
--
-- PEN RECONCILIATION (maintainer decision 2026-09-02): the herd register (DB) is TRUTH.
-- When an individual weighing bucket is submitted, every scanned tag that resolves to a live
-- animal whose registered pen differs from the pen it was weighed in raises ONE card in the
-- Herd Operations "Reconcile" tab. The operator physically returns the animal to its
-- registered pen, records a mandatory video, and submits; the video goes to the tenant
-- verifier (approve = completed, reject = rework). There is NO approver step, and completion
-- NEVER rewrites the register -- the card is closed by moving the animal, not the row.
--
-- One open card per animal ("one piece one card"): enforced by the partial unique index
-- below, which is also what makes the event-driven raise idempotent across bus deliveries.
--
-- Raised by the counts-side consumer of weighing.shed_submission.completed. Weighing itself
-- knows nothing about this table (weighing isolation is untouched; this is outward-only
-- consumption of weighing's durable event + source rows, the recorded kernel pattern).

CREATE TABLE IF NOT EXISTS pen_reconciliation_cards (
  card_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  goat_id uuid NOT NULL,

  -- The tag exactly as the weighing operator scanned it: what the reconcile operator will
  -- read off the animal's ear in the field.
  scanned_identifier text NOT NULL,

  -- WHERE THE ANIMAL WAS FOUND: the weighing bucket's pen, canonicalized at raise time to the
  -- physical shed + scrubbed partition (legacy alias location rows are resolved before
  -- comparison, so "Castro 1"-as-its-own-location never false-flags animals registered on
  -- Castro + partition 1). found_display_name is the operator-facing bucket label snapshotted
  -- from the weighing bucket, because that is the name the weigher actually stood in.
  found_location_id uuid NOT NULL,
  found_partition_label text NOT NULL DEFAULT '',
  found_display_name text NOT NULL,

  -- WHERE THE REGISTER SAYS THE ANIMAL LIVES (the pen to return it to), snapshotted at raise
  -- time from goat_shed_partitions (fallback goats.shed_id). Display is composed at read time
  -- via the canonical oploc helper, never stored.
  registered_shed_id uuid NOT NULL,
  registered_partition_label text NOT NULL DEFAULT '',

  park_id uuid,

  -- Provenance back to the weighing submit that raised the card.
  campaign_id uuid NOT NULL,
  campaign_shed_id uuid NOT NULL,

  raised_at timestamptz NOT NULL DEFAULT now(),

  -- open -> pending_verification -> completed; verifier reject sends pending_verification
  -- back to rework (operator re-shoots). No approver state exists on purpose.
  status text NOT NULL DEFAULT 'open',

  proof_ref text,
  completed_by uuid,
  completed_at timestamptz,
  verified_by uuid,
  verified_at timestamptz,
  rework_reason text,

  completion_idempotency_key text,
  completion_request_fingerprint text,

  row_version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT pen_reconciliation_cards_status_chk
    CHECK (status IN ('open', 'pending_verification', 'completed', 'rework')),
  CONSTRAINT pen_reconciliation_cards_tag_nonblank
    CHECK (btrim(scanned_identifier) <> ''),
  CONSTRAINT pen_reconciliation_cards_goat_fk
    FOREIGN KEY (tenant_id, goat_id)
    REFERENCES goats (tenant_id, goat_id)
    ON DELETE CASCADE
);

-- ONE OPEN CARD PER ANIMAL. Everything not completed is "open work" for this rule: an animal
-- already carrying an open/pending/rework card gains no second card from the next weighing.
CREATE UNIQUE INDEX IF NOT EXISTS pen_reconciliation_cards_one_open_goat_uidx
  ON pen_reconciliation_cards (tenant_id, goat_id)
  WHERE status <> 'completed';

-- Completion replay: the phone retries with the same Idempotency-Key; the key finds the
-- original write and echoes it instead of completing twice.
CREATE UNIQUE INDEX IF NOT EXISTS pen_reconciliation_cards_completion_idem_uidx
  ON pen_reconciliation_cards (tenant_id, completion_idempotency_key)
  WHERE completion_idempotency_key IS NOT NULL;

-- Keyset list order for the operator tab: newest raised first, id as the tiebreak, filtered
-- by status bucket.
CREATE INDEX IF NOT EXISTS pen_reconciliation_cards_actions_idx
  ON pen_reconciliation_cards (tenant_id, raised_at DESC, card_id DESC);

CREATE INDEX IF NOT EXISTS pen_reconciliation_cards_status_idx
  ON pen_reconciliation_cards (tenant_id, status, raised_at DESC, card_id DESC);

-- +goose Down
DROP TABLE IF EXISTS pen_reconciliation_cards;
