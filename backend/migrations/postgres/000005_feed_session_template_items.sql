-- +goose Up
-- Two defects found by running /feed-direction/preview against live seeded data. Both are about
-- the same thing: WHICH feed items a direction row is allowed to name.
--
-- WHY A FORWARD MIGRATION (not an edit to 000003):
-- the migrator is forward-only. 000001-000004 are applied and checksum-tracked on dev/stg, so
-- editing historical SQL would leave those databases silently missing everything below. Same rule
-- that produced 000002, 000003, and 000004.
--
-- ===========================================================================
-- DEFECT 1 -- the direction generated feed nobody packs
-- ===========================================================================
-- 000003 modelled feed_session_templates as (park, session_no, session_label, split_fraction) and
-- stopped there. It recorded HOW MUCH of the day each session carries, but never WHICH feed items
-- that session actually consists of. With no such declaration, the generator had only one list of
-- items to walk -- the whole feed_item_catalog -- so every shed row asked for every feed item in
-- the tenant.
--
-- That is wrong on the live grid for a specific reason. Hybrid, COFS, Hedge Lucerne and Dry Maize
-- are not four feeds that are all served; they are inter-feed SUBSTITUTION alternatives (that is
-- what feed_conversions exists for). Exactly one roughage is fed. Walking the catalog produced,
-- on one real CBE run, 112 blocked cells (28 each for the four alternatives, which are legitimately
-- unauthored for ~12 of the 77 group/tag rows) plus 619 kg of "Hybrid" that no packer has a bag
-- for. Both symptoms are the same root cause: the catalog is the tenant's VOCABULARY of feeds, not
-- any park's RECIPE for a session.
--
-- The source workbook has always carried the recipe. Its `Template` tab declares, per farm per
-- session, exactly five numbered feed slots:
--
--   CBE / CPT, Session 1 and 2:
--     Feed 1 Concentrate | Feed 2 Dry Masoor Bhusa | Feed 3 Mesha Concentrate Goat
--     Feed 4 Mesha Concentrate Sheep | Feed 5 Baking Soda
--
-- feed_session_template_items below is that tab. The generator now emits ONLY the items declared
-- for the park+session it is generating, in slot_no order.
--
-- THIS DOES NOT WEAKEN THE BLOCKED-VS-ZERO RULE. Read 000003's CONFIGURED ZERO block again: it
-- governs what happens when a REQUESTED item has no authored rate, and that is unchanged. A slot
-- that IS declared but whose (park, group, tag, item) cell has no currently-open rate still blocks
-- loudly and still contributes no number. What changed is only which items are requested:
--
--   declared slot, authored rate      -> resolved (0 is a legal authored rate)
--   declared slot, NO authored rate   -> BLOCKED, exactly as before   <-- unchanged
--   catalog item, never declared      -> not requested, so absent entirely -- not blocked,
--                                        because nobody ever said this park feeds it
--
-- The last line is the fix, and it is a narrowing of the QUESTION, not a softening of the ANSWER.
-- "This park does not feed Hybrid" and "this park feeds Hybrid but nobody said how much" are
-- different states and must not render identically; before this migration they did, and the real
-- gaps were buried under 112 cells of noise.
--
-- WHY THE SLOTS HANG OFF THE SESSION AND NOT OFF THE PARK
-- -------------------------------------------------------
-- Both live parks currently declare the same five slots in both sessions, so a park-level list
-- would fit today's data. It is modelled per session anyway because the workbook models it per
-- session: the Template tab has a row per session precisely so a farm CAN feed concentrate in the
-- morning and roughage in the evening. Collapsing that to a park list would make the first such
-- change a migration instead of a config edit, and would silently split the wrong feed across the
-- wrong session in the meantime.
--
-- The experiment workbook's Template tab declares a DIFFERENT five (Mesha Concentrate Goat |
-- Mesha Concentrate Sheep | RGS Concentrate | Vijay Concentrate | Dry Masoor Bhusa). This table
-- represents that shape fine -- it is just another (park, session) slot set. It is deliberately
-- NOT seeded: feed_experiment_config has no source rows yet, and seeding a recipe for sheds that
-- do not exist would author config nobody can trace to a farm decision. Experiment sheds also do
-- not consult this table at all -- their hand-entered cells ARE their complete item list (see
-- domain.ExperimentPlanner), which is why the generator scopes slots per planner rather than
-- globally.
--
-- EFFECTIVE-DATED, like feed_ration_rates and feed_schedule_config. Changing what a session
-- consists of is a real farm decision with a date, and "what were we packing for CBE session 2 in
-- March" must stay answerable. A change CLOSES the current row (valid_to = the day the change
-- takes effect) and OPENS a new one; it is not an in-place UPDATE of feed_item_label.
CREATE TABLE feed_session_template_items (
    session_template_item_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                uuid NOT NULL REFERENCES tenants (tenant_id),
    park_id                  uuid NOT NULL,
    session_no               integer NOT NULL,
    -- The workbook's "Feed 1..Feed 5" column position. It is the PACKING ORDER, not a mere display
    -- hint: the direction sheet and the packing worklist both render items in this sequence, so a
    -- packer's bags come out in the order the sheet lists them.
    slot_no                  integer NOT NULL,
    feed_item_label          text NOT NULL,
    feed_item_key            text GENERATED ALWAYS AS (feed_config_norm(feed_item_label)) STORED,
    status                   text NOT NULL DEFAULT 'active',
    valid_from               date NOT NULL DEFAULT CURRENT_DATE,
    valid_to                 date,
    created_by               uuid,
    created_at               timestamptz NOT NULL DEFAULT now(),
    updated_at               timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT feed_session_template_items_park_fk
        FOREIGN KEY (tenant_id, park_id) REFERENCES locations (tenant_id, location_id),
    -- A slot for a session that does not exist is unusable: it declares an item for a share of the
    -- day that has no split_fraction, so it could never be given a quantity. Unlike the label FKs
    -- this schema deliberately omits (rates do not FK the catalog either -- an authored label is
    -- allowed to name a feed the catalog has not caught up with, and a missing rate is what blocks,
    -- not a missing catalog row), this one is a structural impossibility rather than a data gap.
    CONSTRAINT feed_session_template_items_session_fk
        FOREIGN KEY (tenant_id, park_id, session_no)
        REFERENCES feed_session_templates (tenant_id, park_id, session_no),
    CONSTRAINT feed_session_template_items_session_no_check CHECK (session_no >= 1),
    CONSTRAINT feed_session_template_items_slot_no_check CHECK (slot_no >= 1),
    CONSTRAINT feed_session_template_items_item_label_not_blank CHECK (btrim(feed_item_label) <> ''),
    CONSTRAINT feed_session_template_items_status_check
        CHECK (status = ANY (ARRAY['active'::text, 'retired'::text])),
    CONSTRAINT feed_session_template_items_window_check
        CHECK (valid_to IS NULL OR valid_to > valid_from)
);

-- Natural key including valid_from: one authored item per (park, session, slot) per effective date.
CREATE UNIQUE INDEX feed_session_template_items_natural_key_uidx
    ON feed_session_template_items (tenant_id, park_id, session_no, slot_no, valid_from);

-- At most ONE open (current) row per slot. Two open rows would make "what is Feed 3 of CBE session
-- 1" ambiguous and the packing order non-deterministic.
CREATE UNIQUE INDEX feed_session_template_items_open_slot_uidx
    ON feed_session_template_items (tenant_id, park_id, session_no, slot_no)
    WHERE valid_to IS NULL;

-- The SAME feed item may not occupy two live slots of one session. This is not tidiness: the
-- generator emits one line per declared slot, so a duplicate would print the shed's concentrate
-- twice and the packer would fill two bags of it. Retiring a slot frees the item to be re-declared
-- elsewhere, which is why status is in the predicate.
CREATE UNIQUE INDEX feed_session_template_items_open_item_uidx
    ON feed_session_template_items (tenant_id, park_id, session_no, feed_item_key)
    WHERE valid_to IS NULL AND status = 'active';

-- Hot read: the whole park's slot set for one business date, in one indexed scan, once per
-- generation request. Covering, so the recipe is answered from the index.
CREATE INDEX feed_session_template_items_current_lookup_idx
    ON feed_session_template_items (tenant_id, park_id, session_no, slot_no)
    INCLUDE (feed_item_label, feed_item_key)
    WHERE valid_to IS NULL AND status = 'active';

-- As-of read: the same lookup for a historical business date (audit / back-dated regeneration).
CREATE INDEX feed_session_template_items_asof_lookup_idx
    ON feed_session_template_items (tenant_id, park_id, session_no, valid_from DESC);

COMMENT ON TABLE feed_session_template_items IS
  'The source workbook''s Template tab: which feed items each (park, session) actually consists of, in packing-slot order. The generator emits ONLY these items -- feed_item_catalog is the tenant''s feed VOCABULARY, not any park''s session RECIPE. Narrowing which items are requested does NOT weaken 000003''s blocked-vs-zero rule: a declared slot with no authored rate still blocks.';
COMMENT ON COLUMN feed_session_template_items.slot_no IS
  'The workbook''s Feed 1..Feed 5 position. Packing order, not a display hint: the direction sheet and the packing worklist both render items in this sequence.';
COMMENT ON COLUMN feed_session_template_items.feed_item_label IS
  'The authored feed name, joined to feed_ration_rates.feed_item_label through the generated feed_item_key. A declared item whose (park, group, tag, item) cell has no currently-open rate BLOCKS the shed -- it is never fed 0.';
COMMENT ON COLUMN feed_session_template_items.valid_to IS
  'NULL = currently in force. Changing what a session consists of closes this row and inserts a new one; it does not UPDATE feed_item_label in place.';


-- ===========================================================================
-- DEFECT 2 -- feed item labels were spreadsheet column headers, not feed names
-- ===========================================================================
-- The catalog and the grid were loaded straight from the source VALIDATION sheet's column headers,
-- which read "Concentrate Per Goat", "Dry Masoor Bhusa Per Goat", "Hybrid Per Goat", and so on.
-- "Per Goat" is that sheet's UNIT descriptor -- it tells a reader the column holds a per-head
-- figure -- and it is not part of any feed's name. Nobody at the farm orders "Baking Soda Per
-- Goat"; they order baking soda.
--
-- The unit is already carried separately and correctly by the app contract ("Grams / head / day"),
-- so the suffix was pure duplication in the one place it does damage: a packing sheet, where the
-- line an operator reads must be the name on the sack.
--
-- WHY THE RENAME IS A DATA MIGRATION AND NOT A CODE-SIDE DISPLAY TRIM
-- -------------------------------------------------------------------
-- feed_item_key is GENERATED ALWAYS from feed_item_label, and it is the JOIN KEY between the
-- catalog, the ration grid, the shed factors, the substitution matrix, and (from today) the session
-- slots. Trimming the suffix at render time would leave the stored keys as
-- 'concentrate_per_goat' while the new session slots key on 'concentrate' -- every rate lookup
-- would miss, and per 000003's rule a missed lookup BLOCKS. Renaming the label is therefore the
-- only way to move the key, and the label must move in every table that carries one, in the same
-- transaction, or the grid orphans itself.
--
-- Goose runs this file in a single transaction, so the statements below either all land or none
-- do; there is no window in which the catalog is renamed and the rates are not.
--
-- SAFE BY CONSTRUCTION: the strip is applied uniformly to every table's copy of the label, so two
-- items that were distinct stay distinct. All ten live labels remain distinct after the strip
-- (Concentrate, Mesha Concentrate Goat, Mesha Concentrate Sheep, Dry Masoor Bhusa, Toor Dal Bhusa
-- Pellet, Hybrid, COFS, Hedge Lucerne, Dry Maize, Baking Soda). If some other database did hold a
-- pair that collides under the strip, the natural-key unique indexes reject the UPDATE and this
-- migration fails -- which is the correct outcome, because silently merging two feeds' rates would
-- be a feeding error, not a naming one.
--
-- The pattern is anchored to the END of the label and requires whitespace before "per", so the
-- genuine names are untouched: "Mesha Concentrate Goat" and "Mesha Concentrate Sheep" do not end in
-- "Per Goat", and "Toor Dal Bhusa Pellet" contains neither word.

-- +goose StatementBegin
DO $$
DECLARE
    -- Anchored at end-of-string; requires at least one space before 'per'. Case-insensitive so a
    -- hand-authored 'per goat' is caught too.
    strip_pattern CONSTANT text := '[[:space:]]+per[[:space:]]+goat[[:space:]]*$';
BEGIN
    -- 1. The catalog -- the vocabulary itself.
    UPDATE feed_item_catalog
    SET feed_item_label = regexp_replace(feed_item_label, strip_pattern, '', 'i'),
        updated_at = now()
    WHERE feed_item_label ~* strip_pattern
      AND btrim(regexp_replace(feed_item_label, strip_pattern, '', 'i')) <> '';

    -- 2. The ration grid -- carries the same string, so the same rename, or every rate orphans.
    UPDATE feed_ration_rates
    SET feed_item_label = regexp_replace(feed_item_label, strip_pattern, '', 'i'),
        updated_at = now()
    WHERE feed_item_label ~* strip_pattern
      AND btrim(regexp_replace(feed_item_label, strip_pattern, '', 'i')) <> '';

    -- 3-5. The remaining tables that carry a feed_item_label. They hold no rows today (they are
    -- authored in-app and have no source data yet), but they are renamed in the same transaction
    -- anyway: leaving them out would make the completeness of this migration depend on the
    -- accident of those tables being empty, and the first authored shed factor under an old label
    -- would join to nothing.
    UPDATE feed_shed_factors
    SET feed_item_label = regexp_replace(feed_item_label, strip_pattern, '', 'i'),
        updated_at = now()
    WHERE feed_item_label ~* strip_pattern
      AND btrim(regexp_replace(feed_item_label, strip_pattern, '', 'i')) <> '';

    UPDATE feed_experiment_config
    SET feed_item_label = regexp_replace(feed_item_label, strip_pattern, '', 'i'),
        updated_at = now()
    WHERE feed_item_label ~* strip_pattern
      AND btrim(regexp_replace(feed_item_label, strip_pattern, '', 'i')) <> '';

    UPDATE feed_conversions
    SET from_item_label = regexp_replace(from_item_label, strip_pattern, '', 'i'),
        to_item_label = regexp_replace(to_item_label, strip_pattern, '', 'i'),
        updated_at = now()
    WHERE (from_item_label ~* strip_pattern OR to_item_label ~* strip_pattern)
      AND btrim(regexp_replace(from_item_label, strip_pattern, '', 'i')) <> ''
      AND btrim(regexp_replace(to_item_label, strip_pattern, '', 'i')) <> '';
END
$$;
-- +goose StatementEnd

-- POST-CONDITION: no rate may be orphaned by the rename. Every feed_ration_rates.feed_item_key must
-- still resolve to a feed_item_catalog row of the same tenant. This is asserted rather than assumed
-- because an orphaned rate is invisible at write time and only surfaces later as a blocked shed --
-- the exact failure mode 000003 exists to prevent. Failing the migration is the loud alternative.
-- +goose StatementBegin
DO $$
DECLARE
    orphans bigint;
BEGIN
    SELECT count(*) INTO orphans
    FROM feed_ration_rates r
    WHERE NOT EXISTS (
        SELECT 1 FROM feed_item_catalog c
        WHERE c.tenant_id = r.tenant_id AND c.feed_item_key = r.feed_item_key
    );
    IF orphans > 0 THEN
        RAISE EXCEPTION
            'feed item rename orphaned % feed_ration_rates row(s): a rate key no longer resolves to a feed_item_catalog row', orphans;
    END IF;
END
$$;
-- +goose StatementEnd


-- +goose Down
-- Reversible in both halves.
--
-- The table drops cleanly (its indexes and constraints are owned by it, and nothing references it).
--
-- The rename is restored by re-appending ' Per Goat' to exactly the labels that carried it before,
-- which is why they are listed explicitly rather than derived: the strip is not injective in
-- general (a label authored as 'Concentrate' by hand is indistinguishable afterwards from one
-- stripped from 'Concentrate Per Goat'), so a blanket re-append would rename feeds nobody renamed.
-- The seven listed here are precisely the source-grid headers that ended in the suffix; the three
-- genuine names (Mesha Concentrate Goat, Mesha Concentrate Sheep, Toor Dal Bhusa Pellet) are
-- absent because they never carried it.
DROP TABLE IF EXISTS feed_session_template_items;

-- +goose StatementBegin
DO $$
DECLARE
    restored CONSTANT text[] := ARRAY[
        'Concentrate', 'Dry Masoor Bhusa', 'Hybrid', 'COFS',
        'Hedge Lucerne', 'Dry Maize', 'Baking Soda'
    ];
BEGIN
    UPDATE feed_item_catalog
    SET feed_item_label = feed_item_label || ' Per Goat', updated_at = now()
    WHERE feed_item_label = ANY (restored);

    UPDATE feed_ration_rates
    SET feed_item_label = feed_item_label || ' Per Goat', updated_at = now()
    WHERE feed_item_label = ANY (restored);

    UPDATE feed_shed_factors
    SET feed_item_label = feed_item_label || ' Per Goat', updated_at = now()
    WHERE feed_item_label = ANY (restored);

    UPDATE feed_experiment_config
    SET feed_item_label = feed_item_label || ' Per Goat', updated_at = now()
    WHERE feed_item_label = ANY (restored);

    UPDATE feed_conversions
    SET from_item_label = CASE WHEN from_item_label = ANY (restored)
                               THEN from_item_label || ' Per Goat' ELSE from_item_label END,
        to_item_label = CASE WHEN to_item_label = ANY (restored)
                             THEN to_item_label || ' Per Goat' ELSE to_item_label END,
        updated_at = now()
    WHERE from_item_label = ANY (restored) OR to_item_label = ANY (restored);
END
$$;
-- +goose StatementEnd
