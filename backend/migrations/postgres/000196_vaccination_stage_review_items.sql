-- +goose Up
-- Durable, goat-scoped, ACTIONABLE review items for vaccination stage/age conflicts (VACC-REV-10).
-- When a goat past the 20-week kid cutoff still carries a K1/K2 management-stage tag, generation
-- routes it to the adult path (never kid vaccines) and records ONE open review item here so an
-- operator can discover, and later RESOLVE, exactly which animals need their stale tag reconciled.
-- The lifecycle is open -> resolved. Dedup is scoped to OPEN items only (partial unique index), so a
-- recurrence AFTER a resolution opens a NEW actionable occurrence rather than being permanently
-- swallowed by the idempotency key.
CREATE TABLE vaccination_stage_review_items (
  review_item_id     uuid PRIMARY KEY,
  tenant_id          uuid NOT NULL,
  goat_id            uuid NOT NULL,
  reason             text NOT NULL,
  observed_stage     text NOT NULL,
  observed_age_weeks integer NOT NULL,
  status             text NOT NULL DEFAULT 'open',
  resolved_by        uuid,
  resolved_at        timestamptz,
  resolution_note    text,
  idempotency_key    text NOT NULL,
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT vaccination_stage_review_items_status_check CHECK (status IN ('open', 'resolved'))
);

-- Idempotent only while OPEN: repeated generation passes never duplicate an open item, but a new
-- occurrence after resolution is allowed.
CREATE UNIQUE INDEX vaccination_stage_review_items_open_idem_unique
  ON vaccination_stage_review_items (tenant_id, idempotency_key)
  WHERE status = 'open';

-- Operator listing: open items per tenant, newest first.
CREATE INDEX vaccination_stage_review_items_tenant_status_idx
  ON vaccination_stage_review_items (tenant_id, status, created_at DESC);

-- +goose Down
DROP TABLE vaccination_stage_review_items;
