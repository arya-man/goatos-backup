-- +goose Up
-- shed_partitions: the catalog of partitions that PHYSICALLY EXIST in a shed.
--
-- Why this table has to exist.
--
-- `goat_shed_partitions` is keyed PRIMARY KEY (tenant_id, goat_id) -- it is a
-- PER-GOAT attribute, not a catalog. A partition therefore "exists" only while
-- an animal is standing in it. CBE Yashoda proves the hole: partitions
-- 1,2,3,4,6,7,8,9,10 hold animals, partition 5 holds none, and `Yashoda 5` is a
-- real physical partition that no goat-derived query can see. Any destination
-- picker built from goat rows alone can never offer an EMPTY partition, so an
-- operator cannot move the first animal into one -- the "cannot fill an empty
-- pen" bug.
--
-- The only surviving record of the full partition list is the set of
-- partition-bearing `locations` rows (`Castro 1`, `Godel 1 - Part 3`,
-- `Yashoda 5`) that carry status='inactive'. Those rows are NOT dead:
-- `weighing_campaign_sheds` already references them today. Their 'inactive'
-- status means "not a building", not "not a place" -- the status column is
-- overloaded, which is the actual schema defect this table retires.
--
-- Those alias rows are parented to the PARK, not to the shed, so resolving one
-- to its parent shed needs a name parse ('Yashoda 5' -> shed 'Yashoda' +
-- partition '5'). Doing that parse per request would put a non-SARGable string
-- match on a hot picker path (banned -- see docs/decisions/scale-anti-patterns.md).
-- So the parse happens ONCE, here, and the result is stored.
--
-- Scope of this migration (deliberately Phase A only):
--   * CREATE the catalog and SEED it from evidence that already exists.
--   * NO foreign key from goat_shed_partitions yet -- a goat whose label is not
--     in the catalog must not fail to save while the catalog is still settling.
--   * NO mutation of ANY existing row in any table -- this migration only ever INSERTs into the
--     brand-new catalog it just created.
--   seed-fixture-guard:ignore: creates one brand-new operational catalog table and mutates nothing
--   else. Seed closeout neither authors nor rebuilds shed_partitions, so the fixture, manifest and
--   runbook companions carry no contract for it.
--     Preserved history (CPT adult ET+TT W1/W2, 324 animals each) and all
--     Aug-5/future data are untouched by construction: this migration only ever
--     INSERTs into a brand-new table.
--   * NO invention. A partition is seeded only if it is attested by a live goat
--     or by an existing partition-bearing location row.

CREATE TABLE IF NOT EXISTS shed_partitions (
    tenant_id        uuid        NOT NULL,
    shed_id          uuid        NOT NULL,
    -- Raw label as the farm writes it: '1', '2', 'Part 3'. Display keeps this
    -- verbatim so the screen matches the sign painted on the shed.
    partition_label  text        NOT NULL,
    -- Comparison key. Mirrors oploc.NormalizePartition and the SQL normalizer
    -- used by the operator-execution reads, so 'Part 3' and '3' are ONE
    -- partition and can never be catalogued twice.
    normalized_label text        NOT NULL,
    status           text        NOT NULL DEFAULT 'active',
    display_order    integer,
    -- Where the row came from, so a later audit can tell attested-by-animals
    -- from attested-by-legacy-location.
    source           text        NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT shed_partitions_pkey PRIMARY KEY (tenant_id, shed_id, normalized_label),
    CONSTRAINT shed_partitions_label_nonblank CHECK (btrim(partition_label) <> ''),
    -- 'whole' is a matching sentinel, never a real partition, and must never be
    -- catalogued as one.
    CONSTRAINT shed_partitions_not_whole CHECK (normalized_label <> 'whole'),
    CONSTRAINT shed_partitions_status CHECK (status IN ('active', 'retired')),
    CONSTRAINT shed_partitions_source CHECK (source IN ('goat_attested', 'location_alias', 'manual')),
    CONSTRAINT shed_partitions_shed_fk FOREIGN KEY (tenant_id, shed_id)
        REFERENCES locations (tenant_id, location_id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS shed_partitions_tenant_shed_idx
    ON shed_partitions (tenant_id, shed_id, status);

-- Seed 1: every partition currently attested by a live goat.
INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, source)
SELECT DISTINCT ON (gsp.tenant_id, gsp.shed_id, regexp_replace(lower(btrim(gsp.partition_label)), '^part[[:space:]]+', ''))
       gsp.tenant_id,
       gsp.shed_id,
       btrim(gsp.partition_label),
       regexp_replace(lower(btrim(gsp.partition_label)), '^part[[:space:]]+', ''),
       'goat_attested'
FROM goat_shed_partitions gsp
WHERE regexp_replace(lower(btrim(COALESCE(gsp.partition_label, 'whole'))), '^part[[:space:]]+', '') <> 'whole'
-- Explicit ORDER BY so DISTINCT ON is deterministic. Today no shed carries two
-- spellings of one partition (verified: zero rows where a single
-- (shed, normalized_label) has more than one distinct partition_label), but
-- without this a future shed holding both '3' and 'Part 3' would store whichever
-- row the planner happened to return, and a re-run could silently flip the
-- displayed label. Seed 2 below already orders explicitly; this matches it.
ORDER BY gsp.tenant_id,
         gsp.shed_id,
         regexp_replace(lower(btrim(gsp.partition_label)), '^part[[:space:]]+', ''),
         btrim(gsp.partition_label)
ON CONFLICT (tenant_id, shed_id, normalized_label) DO NOTHING;

-- Seed 2: partitions attested ONLY by a partition-bearing location row -- the
-- empty ones (Yashoda 5). Alias rows hang off the PARK, so match them to a shed
-- in the SAME park whose name is a prefix of the alias name, then take the
-- remainder as the label:
--     'Yashoda 5'        -> shed 'Yashoda'  + '5'
--     'Godel 1 - Part 3' -> shed 'Godel 1'  + 'Part 3'
-- The longest matching shed name wins, so 'Godel 1 - Part 3' binds to 'Godel 1'
-- and never to a shorter shed that happens to share a prefix. Shed names repeat
-- ACROSS parks, so the park equality below is load-bearing: without it a CBE
-- alias could bind to the CPT shed of the same name.
INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, source)
SELECT DISTINCT ON (m.tenant_id, m.shed_id, m.normalized_label)
       m.tenant_id, m.shed_id, m.partition_label, m.normalized_label, 'location_alias'
FROM (
    SELECT alias.tenant_id,
           shed.location_id AS shed_id,
           btrim(regexp_replace(substr(alias.name, length(shed.name) + 1), '^[[:space:]]*-?[[:space:]]*', '')) AS partition_label,
           regexp_replace(
               lower(btrim(regexp_replace(substr(alias.name, length(shed.name) + 1), '^[[:space:]]*-?[[:space:]]*', ''))),
               '^part[[:space:]]+', ''
           ) AS normalized_label,
           length(shed.name) AS shed_name_len
    FROM locations alias
    JOIN locations shed
      ON shed.tenant_id = alias.tenant_id
     AND shed.parent_location_id = alias.parent_location_id  -- same park
     AND shed.location_type = 'shed'
     AND shed.status = 'active'
     AND shed.retired_at IS NULL
     AND alias.name <> shed.name
     AND alias.name LIKE shed.name || '%'
    WHERE alias.location_type = 'shed'
      AND alias.status = 'inactive'
) m
WHERE m.partition_label <> ''
  AND m.normalized_label <> 'whole'
  -- Conservative whitelist: after normalization a real partition is a bare
  -- ordinal ('1', '10' -- and 'Part 3' normalizes to '3'). Anything else is a
  -- legacy artifact, NOT a partition, and seeding it would invent a place that
  -- does not exist.
  --
  -- This rejects the alias-of-an-alias rows. CBE has locations named
  -- 'Gandhi 1 - Part 1' AND an inactive 'Gandhi 1'. Because 'Gandhi 1' is not an
  -- ACTIVE shed, longest-prefix binds 'Gandhi 1 - Part 1' to shed 'Gandhi' and
  -- yields the nonsense label '1 - Part 1'. Live animals say Gandhi has exactly
  -- partitions 1, 2, 3. Without this filter the catalog would gain two fake
  -- partitions and offer them as shifting destinations.
  AND m.normalized_label ~ '^[0-9]+$'
ORDER BY m.tenant_id, m.shed_id, m.normalized_label, m.shed_name_len DESC
ON CONFLICT (tenant_id, shed_id, normalized_label) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS shed_partitions;
