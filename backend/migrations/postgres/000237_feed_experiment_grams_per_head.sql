-- +goose Up

-- EXPERIMENT QUANTITIES ARE AUTHORED PER ANIMAL (maintainer decision 2026-09-01).
--
-- Until today an experiment cell was an ABSOLUTE kg for the whole pen, and head_count was
-- informational -- the single most important distinction between this table and feed_ration_rates,
-- stated in the baseline comment, in domain.ExperimentPlanner, and in a copy guard. That is now
-- INVERTED for newly authored cells: the operator enters GRAMS PER ANIMAL and the generator
-- multiplies by the pen's LIVE projected head count, exactly as the ration grid does.
--
-- WHAT DID NOT CHANGE, and why this is two columns rather than a rewrite of one. The maintainer's
-- instruction was that no previously authored data changes. Converting today's kg into a per-head
-- rate would write a number nobody authored (kg x 1000 / a head count recorded at some past date),
-- and it would silently drift from today's quantity the moment an animal moves. So every existing
-- row keeps its absolute kg and keeps feeding exactly what it feeds now, and quantity_basis records
-- which question the stored number answers. A pen may hold both kinds while it is being
-- re-authored; that is honest and temporary, and the basis is per CELL rather than per pen so that
-- editing one item can never silently reinterpret the pen's other items as per-head figures.
--
-- WHAT IS STILL NOT A MULTIPLIER: head_count. It stays the informational population the author
-- typed alongside the figure. The count that scales a grams_per_head cell is the LIVE projected
-- count the generator already holds for the pen -- the same one the normal workflow uses, so an
-- experiment pen and a normal pen answer "how many animals am I feeding" from one source.
--
-- The pen's shed factor is deliberately NOT applied (maintainer decision, same day): an experiment
-- rate is grams per animal x head count and nothing else.
ALTER TABLE feed_experiment_config
    ADD COLUMN grams_per_head numeric(12, 3),
    ADD COLUMN quantity_basis text NOT NULL DEFAULT 'absolute_kg';

-- absolute_kg becomes nullable because a per-head cell has no pen total to store: the total is
-- derived at generation time from a head count this table does not own. Leaving it NOT NULL would
-- force a per-head row to carry a frozen total that goes stale the first time an animal moves.
ALTER TABLE feed_experiment_config
    ALTER COLUMN absolute_kg DROP NOT NULL;

-- EXACTLY ONE of the two figures is present, and the basis names which. The pair check is what
-- stops a row from carrying both (two answers, one of them stale) or neither (a cell that reads as
-- configured while feeding nothing). The zero-vs-missing rule is unchanged on both sides: an
-- authored 0 is a real instruction ("this arm gets none of this item") and a MISSING ROW is what
-- means "not configured".
ALTER TABLE feed_experiment_config
    ADD CONSTRAINT feed_experiment_config_quantity_basis_check
        CHECK (quantity_basis = ANY (ARRAY['absolute_kg'::text, 'grams_per_head'::text])),
    ADD CONSTRAINT feed_experiment_config_grams_per_head_check
        CHECK (grams_per_head IS NULL OR grams_per_head >= 0),
    ADD CONSTRAINT feed_experiment_config_basis_pairing_check
        CHECK (
            (quantity_basis = 'absolute_kg' AND absolute_kg IS NOT NULL AND grams_per_head IS NULL)
            OR (quantity_basis = 'grams_per_head' AND grams_per_head IS NOT NULL AND absolute_kg IS NULL)
        );

-- The generator's read is covered by this index; it must carry both figures and the basis or every
-- experiment row costs a heap fetch to learn which number to use.
DROP INDEX IF EXISTS feed_experiment_config_shed_lookup_idx;
CREATE INDEX feed_experiment_config_shed_lookup_idx
    ON feed_experiment_config (tenant_id, park_id, shed_id)
    INCLUDE (feed_item_key, absolute_kg, grams_per_head, quantity_basis)
    WHERE status = 'active';

COMMENT ON TABLE feed_experiment_config IS
  'Hand-entered experiment pens. quantity_basis names which figure the row carries: grams_per_head is a PER-ANIMAL rate multiplied by the pen''s live projected head count at generation time (the current authoring basis), absolute_kg is a legacy PEN TOTAL that is never multiplied. head_count is informational on both and is never a multiplier.';
COMMENT ON COLUMN feed_experiment_config.grams_per_head IS
  'Grams per animal per day. Multiplied by the pen''s LIVE projected head count by the generator, never by head_count on this row. Present exactly when quantity_basis = ''grams_per_head''. 0 means "authored as zero"; a missing ROW means "not configured" and blocks.';
COMMENT ON COLUMN feed_experiment_config.absolute_kg IS
  'LEGACY basis: absolute kg for the whole pen, never multiplied by any head count. Present exactly when quantity_basis = ''absolute_kg''. Rows authored before 2026-09-01 keep this basis until an author re-enters the cell in grams per animal.';
COMMENT ON COLUMN feed_experiment_config.quantity_basis IS
  'Which figure this row authored: ''grams_per_head'' (current) or ''absolute_kg'' (legacy pen total). Per CELL, not per pen, so editing one item cannot reinterpret the pen''s other items.';
COMMENT ON COLUMN feed_experiment_config.head_count IS
  'Informational population the author recorded beside the figure. NOT a multiplier on either basis -- a grams_per_head cell is scaled by the live projected count, not by this.';

-- +goose Down
DROP INDEX IF EXISTS feed_experiment_config_shed_lookup_idx;
CREATE INDEX feed_experiment_config_shed_lookup_idx
    ON feed_experiment_config (tenant_id, park_id, shed_id)
    INCLUDE (feed_item_key, absolute_kg)
    WHERE status = 'active';
ALTER TABLE feed_experiment_config
    DROP CONSTRAINT IF EXISTS feed_experiment_config_basis_pairing_check,
    DROP CONSTRAINT IF EXISTS feed_experiment_config_grams_per_head_check,
    DROP CONSTRAINT IF EXISTS feed_experiment_config_quantity_basis_check;
DELETE FROM feed_experiment_config WHERE quantity_basis = 'grams_per_head';
ALTER TABLE feed_experiment_config
    ALTER COLUMN absolute_kg SET NOT NULL,
    DROP COLUMN IF EXISTS quantity_basis,
    DROP COLUMN IF EXISTS grams_per_head;
