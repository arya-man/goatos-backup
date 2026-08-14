-- +goose Up
-- Pens get their own configured HOLDING CAPACITY.
--
-- Maintainer decision 2026-08-14: the Counts -> Sheds directory lists operational locations at PEN
-- grain, the way the farm's own Sheds DB sheet does ("Godel 1 - Part 1" ... "Part 10"), so the
-- capacity beside each row has to be that pen's capacity. Until now capacity existed only at
-- PHYSICAL SHED grain on shed_profiles.capacity, and a pen-grain screen reading it would either
-- repeat the shed's total on all ten of its pens (implying each pen holds 100) or show nothing.
--
-- WHY shed_partitions AND NOT shed_profiles. shed_partitions is the ORG-scoped catalog of pens that
-- exist, keyed (tenant_id, shed_id, normalized_label) -- it is the table the operational-location
-- convention names as the pen catalog and the one weighing is allowed to read. The alternative was
-- to hang capacity off the LEGACY partition-alias `locations` rows (the still-active rows literally
-- named "Castro 1" / "Godel 1 - Part 3"), which already have shed_profiles rows and would have
-- needed no migration at all. That was rejected deliberately: those rows are duplicates the whole
-- codebase spends effort excluding (oploc.PartitionAliasExclusionSQL), and hanging NEW configuration
-- off them would make them load-bearing exactly as the convention is retiring them.
--
-- NULLABLE, and null is not zero. A pen with no recorded capacity reads "Not recorded"; a pen
-- recorded as holding nothing reads 0. Collapsing the two would tell an operator something the
-- data does not say, so no DEFAULT is set and nothing is backfilled here -- backfilling from the
-- parent shed would divide a rollup by pen count and invent per-pen figures nobody measured.
-- backend/cmd/seed-shed-capacity loads the real values from the committed sheet capture.
--
-- shed_profiles.capacity KEEPS its meaning and is not deprecated: it is the capacity of a shed that
-- has no pens at all (Q1, Q2, Q3, Ho Chi Minh). The read model prefers the pen's own value and
-- falls back to the shed's only for an unpartitioned shed.
--
-- Additive column on a small catalog table (~120 rows). ADD COLUMN with no DEFAULT and no rewrite
-- takes only a brief ACCESS EXCLUSIVE lock to update the catalog, so no lock timeout dance is
-- needed at this size.
ALTER TABLE public.shed_partitions
    ADD COLUMN IF NOT EXISTS capacity integer;

ALTER TABLE public.shed_partitions
    DROP CONSTRAINT IF EXISTS shed_partitions_capacity_check;

ALTER TABLE public.shed_partitions
    ADD CONSTRAINT shed_partitions_capacity_check
    CHECK (capacity IS NULL OR capacity >= 0);

COMMENT ON COLUMN public.shed_partitions.capacity IS
    'Head count this pen is configured to hold. NULL means never recorded, which is a different fact from 0. Loaded by backend/cmd/seed-shed-capacity from the farm Sheds DB capture.';

-- +goose Down
ALTER TABLE public.shed_partitions
    DROP CONSTRAINT IF EXISTS shed_partitions_capacity_check;

ALTER TABLE public.shed_partitions
    DROP COLUMN IF EXISTS capacity;
