-- +goose Up
-- Durable, goat-scoped review items for vaccination stage/age conflicts (VACC-REV-10). When a goat
-- past the 20-week kid cutoff still carries a K1/K2 management-stage tag, generation routes it to the
-- adult path (never kid vaccines) and records ONE identifiable review item here so an operator can
-- discover exactly which animals need their stale tag reconciled — a plain aggregate counter cannot.
-- Idempotent on (tenant_id, idempotency_key): repeated generation passes never duplicate the item.
CREATE TABLE vaccination_stage_review_items (
  review_item_id     uuid PRIMARY KEY,
  tenant_id          uuid NOT NULL,
  goat_id            uuid NOT NULL,
  reason             text NOT NULL,
  observed_stage     text NOT NULL,
  observed_age_weeks integer NOT NULL,
  idempotency_key    text NOT NULL,
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT vaccination_stage_review_items_idem_unique UNIQUE (tenant_id, idempotency_key)
);

CREATE INDEX vaccination_stage_review_items_tenant_goat_idx
  ON vaccination_stage_review_items (tenant_id, goat_id);

-- +goose Down
DROP TABLE vaccination_stage_review_items;
