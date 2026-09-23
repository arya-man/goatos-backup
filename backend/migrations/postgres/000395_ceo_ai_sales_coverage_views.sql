-- +goose Up
--
-- Leadership assistant coverage for SALES (plan v3 D2 / P2 views wave 1:
-- `sales_deal_lines_closed`, `sales_buyer_summary` --
-- docs/ceo-ai/plan-v3-one-brain-two-doors.md, and the two PLANNED page rows in
-- docs/ceo-ai/coverage-matrix.md for `/sales/sold` and `/sales/buyer-analytics`).
--
-- WHAT WAS MISSING. `docs/ceo-ai/coverage-matrix.md` carries `table:sales_deals`,
-- `table:sales_deal_lines` and `sales_deal_payments` as `api`-only rows with
-- "Cube/`ceo_ai` mapping is future work". There is therefore NO `ceo_ai.*`
-- relation behind sales at all, and the SQL tier can only read `ceo_ai.*`
-- (sqlguard's core allowlist), so a leadership question about WHO BOUGHT,
-- WHAT PRICE PER KG, or HOW MUCH IS STILL OWED has no relation to read and is
-- answered "not covered" -- a genuine coverage gap, confirmed by the held-out
-- eval (goldens `sales-price-per-kg-by-breed`, `sales-outstanding`, both
-- `pending_view`). These two views close it.
--
-- SALES OWNS ITS OWN TABLES (000173: "Sales therefore owns its own tables and
-- reads NOTHING from the herd, vaccination, or procurement schemas"). BOTH
-- views honour that lock: they read `public.sales_deals` and
-- `public.sales_deal_lines` ONLY. In particular `procurement_vendors` is NOT
-- joined -- see the buyer-identity note on view 2 -- and no herd table is
-- touched, so a sale is never re-described by a later edit to a register row.
--
-- PII. `procurement_vendors.phone_number` is the buyer's personal phone and is
-- the reason the coverage matrix states "Phone numbers on that read are PII and
-- never reach the assistant (the P2 view carries no phone column)". Neither view
-- has a phone, contact-person, place or address column.
--
-- COLUMNS ARE DELIBERATELY NARROW. Every column here is rendered into the
-- planner's schema card, so a column a leadership question cannot ask for costs
-- tokens on every request. The deal's sex split, its line id, its status (always
-- 'Deal Closed' on this view, by its own WHERE clause) and the buyer's place are
-- reachable on `GET /sales/deals` and stay there. `line_no` is the one
-- non-question column kept: it is the view's unique key at its own grain and
-- every consumer pages on it (see the projection note on view 1).

--
-- MONEY IS NOT DERIVED FROM STATUS AND STATUS IS NOT DERIVED FROM MONEY (000227):
-- `status` is the deal's human lifecycle and `payment_received` is the running
-- receipts total maintained in the same transaction as each
-- `sales_deal_payments` insert. Both views read the MAINTAINED TOTAL rather than
-- joining the receipts table, which is also why neither can fan a deal out
-- across its receipts.
--
-- LOCK SAFETY: CREATE VIEW only -- no table DDL, no backfill, no lock against
-- the sales write path. SET LOCAL lock_timeout/statement_timeout still bound the
-- DDL acquisition in case a long-running transaction on these tables is
-- mid-flight.

SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '30s';

-- ===========================================================================
-- 1. ceo_ai.sales_deal_lines_closed
--
-- _grain: ONE ROW PER LINE OF A CLOSED DEAL -- i.e. one row per
-- public.sales_deal_lines row whose deal is `status = 'Deal Closed'`, plus ONE
-- SYNTHESISED LINE for a closed deal that has no line rows at all. This is the
-- grain the ₹/kg-by-breed question needs: 000296 moved product/breed/weight/
-- value to the line "ON PURPOSE: the Sold page's price-per-kg bands are keyed by
-- (product, breed), and a single lump value over a mixed sale cannot be divided
-- between two breeds honestly".
--
-- WHY THE SYNTHESISED LINE, AND WHY IT CANNOT DOUBLE-COUNT. 000296 backfilled
-- exactly one line for every deal that existed, and the write path writes lines
-- for every deal since. A closed deal with zero lines is therefore not expected
-- today -- but if one ever appears (a partial write, a future importer), a plain
-- `JOIN sales_deal_lines` would silently DROP its revenue from every ₹ total on
-- this view while `/sales/overview` (which reads the deal row) kept reporting
-- it, and the two surfaces would disagree about the money. The LEFT JOIN + the
-- deal's own columns is the honest reading of "every deal recorded so far IS one
-- line". It cannot double-count: the line columns are read when a line exists
-- and the DEAL columns only when `l.line_id IS NULL` -- PER ROW, never per
-- column, for the reason the projection notes beside `product_type` record -- and
-- the deal-level rollup 000296 maintains means sum(line values) = deal value, so
-- either reading of a deal totals the same money.
--
-- DEAL-LEVEL MONEY REPEATS ON EVERY LINE -- READ `is_deal_primary_line`.
-- `deal_sales_value`, `deal_payment_received` and `deal_outstanding_rupees` are
-- properties of the TRANSACTION, not of the line: a receipt is paid against the
-- whole deal and 000296 is explicit that "the advance and the receipts belong to
-- the whole deal, not to one breed of it". They are carried here so a ₹/kg
-- answer can state what is still owed on the same deal without a second read,
-- and they REPEAT identically on each of a mixed deal's lines. Summing them
-- across lines multiplies a two-line deal's receivable by two. `is_deal_primary_line`
-- is true on exactly one line per deal (the lowest `line_no` -- the SAME
-- `line_no` this view projects, so a reader can verify the flag instead of
-- taking it on trust; it is 1 by the write path and by 000296's backfill, and is
-- resolved by window function here rather than assumed), so the correct
-- deal-grain total is
-- `sum(deal_outstanding_rupees) FILTER (WHERE is_deal_primary_line)` -- or,
-- better, ceo_ai.sales_buyer_summary, which is already at a grain where money
-- sums plainly. The schema card names both facts.
--
-- ₹/kg IS PER LINE AND ONLY FOR ANIMALS. `price_per_kg` is this line's own
-- `sales_value / total_weight_kg`, NULL when the weight is absent or zero (a
-- manure deal records no weight, and 000173 keeps counts/weights nullable
-- because the sheet does). It is ALREADY A RATIO: averaging it across lines
-- weights a 3 kg line the same as a 300 kg one. A ₹/kg figure over a set of
-- lines is `sum(sales_value) / sum(total_weight_kg)` over those lines, which is
-- why both numerator and denominator are on the row. `is_animal_line` separates
-- Sheep/Goat from Manure so a per-kg answer is not diluted by manure tonnage;
-- it is a per-LINE product test, which is exactly why the line grain exists
-- (the deal-level `product_type` may read 'Mixed' since 000296).
--
-- `sale_date` is already a DATE recorded in IST business terms by the sales desk
-- (000173) -- it is NOT a timestamp, so there is no timezone conversion to do
-- and none is done; inventing an `AT TIME ZONE` shift over a stored date would
-- move deals between months. A monthly series is date_trunc('month', sale_date)
-- over this column; a stored month key would only repeat it.
--
-- projection-review: membership=public.sales_deal_lines rows whose deal is status='Deal Closed' (the WHERE is on the deal side and runs before any projection), UNIONed in-place by the LEFT JOIN with the closed deals that have no line row at all; group_key=NONE -- this view has no GROUP BY, it is line-grain detail and every consumer aggregates over it -- but it DOES expose a unique key at its own grain, (tenant_id, deal_id, line_no), projected as `line_no`: unique on the line side by sales_deal_lines_deal_line_no_uq (000296), and the one synthesised row of a line-less deal takes the defined value 1, so the key is total and unique over EVERY row of the view; join_cardinality=sales_deal_lines 0..N per deal BY DESIGN (that IS the grain: one output row per line, never more), and NOTHING else is joined -- no vendor register, no receipts table, no herd table -- so no OTHER relation can multiply a line -- the window function computing is_deal_primary_line partitions by (tenant_id, deal_id) and adds no rows; pagination=NONE, this is a view and every consumer paginates over it -- on the projected (tenant_id, deal_id, line_no) keyset, which TestSalesLinesPageBoundaryReturnsEachLineExactlyOnce walks two rows at a time across a boundary that falls INSIDE a mixed deal and proves returns every line exactly once; scope=tenant_id, exposed as d.tenant_id, and the line join is keyed on BOTH tenant_id and deal_id so a line can never attach to another tenant's deal
--
-- Ratio key sets: price_per_kg has numerator sales_value and denominator total_weight_kg drawn from the SAME LINE ROW -- one key set, no cross-grain division -- and is NULL rather than 0 when the denominator is absent or zero, so an unweighed line is excluded from a ₹/kg answer instead of dragging it to zero. deal_outstanding_rupees = greatest(deal_sales_value - deal_payment_received, 0), both columns of the SAME DEAL ROW, matching `greatest(r.sales_value - r.payment_received, 0)` in procurement/adapters/postgres/buyer_analytics_repository.go so this view and the Buyer analytics page compute a receivable identically; it is clamped at 0 because an overpayment is a credit, not a negative debt, and must not net off another deal's arrears.
-- ===========================================================================
CREATE OR REPLACE VIEW ceo_ai.sales_deal_lines_closed AS
SELECT
    d.tenant_id                                          AS tenant_id,
    d.id                                                 AS deal_id,
    -- THE VIEW'S OWN UNIQUE KEY, AT ITS OWN GRAIN: (tenant_id, deal_id, line_no).
    -- `sales_deal_lines_deal_line_no_uq UNIQUE (tenant_id, deal_id, line_no)`
    -- (000296) makes it unique on the line side, and a deal with NO line row
    -- contributes exactly one synthesised row, which takes the defined value 1 --
    -- the same value 000296's backfill gave every real single-line deal, and a
    -- legal one under `sales_deal_lines_line_no_positive CHECK (line_no >= 1)`.
    -- It is projected because `pagination=NONE` means EVERY consumer pages over
    -- this view, and a keyset page is only stable on a key that is unique at the
    -- grain being paged: ordering on (deal_id, sale_date) or (deal_id, breed)
    -- repeats or skips a line of a mixed deal at the page boundary. It is also
    -- what makes `is_deal_primary_line` below checkable by a reader rather than
    -- a claim about a column the reader cannot select.
    -- NOT A DIMENSION: line_no is the desk's data-entry order, not a business
    -- fact -- page and de-duplicate on it, never GROUP BY it.
    COALESCE(l.line_no, 1)                               AS line_no,
    d.sale_date                                          AS sale_date,
    d.farm                                               AS farm,
    d.buyer_name                                         AS buyer_label,
    -- The matching key the Buyer analytics page folds typed names on
    -- (procurement/domain.NormalizeBuyerName: whitespace collapsed, lower case),
    -- spelled in SQL exactly as buyer_analytics_repository.go spells it, so this
    -- view and that page group the same deals under the same buyer.
    lower(regexp_replace(btrim(d.buyer_name), '\s+', ' ', 'g'))  AS buyer_key,
    -- THE FALLBACK IS PER ROW, NOT PER COLUMN. The discriminator is
    -- `l.line_id IS NULL` -- "this deal has no line at all" -- never COALESCE
    -- on the value. animal_count and total_weight_kg are NULLABLE on a line
    -- (000173 keeps them nullable because the sheet does, and a MANURE line
    -- carries no animals by definition), so a per-column COALESCE silently
    -- filled a manure line with the WHOLE DEAL's head count: a two-line deal of
    -- 12 animals reported 8 on its goat line and 12 more on its manure line,
    -- counting the herd twice and inventing animals in a product that has none.
    -- Caught on a live fixture; the CASE is what keeps a present-but-empty line
    -- value empty.
    CASE WHEN l.line_id IS NULL THEN d.product_type ELSE l.product_type END  AS product_type,
    CASE WHEN l.line_id IS NULL THEN d.breed ELSE l.breed END                AS breed,
    ((CASE WHEN l.line_id IS NULL THEN d.product_type ELSE l.product_type END)
        IN ('Sheep', 'Goat'))                                                AS is_animal_line,
    CASE WHEN l.line_id IS NULL THEN d.animal_count ELSE l.animal_count END  AS animal_count,
    CASE WHEN l.line_id IS NULL THEN d.total_weight_kg ELSE l.total_weight_kg END AS total_weight_kg,
    CASE WHEN l.line_id IS NULL THEN d.sales_value ELSE l.sales_value END    AS sales_value,
    -- Already a ratio; never average it across rows (see the header).
    ((CASE WHEN l.line_id IS NULL THEN d.sales_value ELSE l.sales_value END)
        / NULLIF(CASE WHEN l.line_id IS NULL THEN d.total_weight_kg ELSE l.total_weight_kg END, 0))
                                                                             AS price_per_kg,
    -- Deal-grain money. Repeats on every line of the deal: aggregate it only
    -- under `FILTER (WHERE is_deal_primary_line)`.
    d.sales_value                                        AS deal_sales_value,
    COALESCE(d.payment_received, 0)                      AS deal_payment_received,
    GREATEST(d.sales_value - COALESCE(d.payment_received, 0), 0)  AS deal_outstanding_rupees,
    (COALESCE(l.line_no, 1) = MIN(COALESCE(l.line_no, 1)) OVER (PARTITION BY d.tenant_id, d.id))
                                                         AS is_deal_primary_line
FROM sales_deals d
LEFT JOIN sales_deal_lines l
  ON l.tenant_id = d.tenant_id
 AND l.deal_id = d.id
WHERE d.status = 'Deal Closed';

-- ===========================================================================
-- 2. ceo_ai.sales_buyer_summary
--
-- _grain: ONE ROW PER (tenant_id, buyer_key) over CLOSED deals -- one row per
-- counterparty, which is the grain at which money sums plainly: deals, animals,
-- weight, revenue, received and outstanding, with the first and last sale date
-- and the repeat flag. This is the relation behind "how much do buyers still owe
-- us" (golden `sales-outstanding`) and "who are our repeat buyers".
--
-- BUYER IDENTITY IS THE DEAL'S OWN NORMALIZED BUYER NAME, AND THAT IS A
-- DELIBERATE, DOCUMENTED DIFFERENCE FROM THE PAGE. `GET /procurement/buyer-analytics`
-- resolves each deal to a `procurement_vendors` row -- directly by
-- `buyer_vendor_id`, else by matching the normalized business name when exactly
-- one register row holds it -- and groups by the resolved vendor. That read
-- lives in the PROCUREMENT module and may join the register; this view is a
-- SALES relation and 000173's lock forbids it reading the procurement schema, so
-- it folds on `buyer_key`, the same `lower(regexp_replace(btrim(buyer_name), …))`
-- normalization `domain.NormalizeBuyerName` applies, which 000215 introduced the
-- vendor column to fix ("'Ramesh Traders' / 'ramesh traders' / 'Ramesh' were
-- three buyers on the buyer board" -- the first two fold here, the third does
-- not).
--
-- CONSEQUENCE, stated rather than hidden: a counterparty RENAMED in the register
-- reads as TWO buyers here (its deals carry two different snapshot names --
-- 000215 keeps `buyer_name` a snapshot precisely so an old sale does not
-- re-describe itself), where the page shows one. Company-wide totals -- revenue,
-- outstanding, animals -- are IDENTICAL either way, because every closed deal
-- belongs to exactly one row on both sides; only the per-buyer split and the
-- repeat-buyer count can differ, and only for a renamed counterparty.
-- The schema card carries this caveat and the coverage matrix row names the page
-- as the authority for the register-folded buyer board.
--
-- ANIMALS ARE COUNTED FROM THE LINES, SHEEP AND GOAT ONLY, exactly as
-- buyer_analytics_repository.go counts them:
-- `coalesce(animal_count, coalesce(male_count,0) + coalesce(female_count,0))`
-- per animal line, falling back to the deal's own columns when the deal has no
-- lines. Manure carries no animals and must not contribute a count.
--
-- `is_repeat_buyer` is `deals >= 2`, the page's own repeat definition
-- (`Purchases >= 2` in procurement/domain/buyer_analytics.go).
--
-- projection-review: membership=closed deals, read through ceo_ai.sales_deal_lines_closed's own membership via a per-deal line rollup that is collapsed BEFORE the buyer aggregate, so a mixed deal contributes ONE deal row and not one per line; group_key=(d.tenant_id, buyer_key), exactly the GROUP BY list; join_cardinality=the `lines` LATERAL is a scalar aggregate over sales_deal_lines correlated on (tenant_id, deal_id) and returns EXACTLY ONE row per deal, so count(*) stays at DEAL grain and sum(d.sales_value) cannot be multiplied by a deal's line count -- nothing else is joined; pagination=NONE, this is a view and every consumer paginates over it; scope=tenant_id, grouped and exposed as d.tenant_id, with the LATERAL keyed on tenant_id as well
--
-- Ratio key sets: outstanding_rupees is sum(greatest(sales_value - payment_received, 0)) over the buyer's OWN closed deals -- the same key set that produces revenue_rupees and deals, clamped PER DEAL rather than after summing, so one buyer's overpaid deal cannot cancel another of its arrears (netting after the sum would silently understate the receivable, which is the figure a CEO acts on). payment_received_rupees is the maintained running total from sales_deals (000227), NOT a join to sales_deal_payments, so the receipts table cannot fan a deal out across its instalments. is_repeat_buyer ranges over the identical grouped key set as deals.
-- ===========================================================================
CREATE OR REPLACE VIEW ceo_ai.sales_buyer_summary AS
SELECT
    d.tenant_id                                                   AS tenant_id,
    lower(regexp_replace(btrim(d.buyer_name), '\s+', ' ', 'g'))   AS buyer_key,
    -- The most recent spelling the desk used for this buyer, so the answer names
    -- the counterparty the way the latest deal did rather than the way the
    -- oldest one did.
    (array_agg(d.buyer_name ORDER BY d.sale_date DESC, d.id DESC))[1]   AS buyer_label,
    count(*)::bigint                                              AS deals,
    (count(*) >= 2)                                               AS is_repeat_buyer,
    COALESCE(sum(lines.animals), 0)                               AS animals,
    COALESCE(sum(d.sales_value), 0)                               AS revenue_rupees,
    COALESCE(sum(COALESCE(d.payment_received, 0)), 0)             AS payment_received_rupees,
    COALESCE(sum(GREATEST(d.sales_value - COALESCE(d.payment_received, 0), 0)), 0)
                                                                  AS outstanding_rupees,
    min(d.sale_date)                                              AS first_sale_date,
    max(d.sale_date)                                              AS last_sale_date
FROM sales_deals d
-- EXACTLY ONE ROW PER DEAL. The rollup is a correlated scalar aggregate, so a
-- three-line deal enters the buyer aggregate once, with its three lines already
-- summed. A plain JOIN to sales_deal_lines here would count that deal three
-- times and treble its revenue -- the exact fan-out the line grain exists to
-- keep on the OTHER view.
LEFT JOIN LATERAL (
    SELECT
        sum(CASE WHEN l.product_type IN ('Sheep', 'Goat')
                 THEN COALESCE(l.animal_count, COALESCE(l.male_count, 0) + COALESCE(l.female_count, 0))
                 ELSE 0 END)                         AS animals
    FROM sales_deal_lines l
    WHERE l.tenant_id = d.tenant_id
      AND l.deal_id = d.id
) lines ON true
WHERE d.status = 'Deal Closed'
GROUP BY d.tenant_id, lower(regexp_replace(btrim(d.buyer_name), '\s+', ' ', 'g'));

-- ===========================================================================
-- Grants, AFTER both CREATE VIEW statements and guarded on role existence --
-- the 000001 baseline block's shape, and the lesson 000080's header records:
-- granting on a view before it exists aborts the whole migration with SQLSTATE
-- 42P01 on exactly the environments that HAVE the reader roles (staging,
-- production). mesha_ceo_readonly / mesha_cube_readonly are provisioned per
-- environment (tools/dev/setup-ceo-ai-local-role.sh + Secret Manager), never by
-- a migration. Idempotent and safe to re-run.
-- ===========================================================================
-- +goose StatementBegin
DO $sales_ceo_ai_grants$
DECLARE
    r text;
BEGIN
    FOREACH r IN ARRAY ARRAY['mesha_ceo_readonly','mesha_cube_readonly'] LOOP
        IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = r) THEN
            EXECUTE format('GRANT SELECT ON ceo_ai.sales_deal_lines_closed TO %I', r);
            EXECUTE format('GRANT SELECT ON ceo_ai.sales_buyer_summary TO %I', r);
        END IF;
    END LOOP;
END;
$sales_ceo_ai_grants$;
-- +goose StatementEnd

-- +goose Down
DROP VIEW IF EXISTS ceo_ai.sales_buyer_summary;
DROP VIEW IF EXISTS ceo_ai.sales_deal_lines_closed;
