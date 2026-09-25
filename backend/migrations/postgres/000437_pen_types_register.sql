-- +goose Up
-- seed-fixture-guard:ignore: turns an existing two-value CHECK into a tenant-authored register and
-- seeds each tenant with exactly the two values the CHECK allowed. No vaccination/HRMS seed
-- contract, source fixture schema or read-model table changes.
--
-- PEN TYPES ARE THE FARM'S OWN LIST (maintainer instruction 2026-09-25: "I want to add one more pen
-- type ... after park, before pens, we need pen type ... elevated, non-elevated, whatever we use in
-- weighing or anywhere in future should not be hard coded, it should come from here").
--
-- Until now the vocabulary was a CHECK constraint (elevated | non_elevated, migration 000391) and a
-- two-option list typed into the Configuration register, the Health analytics buckets and the
-- Weights chart. Adding a third kind of pen meant a migration and three code changes. It is now a
-- row in pen_types, edited on Configuration -> Items and settings -> Pen types, and the partition's
-- shed_type is a FOREIGN KEY into it, so a code nobody authored can never be stored.
--
-- The column keeps its name and its CODES. Every classified pen keeps exactly the value it had, and
-- both readers (weighing's Pen-wise comparison, Health's pen-type cut) keep grouping by the same
-- key; only the LABEL and the set of possible keys now come from this table.
--
-- The grain is unchanged: pen type is still set per PARTITION (maintainer, 2026-09-22, reaffirmed
-- 2026-09-25) -- Castro 1 and Castro 2 may be built differently.

CREATE TABLE IF NOT EXISTS public.pen_types (
  tenant_id uuid NOT NULL,
  pen_type_key text NOT NULL,
  name text NOT NULL,
  description text NOT NULL DEFAULT '',
  sort_order integer NOT NULL DEFAULT 100,
  status text NOT NULL DEFAULT 'active',
  row_version integer NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT pen_types_pkey PRIMARY KEY (tenant_id, pen_type_key),
  CONSTRAINT pen_types_status_chk CHECK (status IN ('active', 'archived')),
  CONSTRAINT pen_types_key_chk CHECK (pen_type_key ~ '^[a-z0-9][a-z0-9_]*$'),
  CONSTRAINT pen_types_name_chk CHECK (btrim(name) <> '')
);

-- Two pen types with one name would be two bars with one label on every chart.
CREATE UNIQUE INDEX IF NOT EXISTS pen_types_tenant_name_uidx
  ON public.pen_types (tenant_id, lower(btrim(name)));

-- Seed every tenant that exists or already has pens with the two kinds the CHECK allowed, so no
-- stored partition value is left without its row. The labels are the ones the charts already
-- showed ("Elevated pen" / "Non-elevated pen" read "Elevated" / "Non-elevated" on Configuration).
-- projection-review: membership=every tenant in tenants UNION every tenant with a partition row;
-- group_key=(tenant_id, pen_type_key), the primary key, and ON CONFLICT DO NOTHING keeps a re-run
-- and an already-authored row untouched; join_cardinality=the UNION is set-deduplicated and the
-- VALUES cross join is exactly two rows per tenant; pagination=NONE, one-shot over a handful of
-- tenants; scope=tenant_id is the first key of every row written.
INSERT INTO public.pen_types (tenant_id, pen_type_key, name, sort_order)
SELECT t.tenant_id, v.pen_type_key, v.name, v.sort_order
FROM (
  SELECT tenant_id FROM public.tenants
  UNION
  SELECT tenant_id FROM public.shed_partitions
) t
CROSS JOIN (VALUES ('elevated', 'Elevated', 10), ('non_elevated', 'Non-elevated', 20)) AS v(pen_type_key, name, sort_order)
ON CONFLICT (tenant_id, pen_type_key) DO NOTHING;

-- The closed list retires; the register is the list now.
ALTER TABLE public.shed_partitions
  DROP CONSTRAINT IF EXISTS shed_partitions_shed_type_check;

-- RESTRICT both ways: a pen type still assigned to a pen cannot be removed (archive it instead),
-- and a code is immutable once written, so a rename changes the NAME and never the key.
ALTER TABLE public.shed_partitions
  DROP CONSTRAINT IF EXISTS shed_partitions_pen_type_fk,
  ADD CONSTRAINT shed_partitions_pen_type_fk
    FOREIGN KEY (tenant_id, shed_type) REFERENCES public.pen_types (tenant_id, pen_type_key)
    ON UPDATE RESTRICT ON DELETE RESTRICT;

-- +goose Down
ALTER TABLE public.shed_partitions
  DROP CONSTRAINT IF EXISTS shed_partitions_pen_type_fk;

-- Going back to the closed list cannot keep a pen typed with a kind the farm added after this
-- migration: such a pen is left unclassified (NULL means "nobody has said") rather than folded into
-- one of the two old kinds. Recorded here because a reader of the Down path deserves to know it is
-- lossy.
UPDATE public.shed_partitions
SET shed_type = NULL, updated_at = now()
WHERE shed_type IS NOT NULL
  AND shed_type NOT IN ('elevated', 'non_elevated');

ALTER TABLE public.shed_partitions
  DROP CONSTRAINT IF EXISTS shed_partitions_shed_type_check,
  ADD CONSTRAINT shed_partitions_shed_type_check
    CHECK (shed_type IS NULL OR shed_type = ANY (ARRAY['elevated'::text, 'non_elevated'::text]));

DROP TABLE IF EXISTS public.pen_types;
