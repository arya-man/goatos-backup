-- +goose Up
-- One immutable delivery fact row per child. A twin/triplet is still created as an independent
-- canonical goat; every sibling row repeats the delivery's litter_size and points to the same
-- canonical mother goat. The operator-scanned RFID is deliberately not copied here.
--
-- Initial-seed coupling: goat_births is operational birth metadata written only by the identity
-- create transaction. Existing birth-origin goats cannot be backfilled safely because the legacy
-- records did not capture a canonical mother or litter size, so this additive table starts empty.
CREATE TABLE public.goat_births (
  tenant_id uuid NOT NULL,
  child_goat_id uuid NOT NULL,
  mother_goat_id uuid NOT NULL,
  litter_size smallint NOT NULL,
  created_at timestamp with time zone DEFAULT now() NOT NULL,
  CONSTRAINT goat_births_pkey PRIMARY KEY (child_goat_id),
  CONSTRAINT goat_births_tenant_child_unique UNIQUE (tenant_id, child_goat_id),
  CONSTRAINT goat_births_litter_size_check CHECK (litter_size IN (1, 2, 3)),
  CONSTRAINT goat_births_distinct_goats_check CHECK (child_goat_id <> mother_goat_id),
  CONSTRAINT goat_births_child_tenant_fk FOREIGN KEY (tenant_id, child_goat_id)
    REFERENCES public.goats (tenant_id, goat_id),
  CONSTRAINT goat_births_mother_tenant_fk FOREIGN KEY (tenant_id, mother_goat_id)
    REFERENCES public.goats (tenant_id, goat_id)
);

-- Mother-history and sibling reads are exact, tenant-scoped index lookups.
CREATE INDEX goat_births_mother_idx
  ON public.goat_births (tenant_id, mother_goat_id, child_goat_id);

-- +goose Down
DROP TABLE IF EXISTS public.goat_births;
