package postgres

// saleLineShareCTEs prices every live tagged animal from ITS OWN SALE LINE (maintainer decision
// 2026-09-25, "option B"). It is a list of CTEs, spliced into the WITH clause of BOTH Load wise
// (loadwiseSalesSQL, loadwiseOverallAvgSQL) and Farm born (farmBornPopulationSQL), so the two
// pages can never price the same animal differently. $1 is the tenant.
//
// It ends in animal_share(goat_id, sales_deal_id, park_id, shed_id, partition_label, sale_date,
// buyer_name, closed, share): one row per live tagged allocation.
//
// THE MATCHING RULE, in order:
//  1. Only ANIMAL lines (product_kind = 'animal') carry animal value. Feed, manure and any other
//     item line is never attributed to an animal or a load -- it stays on the Summary cards.
//  2. A line's species is its product's species_code in the farm's sellable register, else its
//     product code (the built-in codes are the species words, 'goat' and 'sheep').
//  3. When the deal has ONE animal line of the animal's species, the animal belongs to it. When it
//     has several, the animal belongs to the one(s) whose breed matches the animal's breed; lines
//     of the same species AND breed are POOLED (their values summed, divided over every animal
//     matched to any of them).
//  4. Share = the matched line (pool) value / the animals tagged to that line (pool) -- a per-line
//     count, never per deal.
//  5. An animal that matches no line (a legacy sheet deal whose single line names another
//     species, or a breed on no line) takes the deal's UNCLAIMED animal-line value -- the animal
//     lines no animal matched -- divided over the unmatched animals. When every animal line is
//     claimed there is nothing left to give it, so its share is NULL (sold, unpriced): money is
//     neither invented nor double-counted. When NO animal matches, this is exactly "all animal
//     lines / all animals tagged", today's rule minus the non-animal lines.
//  6. A deal with NO lines at all (none exist after migration 000296's backfill, but a row written
//     around it would have none) is priced as before: the deal value / the animals tagged to it.
//  7. Only a CLOSED deal is a sale: an allocation on an open deal carries closed = false and a
//     NULL share.
//
// Consequence, pinned by TestEachTaggedAnimalIsPricedFromItsOwnSaleLine: for every closed deal
// whose animal lines all carry tagged animals, the shares across Load wise, Farm born and animals
// on neither page sum to the deal's animal-line value, and non-animal money is 0 on both.
//
// projection-review: membership=goat_sale_allocations status='tagged', one row per animal by the partial unique index (tenant_id, goat_id) WHERE status='tagged', each joined 1:1 to sales_deals and goats on their PKs, and sales_deal_lines narrowed to product_kind='animal' (unique on tenant_id, deal_id, line_no) joined 1:{0,1} to sellable_product_catalog on its PK; group_key=producer line_bucket GROUP BY (deal_id, species, bucket) and consumer alloc_bucket matches that SAME (deal_id, species, bucket) key, bucket being '*' when the deal has one line of the species and the breed otherwise, and the numerator (line_bucket value) and denominator (bucket_count) range over the identical key set; join_cardinality=an allocation matches at most one bucket because for one (deal, species) the buckets are either the single '*' or distinct breeds, and bucket_count, unclaimed and unmatched_count are pre-aggregated to one row per key so each attaches 1:{0,1}; pagination=none, a whole-tenant input CTE consumed by bounded reads that page loads after pricing; scope=tenant_id on every branch
const saleLineShareCTEs = `
sale_alloc AS (
    SELECT a.goat_id, a.sales_deal_id, a.park_id, a.shed_id, a.partition_label,
           d.sale_date, d.buyer_name, (d.status = 'Deal Closed') AS closed,
           d.sales_value AS deal_value,
           NOT EXISTS (
               SELECT 1 FROM public.sales_deal_lines l
               WHERE l.tenant_id = d.tenant_id AND l.deal_id = d.id) AS lineless,
           lower(btrim(COALESCE(g.species, ''))) AS sp,
           lower(btrim(COALESCE(g.breed, ''))) AS br
    FROM public.goat_sale_allocations a
    JOIN public.sales_deals d ON d.tenant_id = a.tenant_id AND d.id = a.sales_deal_id
    JOIN public.goats g ON g.tenant_id = a.tenant_id AND g.goat_id = a.goat_id
    WHERE a.tenant_id = $1 AND a.status = 'tagged'
),
sale_line AS (
    SELECT l.deal_id,
           COALESCE(l.sales_value, 0) AS v,
           lower(btrim(COALESCE(c.species_code, l.product_code))) AS sp,
           lower(btrim(COALESCE(l.breed, ''))) AS br,
           count(*) OVER (PARTITION BY l.deal_id, lower(btrim(COALESCE(c.species_code, l.product_code)))) AS sp_lines
    FROM public.sales_deal_lines l
    LEFT JOIN public.sellable_product_catalog c
      ON c.tenant_id = l.tenant_id AND c.product_code = l.product_code
    WHERE l.tenant_id = $1 AND l.product_kind = 'animal'
),
line_bucket AS (
    SELECT deal_id, sp, CASE WHEN sp_lines = 1 THEN '*' ELSE br END AS bkt, sum(v) AS v
    FROM sale_line
    GROUP BY 1, 2, 3
),
alloc_bucket AS (
    SELECT sa.*, lb.sp AS b_sp, lb.bkt AS b_bkt, lb.v AS b_v
    FROM sale_alloc sa
    LEFT JOIN line_bucket lb
      ON lb.deal_id = sa.sales_deal_id AND lb.sp = sa.sp AND (lb.bkt = '*' OR lb.bkt = sa.br)
),
bucket_count AS (
    SELECT sales_deal_id, b_sp, b_bkt, count(*)::numeric AS n
    FROM alloc_bucket
    WHERE b_sp IS NOT NULL
    GROUP BY 1, 2, 3
),
unclaimed AS (
    SELECT lb.deal_id, sum(lb.v) AS v
    FROM line_bucket lb
    WHERE NOT EXISTS (
        SELECT 1 FROM bucket_count bc
        WHERE bc.sales_deal_id = lb.deal_id AND bc.b_sp = lb.sp AND bc.b_bkt = lb.bkt)
    GROUP BY lb.deal_id
),
unmatched_count AS (
    SELECT sales_deal_id, count(*)::numeric AS n
    FROM alloc_bucket
    WHERE b_sp IS NULL
    GROUP BY 1
),
animal_share AS (
    SELECT ab.goat_id, ab.sales_deal_id, ab.park_id, ab.shed_id, ab.partition_label,
           ab.sale_date, ab.buyer_name, ab.closed,
           (CASE
                WHEN NOT ab.closed THEN NULL
                WHEN ab.b_sp IS NOT NULL THEN CASE WHEN ab.b_v > 0 THEN ab.b_v / bc.n END
                WHEN ab.lineless THEN CASE WHEN ab.deal_value > 0 THEN ab.deal_value / uc.n END
                ELSE CASE WHEN u.v > 0 THEN u.v / uc.n END
            END)::float8 AS share
    FROM alloc_bucket ab
    LEFT JOIN bucket_count bc
      ON bc.sales_deal_id = ab.sales_deal_id AND bc.b_sp = ab.b_sp AND bc.b_bkt = ab.b_bkt
    LEFT JOIN unclaimed u ON u.deal_id = ab.sales_deal_id
    LEFT JOIN unmatched_count uc ON uc.sales_deal_id = ab.sales_deal_id
)`
