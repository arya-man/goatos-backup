-- +goose Up

-- SEED THE PER-ANIMAL RATES FROM WHAT THE FARM IS ALREADY FEEDING (maintainer decision 2026-09-01,
-- SUPERSEDING the "no previously authored data changes" half of 000237 the same day).
--
-- 000237 added the per-animal basis and deliberately left every existing cell on its legacy
-- absolute-kg total, so nothing moved until someone re-entered it by hand. The maintainer's
-- follow-up instruction is the opposite and is what this migration does: take the values the farm
-- already has, DIVIDE BY THEIR COUNT, and store the result as the pen's per-animal rate — so a
-- freshly migrated database is immediately on the new basis with real numbers in it, rather than
-- waiting on 204 hand edits. "In future if required we will change" — i.e. these are working
-- values to be corrected on screen, not a final authoring.
--
-- THE DENOMINATOR IS THE PEN'S LIVE RESIDENT COUNT (maintainer instruction: "use live only, forget
-- recorded"). The stored head_count is the population somebody typed beside the figure at some past
-- date and nothing has maintained it since; on this data it disagrees with the pen's actual
-- population for 15 of 34 pens, by as much as 7 animals against 16.
--
-- Dividing by the LIVE count means every pen keeps being fed EXACTLY what it is fed today: the rate
-- back-multiplies by the same number it was divided by. From tomorrow the total follows the animals,
-- which is the whole point of the per-animal basis, but the conversion itself moves nothing. Using
-- the recorded count instead would have re-based 15 pens on the spot, two of them by +129% and -78%,
-- on the strength of a number nobody has kept up to date.
--
-- A pen with NO LIVE ANIMALS cannot be converted — there is no denominator, and inventing one would
-- invent a rate — so its rows STAY on the legacy basis and keep feeding exactly what they feed
-- today. That is also the safe reading operationally: an empty pen is fed nothing either way, and
-- the cell is still there to convert once animals arrive and someone re-authors it.
--
-- IDEMPOTENT AND UPGRADE-SAFE. It touches ONLY rows still on quantity_basis='absolute_kg', so a
-- re-run is a no-op, a cell already re-authored in the app is never overwritten, and a database
-- that skipped 000237 is impossible (the columns are created there). Running migrations does not
-- break: this is a data conversion inside the constraints 000237 already enforces.

-- Provenance, and the reason the conversion is reversible: the pairing CHECK from 000237 forces
-- absolute_kg to NULL when a row moves to the per-animal basis, so without this table the original
-- pen total would be gone. Every converted row is recorded here with the exact inputs -- including
-- the LIVE count it was divided by, which is a moving number nobody could reconstruct later -- so
-- the arithmetic is auditable off a single SELECT rather than a re-derivation nobody trusts.
CREATE TABLE IF NOT EXISTS feed_experiment_basis_conversions (
    experiment_config_id uuid PRIMARY KEY REFERENCES feed_experiment_config (experiment_config_id) ON DELETE CASCADE,
    tenant_id            uuid NOT NULL,
    absolute_kg          numeric(12, 3) NOT NULL,
    -- The pen's LIVE resident count at conversion time. Named for what it was used as (the
    -- denominator), not for the column it did not come from: feed_experiment_config.head_count is the
    -- stale recorded figure and is deliberately not what this conversion used.
    head_count           integer NOT NULL,
    grams_per_head       numeric(12, 3) NOT NULL,
    converted_at         timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT feed_experiment_basis_conversions_head_count_check CHECK (head_count > 0)
);

COMMENT ON TABLE feed_experiment_basis_conversions IS
  'One row per experiment cell converted from the legacy absolute-kg basis to grams per animal (migration 000238). Keeps the original pen total, the LIVE head count it was divided by, and the derived rate, so the conversion is auditable and exactly reversible.';

-- SOURCE, so the workbook seeder can tell its own rows from a hand authoring.
--
-- seed-feed-ration re-asserts the experiment workbook on every run. Before the per-animal basis
-- existed that was harmless — the workbook was the only writer. Now the app is a writer too, and a
-- re-seed would quietly discard a rate the farm corrected on screen. Rows already in the table came
-- from the workbook, which is why the default is the safe answer for the backfill.
ALTER TABLE feed_experiment_config
    ADD COLUMN IF NOT EXISTS source text NOT NULL DEFAULT 'workbook';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'feed_experiment_config_source_check'
    ) THEN
        ALTER TABLE feed_experiment_config
            ADD CONSTRAINT feed_experiment_config_source_check
            CHECK (source = ANY (ARRAY['workbook'::text, 'app'::text]));
    END IF;
END $$;

COMMENT ON COLUMN feed_experiment_config.source IS
  'Who authored this cell: ''workbook'' (seed-feed-ration, re-asserted on every seed run) or ''app'' (a person on /feed/config). The seeder updates only its own rows so a re-seed cannot discard a hand authoring.';

-- The conversion itself. Recorded first, applied second, in ONE transaction: the log and the value
-- can never disagree about what a row used to hold.
-- projection-review: membership=every feed_experiment_config row still on quantity_basis 'absolute_kg' that has a live pen census to divide by; group_key=(tenant_id,shed_id,COALESCE(partition_label,'')) on the census side, the same pen identity feed_experiment_config is keyed on minus the park, which cannot fan out because a shed belongs to exactly one park; join_cardinality=census to cells is 1:N with the ONE side pre-aggregated and the MANY side being the rows updated, and goats to goat_shed_partitions is 0:1 on its (tenant_id,goat_id) PK so no animal is counted twice; pagination=none, a single set-based UPDATE over the whole table; scope=every tenant and every park, because a migration converts the table it is given
WITH live_pen_count AS (
    -- The pen's live residents, at the SAME operational grain the experiment table is keyed on:
    -- (shed, partition), with an undivided shed's animals landing on the blank label. One grouped
    -- pass over the herd rather than a correlated count per cell — 204 cells across 34 pens would
    -- otherwise be 204 scans of the same rows.
    SELECT g.tenant_id,
           g.shed_id,
           COALESCE(p.partition_label, '') AS partition_label,
           count(*) AS live_head_count
    FROM goats g
    LEFT JOIN goat_shed_partitions p
           ON p.tenant_id = g.tenant_id AND p.goat_id = g.goat_id
    WHERE g.lifecycle_status = 'alive'
    GROUP BY 1, 2, 3
),
convertible AS (
    SELECT c.experiment_config_id,
           c.tenant_id,
           c.absolute_kg,
           l.live_head_count AS head_count,
           -- TRUNCATED to the column's scale, never rounded to nearest, and the direction is the
           -- whole point. 16 kg across 63 animals is 253.968253... g; the pipeline then multiplies
           -- back up and rounds the pen's session quantity UP to a packable 0.1 kg. A rate rounded
           -- to NEAREST can land a hair ABOVE the exact figure (8 kg / 31 = 258.0645 -> 258.065 ->
           -- 8000.015 g), and that hair is enough to push the packable rounding to the next notch --
           -- 4.000 kg becomes 4.100 kg for a pen whose population never changed. Truncating keeps
           -- the derived rate at or a hair below exact (at most 0.001 g per animal, ~0.03 g across a
           -- whole pen), so a pen whose live count still equals its authored count is fed EXACTLY
           -- what it is fed today, and only a pen whose population actually moved sees its total
           -- move.
           trunc((c.absolute_kg * 1000) / l.live_head_count, 3) AS grams_per_head
    FROM feed_experiment_config c
    JOIN live_pen_count l
      ON l.tenant_id = c.tenant_id
     AND l.shed_id = c.shed_id
     AND l.partition_label = COALESCE(c.partition_label, '')
    WHERE c.quantity_basis = 'absolute_kg'
      AND c.absolute_kg IS NOT NULL
      AND l.live_head_count > 0
),
logged AS (
    INSERT INTO feed_experiment_basis_conversions
        (experiment_config_id, tenant_id, absolute_kg, head_count, grams_per_head)
    SELECT experiment_config_id, tenant_id, absolute_kg, head_count, grams_per_head
    FROM convertible
    ON CONFLICT (experiment_config_id) DO NOTHING
    RETURNING experiment_config_id
)
UPDATE feed_experiment_config c
SET quantity_basis = 'grams_per_head',
    grams_per_head = v.grams_per_head,
    absolute_kg    = NULL,
    updated_at     = now()
FROM convertible v
WHERE c.experiment_config_id = v.experiment_config_id;

-- The stored count stops being a thing anyone maintains. It is not read by the generator, not shown
-- on any screen and not written by any route from here on; the count everything uses is the pen's
-- live population. Saying so on the column is what stops the next author reaching for it.
COMMENT ON COLUMN feed_experiment_config.head_count IS
  'LEGACY PROVENANCE ONLY. The population somebody typed beside the quantity before 2026-09-01. Nothing reads, writes or displays it any more -- every count shown or multiplied by is the pen''s LIVE population from the herd register. Kept because migration 000238 recorded what it divided by, and because deleting an authored figure destroys evidence.';

-- +goose Down
-- Put every converted cell back exactly as it was, from the log rather than by re-deriving it: a
-- re-derivation would multiply the rounded rate back up and land a few grams away from the figure
-- the farm actually authored.
UPDATE feed_experiment_config c
SET quantity_basis = 'absolute_kg',
    absolute_kg    = v.absolute_kg,
    grams_per_head = NULL,
    updated_at     = now()
FROM feed_experiment_basis_conversions v
WHERE c.experiment_config_id = v.experiment_config_id
  AND c.quantity_basis = 'grams_per_head';
DROP TABLE IF EXISTS feed_experiment_basis_conversions;
ALTER TABLE feed_experiment_config
    DROP CONSTRAINT IF EXISTS feed_experiment_config_source_check,
    DROP COLUMN IF EXISTS source;
