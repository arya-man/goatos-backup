-- +goose Up
-- seed-fixture-guard:ignore: authored feed-grid completion only; no seed contract or schema change
--
-- Fill the ration grid so EVERY active feed item has a row in every cell that already exists,
-- authored at 0 g/head where nothing was authored before.
--
-- WHY, and it is not cosmetic. Absence and zero mean different things in this module, and the
-- difference decides whether animals get fed. `normalItem` (feeddirection/domain/strategy.go) reads
-- a MISSING rate as BLOCKED -- the shed's sheet fails with `no currently-open ration rate for ...`
-- -- and an authored 0 as a real quantity that happens to contribute nothing. The retire path says
-- the same thing from the other side: a restored item "whose every combination is UNCONFIGURED ...
-- means BLOCKED: those sheds would not be fed."
--
-- The consequence was that a feed item with no rate row was UNREACHABLE from the Feed Config
-- screen. The grid renders only rows that exist, each with an Edit button, and there is no control
-- that creates one -- so `Vijay Concentrate` and `RGS Concentrate`, added to the catalog on
-- 2026-08-07 and never given a rate, could not be authored anywhere. This makes every cell
-- editable.
--
-- THIS MIGRATION ONLY EVER INSERTS. It contains no UPDATE and no DELETE, and every insert is
-- guarded by NOT EXISTS against an open row, so an authored quantity -- including an authored 0 --
-- is never overwritten, closed, or replaced. The assertions at the bottom prove it rather than
-- asserting it in prose: the count of pre-existing open rows and the SUM of every authored gram
-- must both come through unchanged.
--
-- SCOPE IS THE CELLS THAT ALREADY EXIST, deliberately. The authored grid is ragged on purpose --
-- Boer carries 1 shed tag where Beetal/Sirohi carries 13 -- so the live set is 77 cells per park
-- against the 101 the group x tag cartesian would allow. Filling the cartesian would INVENT 24
-- cells per park and thereby unblock feeding combinations nobody authored, which is a business
-- decision this migration has no standing to make. A cell nobody authored stays blocked.
--
-- Active items only, matching the read (feedconfig/adapters/postgres.ListRationRates already scopes
-- to `feed_item_catalog.status = 'active'`), so this does not resurrect a retired feed.
--
-- Nothing an animal eats changes. These items are not declared in any park's session template, and
-- generation looks up a rate only for a DECLARED slot (`ConfigSnapshot.PlannedFeedItems`), so the
-- new rows are not even read by a feed sheet. They make the cells AUTHORABLE; declaring a slot is a
-- separate, deliberate act.
DO $$
DECLARE
    open_rows_before  bigint;
    open_rows_after   bigint;
    authored_before   numeric;
    authored_after    numeric;
    filled            bigint;
BEGIN
    SELECT count(*), coalesce(sum(grams_per_head), 0)
      INTO open_rows_before, authored_before
      FROM public.feed_ration_rates
     WHERE valid_to IS NULL;

    -- One set-based insert. `cells` is the authored grid as it stands: every (tenant, park, group,
    -- tag) that already carries at least one open row. Labels are carried from the existing rows
    -- rather than from the group/tag catalogs, so a filled row is spelled exactly like its
    -- neighbours in the same cell and normalizes onto the same generated key.
    WITH cells AS (
        SELECT DISTINCT tenant_id, park_id, ration_group_label, shed_tag_label
          FROM public.feed_ration_rates
         WHERE valid_to IS NULL
    ),
    items AS (
        SELECT tenant_id, feed_item_label
          FROM public.feed_item_catalog
         WHERE status = 'active'
    )
    INSERT INTO public.feed_ration_rates (
        tenant_id, park_id, ration_group_label, shed_tag_label, feed_item_label,
        grams_per_head, valid_from, source_system
    )
    SELECT c.tenant_id, c.park_id, c.ration_group_label, c.shed_tag_label, i.feed_item_label,
           0, CURRENT_DATE, 'grid_fill'
      FROM cells c
      JOIN items i ON i.tenant_id = c.tenant_id
     WHERE NOT EXISTS (
        SELECT 1
          FROM public.feed_ration_rates r
         WHERE r.tenant_id        = c.tenant_id
           AND r.park_id          = c.park_id
           AND r.ration_group_key = public.feed_config_norm(c.ration_group_label)
           AND r.shed_tag_key     = public.feed_config_norm(c.shed_tag_label)
           AND r.feed_item_key    = public.feed_config_norm(i.feed_item_label)
           AND r.valid_to IS NULL)
    -- Re-runnable and safe against a same-day authored-then-superseded row occupying this
    -- (cell, item, CURRENT_DATE) natural key. Skipping is correct: the cell then already has an
    -- open row, which is the state this migration exists to reach.
    ON CONFLICT DO NOTHING;

    GET DIAGNOSTICS filled = ROW_COUNT;

    SELECT count(*), coalesce(sum(grams_per_head), 0)
      INTO open_rows_after, authored_after
      FROM public.feed_ration_rates
     WHERE valid_to IS NULL;

    -- Proof of non-destruction, checked inside the migration's own transaction so a violation rolls
    -- the whole thing back rather than being discovered later against live feed data.
    --
    -- (1) Every pre-existing open row is still open: the population grew by exactly what was
    --     inserted, so nothing was closed or removed to make room.
    IF open_rows_after <> open_rows_before + filled THEN
        RAISE EXCEPTION
            'feed grid fill changed the open-row population: before=% inserted=% after=% (expected %)',
            open_rows_before, filled, open_rows_after, open_rows_before + filled;
    END IF;

    -- (2) Not one authored gram moved. Every inserted row is 0, so the total authored quantity
    --     across the whole live grid must be byte-for-byte what it was. This is the check that
    --     would catch an accidental overwrite of a real quantity with a zero.
    IF authored_after <> authored_before THEN
        RAISE EXCEPTION
            'feed grid fill altered authored quantities: total was % and is now %',
            authored_before, authored_after;
    END IF;

    RAISE NOTICE 'feed grid fill: % cells completed, % authored rows untouched', filled, open_rows_before;
END
$$;

-- +goose Down
-- Reversible precisely because the fill is identifiable: `source_system = 'grid_fill'` is written
-- by nothing else (the authoring path writes 'manual'), and every filled row is 0 g/head. Both
-- predicates are required -- an operator who edits a filled cell to a real quantity is authoring,
-- and that row must survive a rollback even though this migration created it.
DELETE FROM public.feed_ration_rates
 WHERE source_system = 'grid_fill'
   AND grams_per_head = 0
   AND valid_to IS NULL;
