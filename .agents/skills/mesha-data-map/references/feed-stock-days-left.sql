-- Feed stock days left: runtime stockItemsSQL with maintained family/rate bindings.
-- Re-derived 25/09/2026 for family-level sale depletion; no new live-data snapshot claimed.
-- Re-checked 26/09/2026 after #415: stock SQL constants unchanged (sections now read in parallel only).
-- Re-checked 26/09/2026 (#415 at-scale proofs): analytics.go change is directedAnalyticsCombinedSQL shared-pen park scoping only; stock SQL unchanged.

WITH rate_override AS (
    -- HARD-CODED burn rates (domain.StockRateOverrides). Keyed on (farm, feed);
    -- an empty list is the behaviour without it.
    SELECT o.farm_label, o.feed_item_key, o.kg_per_day::numeric AS kg_per_day
    FROM unnest(array['CBE']::text[], array['concentrate']::text[], array['55']::text[]) AS o(farm_label, feed_item_key, kg_per_day)
),
merge_map AS (
    -- TRANSITIONAL split-concentrate merge (domain.StockFamilyMerge). An EMPTY
    -- mapping leaves every item its own family, reducing this query to the
    -- per-item shape it had before the merge -- which is how it reverts.
    SELECT m.member_key, m.family_key, m.family_label
    FROM unnest(array['mesha_adult_concentrate_goat','mesha_adult_concentrate_sheep','mesha_kids_goat_concentrate','mesha_kids_sheep_concentrate']::text[], array['mesha_adult_concentrate','mesha_adult_concentrate','mesha_kids_concentrate','mesha_kids_concentrate']::text[], array['Mesha Adult Concentrate','Mesha Adult Concentrate','Mesha Kids Concentrate','Mesha Kids Concentrate']::text[]) AS m(member_key, family_key, family_label)
),
bought AS (
    SELECT farm_label, feed_item_key,
           MAX(feed_item_label)                          AS feed_item_label,
           MIN(park_id::text)                            AS park_id_text,
           SUM(stock_kg - consumed_at_import_kg) AS net_kg,
           (array_agg(batch_no ORDER BY depletes_from DESC, purchase_date DESC, batch_no DESC))[1] AS latest_batch,
           MIN(depletes_from)                            AS depletes_from
	FROM feed_purchases
	WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
	  AND delivery_status = 'reached'
	  AND (coalesce(cardinality('{}'::uuid[]::uuid[]), 0) = 0 OR park_id = ANY ('{}'::uuid[]::uuid[]))
    GROUP BY farm_label, feed_item_key
),
locked_cells AS (
    -- Consumption from BOTH sources at one grain: locked-sheet directed kg,
    -- plus feed_effective_external_consumption for feeds the ration grid does
    -- not direct (UHT Milk: the Milk Preparation operator's submitted litres,
    -- read as kg 1:1, with the 000185 ledger as fallback; migration 000216).
    -- That view is already one row per (tenant, park, item, day). The outer GROUP BY
    -- collapses the union so a feed appearing in both sources on one day sums
    -- once per (park, item, day) — total consumed, never a duplicate row.
    SELECT park_id, feed_item_key, feed_day, SUM(kg) AS kg
    FROM (
        SELECT i.park_id, r.feed_item_key, i.feed_day, SUM(r.quantity_kg) AS kg
        FROM feed_direction_issues i
        JOIN feed_direction_issue_rows r
          ON r.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid AND r.feed_direction_issue_id = i.feed_direction_issue_id
        WHERE i.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
          AND (coalesce(cardinality('{}'::uuid[]::uuid[]), 0) = 0 OR i.park_id = ANY ('{}'::uuid[]::uuid[]))
          AND i.state = 'locked'
          AND r.quantity_kg IS NOT NULL
        GROUP BY i.park_id, r.feed_item_key, i.feed_day
        UNION ALL
        SELECT x.park_id, x.feed_item_key, x.feed_day, SUM(x.quantity_kg) AS kg
        FROM feed_effective_external_consumption x
        WHERE x.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
          AND x.park_id IS NOT NULL
          AND (coalesce(cardinality('{}'::uuid[]::uuid[]), 0) = 0 OR x.park_id = ANY ('{}'::uuid[]::uuid[]))
        GROUP BY x.park_id, x.feed_item_key, x.feed_day
    ) both_sources
    GROUP BY park_id, feed_item_key, feed_day
),
directed AS (
    SELECT b.farm_label, b.feed_item_key, COALESCE(SUM(lc.kg), 0) AS kg
    FROM bought b
    LEFT JOIN locked_cells lc
      ON b.park_id_text IS NOT NULL
     AND lc.park_id = b.park_id_text::uuid
     AND lc.feed_item_key = b.feed_item_key
     AND lc.feed_day >= b.depletes_from
    GROUP BY b.farm_label, b.feed_item_key
),

sold AS (
    SELECT s.farm_label, COALESCE(mm.family_key, s.feed_item_key) AS family_key,
           SUM(s.quantity_kg) AS kg
    FROM feed_sale_depletions s
    LEFT JOIN merge_map mm ON mm.member_key = s.feed_item_key
    WHERE s.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
    GROUP BY s.farm_label, COALESCE(mm.family_key, s.feed_item_key)
),
-- Each item keeps its OWN ledger arithmetic -- net purchased minus everything
-- directed since ITS depletion date -- and only the finished balance is folded
-- into the family. Merging the purchases first would have collapsed the members'
-- differing depletes_from into one MIN and counted consumption that predates a
-- member's own load.
item_balance AS (
    SELECT b.farm_label,
           COALESCE(mm.family_key, b.feed_item_key)     AS family_key,
           COALESCE(mm.family_label, b.feed_item_label) AS family_label,
           b.park_id_text,
           b.latest_batch,
           b.depletes_from,
           b.net_kg - d.kg         AS balance_kg
    FROM bought b
    JOIN directed d USING (farm_label, feed_item_key)
    LEFT JOIN merge_map mm ON mm.member_key = b.feed_item_key
),
family_stock_before_sales AS (
    -- SUM, not SUM of GREATEST(...,0): a member's negative balance means the
    -- farm fed more than the ledger bought, and in a merged store that feed
    -- physically came out of a sibling sack -- so subtracting it is both the
    -- truer figure and the more conservative one. It also keeps the family
    -- balance the plain sum of the same per-item balances the tab already
    -- shows, and preserves the never-clamp rule on every unmerged card.
    --
    -- latest_batch keeps MAIN's tie-break (freshest depletion date, then
    -- purchase date, then batch number) by carrying the winner of each member
    -- and picking the family's freshest member the same way -- never MAX(),
    -- which would report an older load that happens to carry a higher number.
    SELECT farm_label, family_key,
           MAX(family_label) AS family_label,
           MIN(park_id_text) AS park_id_text,
           (array_agg(latest_batch ORDER BY depletes_from DESC, latest_batch DESC))[1] AS latest_batch,
           SUM(balance_kg)   AS balance_kg
    FROM item_balance
    GROUP BY farm_label, family_key
),
family_stock AS (
    SELECT fs.farm_label, fs.family_key, fs.family_label, fs.park_id_text, fs.latest_batch,
           fs.balance_kg - COALESCE(s.kg, 0) AS balance_kg
    FROM family_stock_before_sales fs
    LEFT JOIN sold s USING (farm_label, family_key)
),
family_day AS (
    -- Consumption re-grouped to the FAMILY before the daily average, so two
    -- members feeding the same pens on the same day count once and a member
    -- SUBSTITUTED for another does not inflate the rate.
    SELECT lc.park_id,
           COALESCE(mm.family_key, lc.feed_item_key) AS family_key,
           lc.feed_day,
           SUM(lc.kg)                                AS kg
    FROM locked_cells lc
    LEFT JOIN merge_map mm ON mm.member_key = lc.feed_item_key
    GROUP BY lc.park_id, COALESCE(mm.family_key, lc.feed_item_key), lc.feed_day
),
recent AS (
    SELECT park_id, family_key, AVG(kg) AS avg_kg
    FROM (
        SELECT park_id, family_key, kg,
               ROW_NUMBER() OVER (PARTITION BY park_id, family_key ORDER BY feed_day DESC) AS rn
        FROM family_day
    ) ranked
    WHERE rn <= 3
    GROUP BY park_id, family_key
)
SELECT fs.farm_label,
       fs.family_label,
       fs.family_key,
       round(fs.balance_kg, 1)::text                      AS balance_kg,
       COALESCE(round(COALESCE(ov.kg_per_day, r.avg_kg), 1)::text, '') AS avg_daily_kg,
       CASE WHEN COALESCE(ov.kg_per_day, r.avg_kg, 0) > 0
            THEN GREATEST(floor(fs.balance_kg / COALESCE(ov.kg_per_day, r.avg_kg)), 0)::bigint
       END                                                AS days_left,
       fs.latest_batch,
       (COALESCE(ov.kg_per_day, r.avg_kg, 0) <= 0
        AND round(fs.balance_kg, 1) > 0)                  AS not_started
FROM family_stock fs
LEFT JOIN recent r
  ON fs.park_id_text IS NOT NULL
 AND r.park_id = fs.park_id_text::uuid
 AND r.family_key = fs.family_key
LEFT JOIN rate_override ov
  ON ov.farm_label = fs.farm_label
 AND ov.feed_item_key = fs.family_key
-- ACTIVE FEEDS ONLY (2026-09-24, stock cards): a card needs a catalog row that says 'active';
-- read on the FAMILY key, so a retired MEMBER still contributes to its active successor's card.
LEFT JOIN feed_item_catalog c
  ON c.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid AND c.feed_item_key = fs.family_key
WHERE c.status = 'active'
ORDER BY days_left NULLS LAST, fs.family_label, fs.farm_label;
