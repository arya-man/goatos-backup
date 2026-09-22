-- +goose Up
--
-- Leadership assistant coverage for PER-ANIMAL WEIGHT and GROWTH (plan v3 D2 /
-- P2 views wave 1: `weighing_latest_individual_weight`, `growth_adg_pairs` --
-- docs/ceo-ai/plan-v3-one-brain-two-doors.md, and the two PLANNED page rows in
-- docs/ceo-ai/coverage-matrix.md for `/weighing/weights` and
-- `/weighing/analytics`).
--
-- WHAT WAS MISSING. 000080 gave weighing two views, and both are at BUCKET
-- grain: `ceo_ai.weighing_capture_activity` is one row per campaign_shed bucket
-- and `ceo_ai.weighing_verification_status` is a park/shed proof rollup. Neither
-- carries a single animal's weighs, so there is no `ceo_ai.*` relation that can
-- answer "how has this kid grown", "what is our daily gain", or "which pens are
-- heaviest" -- the SQL tier reads `ceo_ai.*` only (sqlguard's core allowlist),
-- so those questions have no relation to read. The held-out eval confirms it:
-- goldens `weighing-adg-headline-month`, `weighing-adg-by-breed` and
-- `weighing-heaviest-pens-table` are all `pending_view`. These two views close
-- that gap at the two grains the pages actually use: LATEST WEIGHT PER ANIMAL,
-- and CONSECUTIVE PAIRS OF WEIGHS.
--
-- WEIGHING STAYS FREE-FLOW. 000078 DROPPED `weighing_observations.animal_id` and
-- 000079 dropped `weighing_expected_animals` outright; a scan has no expected
-- animal set and no roster denominator. NOTHING HERE REINTRODUCES EITHER: no
-- `animal_id` column is referenced, no expected/roster table is read, no
-- progress ratio against an expected count is computed, and the membership of
-- both views is the SCANS THAT HAPPENED. An animal's identity in weighing IS ITS
-- SCANNED TAG, exactly as 000073's duplicate guard and 000080's grain treat it.
--
-- WHY `goat_identifiers` IS JOINED HERE WHEN 000080 REFUSED TO. 000080 is a
-- CAPTURE view -- how much work was done in a bucket -- and joining the herd
-- there would have made the roster a precondition for counting a scan, which is
-- the coupling free-flow exists to prevent. These two views answer a different
-- question: how one ANIMAL grew, and how growth splits by BREED and SEX. Breed
-- and sex are not recorded on a scan and exist nowhere but the herd register, so
-- the alternative is not "a purer view", it is "the question stays unanswerable".
--
-- The join is the one the Growth Director already ships and the one the
-- maintainer has already reviewed: `backend/internal/growthdirector/adapters/postgres/growth_director.go`
-- (`breedSexJoin`) resolves breed/sex "EXCLUSIVELY through goat_identifiers ->
-- goats with a one-hop merged_into_goat_id redirect", matching
-- `gi.normalized_value = upper(tag_key)` because the identity module's
-- normalizer is UPPER(trim) while the weighing tag key is lower(btrim). It is
-- READ-ONLY DECORATION and it is a LEFT JOIN here, deliberately stricter than
-- the Growth Director's inner join: a scanned tag with no register row keeps its
-- row, its weight and its ADG, and reports breed/sex as NULL. The herd is never
-- a filter, never a denominator, and never a precondition -- it only names what
-- was weighed.
--
-- The join CANNOT FAN OUT: `goat_identifiers_lifetime_value_unique` is
-- UNIQUE (tenant_id, normalized_value) (000001 baseline), so at most one
-- identifier row exists per tenant per normalized tag, and `goats` and the merge
-- redirect are PK lookups. No status filter, for the reason the Growth Director
-- states: "a retired tag is still unambiguous for its lifetime".
--
-- TWO TAGS, ONE ANIMAL. An animal may carry two RFIDs (`goat_identifiers`
-- allows `animal_identifier_1` and `animal_identifier_2`), and one weighed on
-- its primary tag in one round and its secondary in the next would otherwise be
-- two animals with one weigh each -- no pair, no gain, and the interval between
-- those weighs never compared. `animal_key` is therefore the animal's OWN
-- CANONICAL TAG -- its `animal_identifier_1`, preferring the row flagged
-- primary -- and the normalized scanned tag itself when the register knows
-- nothing about it. A tag with no register row is its own animal, which is the
-- only honest reading of a tag nothing else knows about.
--
-- IT IS A TAG AND NEVER A `goat_id`, for the reason AGENTS.md records against
-- `identity_scope.go`: "the canonical key is one of the animal's OWN tags ...
-- never a goat_id, because `animal_key` is rendered verbatim to a reader."
-- Keying on the uuid was tried here first and the live assistant duly printed
-- `91000000-0000-4000-8000-000000001001` at a CEO as the name of an animal --
-- an internal id the farm cannot match to anything it can hold, and the exact
-- thing the RFID rule at the end of AGENTS.md forbids. The canonical tag folds
-- the two RFIDs exactly as well and reads as what it is.
--
-- The canonical-tag pick is a deterministic ORDER BY over a set that is unique
-- by construction (`goat_identifiers_lifetime_value_unique` makes
-- normalized_value unique per tenant and is itself the final tiebreak), so it
-- is a stable CHOICE, not the "ORDER BY ... LIMIT 1 over rows the producer can
-- legitimately duplicate" shape the aggregate rule bans: a re-run cannot pick a
-- different tag for the same animal.
--
-- WHAT IS EXCLUDED FROM BOTH VIEWS, and why (the same three exclusions the
-- Growth Director applies, so the chat and the pages talk about the same kids):
--   * `verification_status = 'rework'` -- the proof bounced, the weight is not
--     trusted. Pending captures ARE included; they are real measurements whose
--     video has not been reviewed yet, and dropping them would empty the view
--     for any week still in review.
--   * campaigns with `status = 'canceled'` -- a canceled plan's scans are not
--     reporting facts.
--   * blank `scanned_identifier` -- free-flow requires a tag OR (historically) an
--     animal; a blank tag has no identity to attribute a weight or a gain to and
--     cannot be collapsed with other blank tags.
-- Per-shed lump-sum buckets (`weighing_shed_observations`) are ALSO out of
-- scope here BY GRAIN, not by oversight: a lump-sum bucket records ONE total for
-- a whole pen and has no per-animal row at all. Whole-shed weighs remain on
-- `ceo_ai.weighing_capture_activity` (`shed_weight_avg_kg`,
-- `shed_animal_count`), and the schema cards say so, because a headline that
-- silently dropped them would under-report the herd -- plan v3 records that
-- exact failure ("339/791 kids are lump-sum pens with no pairs").
--
-- EVERY ROW ON BOTH VIEWS IS AN INDIVIDUAL SCAN, so neither carries a
-- `weighing_category` column: a `per_shed_partition` bucket never writes
-- `weighing_observations` at all (000080's header states the two sources are
-- disjoint by the bucket's own category), so the column would be the constant
-- 'individual_animal' on every row and only invite a filter that changes nothing.
--
-- NO `scanned_tag` AND NO `origin_type` COLUMN on the latest-weight view either.
-- `animal_key` IS the canonical ear tag now, so a second near-identical tag
-- column only invites a reader to print the wrong one of the two; and the
-- Weights page's farm-born/purchased split is a PAGE filter resolved by
-- weighing's own origin_scope.go, which reads procurement tables this view may
-- not touch. `management_stage` took the space, because it is what makes a
-- question about KIDS answerable at all.
--
-- NO `goat_id` COLUMN, deliberately. sqlguard admits ONE ceo_ai relation per
-- statement, so a register uuid could not be joined to anything and would only
-- be a raw internal id the composer might print at a reader. The register's
-- contribution is carried as breed, sex, stage and origin, and `animal_key` is
-- the animal's own canonical TAG, which folds its two RFIDs onto one identity
-- and is a thing the farm can actually read off an ear. `prev_weight_kg` is likewise absent:
-- weight_kg minus gain_kg is the same number.
--
-- COLUMNS ARE DELIBERATELY NARROW. Every column is rendered into the planner's
-- schema card on every request, so a column no leadership question can ask for is
-- a standing token cost. The capture timestamp (the IST business day is on the
-- row), the campaign id, and the per-scan verification status are all reachable
-- on the weighing reads and on ceo_ai.weighing_verification_status, and stay
-- there.
--
-- LOCK SAFETY: CREATE VIEW only -- no table DDL, no backfill, no lock against
-- the hot weighing_observations write path.

SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '30s';

-- ===========================================================================
-- 1. ceo_ai.weighing_latest_individual_weight
--
-- _grain: ONE ROW PER ANIMAL IDENTITY (`animal_key`) that has at least one
-- accepted, non-rework individual scan -- carrying that animal's LATEST-EVER
-- weigh: the weight, the day, and the pen it was weighed in.
--
-- LATEST-EVER, NOT LATEST-IN-WINDOW. This is the sale-readiness reading plan v3
-- names ("latest-ever individual weigh ... >30/>35 kg (sale-readiness
-- definition)"): an animal that cleared 35 kg in August is sale-ready today
-- whether or not it was weighed again since. `weighed_on` is on the row and is
-- the card's date column, so a question that really is about a window ("kids
-- weighed this month") filters on it and gets the animals weighed IN that
-- window, at their latest weigh within it only if the window excludes later
-- ones -- which is why the column is a DATE on the row rather than a hidden
-- assumption inside the view.
--
-- THE 30 kg / 35 kg FLAGS ARE THE MAINTAINER'S FIXED SALE-READINESS EDGES, the
-- ones the `/weighing/weights` page contract carries as `kpi.over30` /
-- `kpi.over35`. They are booleans, not bands: the tenant's CONFIGURABLE weight
-- band edges live in `growth_assumptions` (`func:ValidWeightBandEdgesKg`,
-- excluded from assistant coverage as configuration), and baking a snapshot of
-- tenant config into a view would silently disagree with the page the day the
-- config changed. A banded answer groups on `weight_kg` with the edges the
-- caller states.
--
-- REPEAT SCANS COLLAPSE TO THE NEWEST. `weighing_observations` keeps history
-- rather than deleting (000061 stamps submitted_at, 000073 collapses duplicate
-- losers, a reopened bucket re-inserts), so a bare read of the table reports an
-- animal more than once. `DISTINCT ON (tenant_id, animal_key)` ordered by
-- `accepted_at DESC, observation_id DESC` is the same "newest capture per tag
-- wins" rule 000073 chose and 000080's LATERAL applies.
--
-- projection-review: membership=weighing_observations rows with a non-blank scanned_identifier, verification_status <> 'rework', on a campaign whose status <> 'canceled', collapsed by DISTINCT ON to the newest capture per (tenant_id, animal_key); group_key=(tenant_id, animal_key), the DISTINCT ON key -- one output row per animal identity, by construction; join_cardinality=weighing_campaigns 1 per campaign_id (PK), weighing_campaign_sheds 0..1 per campaign_shed_id (PK), locations pk 0..1 per location_id (PK), goat_identifiers 0..1 per (tenant_id, normalized_value) (UNIQUE goat_identifiers_lifetime_value_unique), goats 0..1 (PK), the merge redirect 0..1 (PK) -- every joined side is 0..1, so no side can multiply an observation, and the DISTINCT ON collapses to one row per animal regardless; pagination=NONE, this is a view and every consumer paginates over it; scope=tenant_id, exposed as o.tenant_id, with EVERY join keyed on tenant_id as well (o.tenant_id = gi.tenant_id = g.tenant_id), so no relation can cross a tenant boundary
--
-- Ratio key sets: this view computes NO ratio. is_over_30_kg / is_over_35_kg are per-row predicates on that animal's own latest weight, so counting them and counting the view's rows range over the IDENTICAL key set (one row per animal) and a share is a plain count ratio over one population. An AVERAGE weight over these rows is an average of one-weight-per-animal and is therefore animal-weighted by construction -- it is not an average of averages.
-- ===========================================================================
CREATE OR REPLACE VIEW ceo_ai.weighing_latest_individual_weight AS
SELECT DISTINCT ON (o.tenant_id, COALESCE(lower(ck.canonical_tag), lower(btrim(o.scanned_identifier))))
    o.tenant_id                                          AS tenant_id,
    COALESCE(lower(ck.canonical_tag), lower(btrim(o.scanned_identifier)))  AS animal_key,
    -- Scans are accepted on the phone in IST; the business day is the IST day of
    -- acceptance, converted here rather than assumed (accepted_at IS a
    -- timestamptz, unlike sales_deals.sale_date which is already a stored date).
    (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date    AS weighed_on,
    o.weight_kg                                          AS weight_kg,
    (o.weight_kg > 30)                                   AS is_over_30_kg,
    (o.weight_kg > 35)                                   AS is_over_35_kg,
    COALESCE(pk.name, '')                                AS park_label,
    cs.location_id                                       AS shed_id,
    cs.display_name                                      AS shed_label,
    -- 'whole' is a STORAGE key and must never reach a user
    -- (docs/decisions/operational-location-convention.md); an undivided shed
    -- reports NULL here and is named by shed_label alone. Composition of the
    -- user-facing `Godel 1 - Part 3` string stays with the backend/client
    -- helpers (platform/oploc), exactly as on every other ceo_ai view -- no
    -- ceo_ai view composes it in SQL, and a second composer is how the doubled
    -- `Castro 1 1` bug gets reintroduced.
    NULLIF(cs.partition_label, 'whole')                  AS partition_label,
    COALESCE(gc.breed, g.breed)                          AS breed,
    COALESCE(gc.sex, g.sex)                              AS sex,
    -- What the farm CALLS this animal (K0..K4, fattening, bucks). Without it a
    -- question about KIDS is unanswerable -- the live assistant said exactly
    -- that -- and both page contracts chart daily gain on it.
    COALESCE(gc.management_stage, g.management_stage)     AS management_stage
FROM weighing_observations o
JOIN weighing_campaigns c
  ON c.tenant_id = o.tenant_id
 AND c.campaign_id = o.campaign_id
LEFT JOIN weighing_campaign_sheds cs
  ON cs.tenant_id = o.tenant_id
 AND cs.campaign_shed_id = o.campaign_shed_id
LEFT JOIN locations pk
  ON pk.tenant_id = o.tenant_id
 AND pk.location_id = c.park_id
LEFT JOIN goat_identifiers gi
  ON gi.tenant_id = o.tenant_id
 AND gi.normalized_value = upper(lower(btrim(o.scanned_identifier)))
LEFT JOIN goats g
  ON g.tenant_id = o.tenant_id
 AND g.goat_id = gi.goat_id
LEFT JOIN goats gc
  ON gc.tenant_id = o.tenant_id
 AND gc.goat_id = g.merged_into_goat_id
LEFT JOIN LATERAL (
    -- The animal's OWN canonical tag. Bounded to the one animal the scanned tag
    -- already resolved to, so it reads no wider than the joins above it.
    SELECT ci.normalized_value AS canonical_tag
    FROM goat_identifiers ci
    WHERE ci.tenant_id = o.tenant_id
      AND ci.goat_id = COALESCE(gc.goat_id, g.goat_id)
      AND ci.status = 'active'
    ORDER BY (ci.identifier_type = 'animal_identifier_1') DESC,
             ci.is_primary_for_goat DESC,
             ci.normalized_value
    LIMIT 1
) ck ON true
WHERE btrim(o.scanned_identifier) <> ''
  AND o.verification_status <> 'rework'
  AND c.status <> 'canceled'
ORDER BY
    o.tenant_id,
    COALESCE(lower(ck.canonical_tag), lower(btrim(o.scanned_identifier))),
    o.accepted_at DESC,
    o.observation_id DESC;

-- ===========================================================================
-- 2. ceo_ai.growth_adg_pairs
--
-- _grain: ONE ROW PER CONSECUTIVE PAIR OF CAMPAIGN ROUNDS for one animal
-- identity -- the animal's weigh in one round and its weigh in the NEXT round it
-- appears in, with the interval and the gain between them. An animal weighed in
-- four rounds contributes three rows; an animal weighed once contributes none
-- (there is no interval to measure).
--
-- ONE WEIGH PER ROUND FIRST. Repeat scans within a round collapse to the newest
-- capture (`DISTINCT ON (animal_key, campaign_id)`, the 000073 grain and the
-- 000080 convention) BEFORE pairing, so a re-scan inside one bucket can never
-- become a zero-day "pair" against itself.
--
-- NEVER AVERAGE THIS COLUMN FOR A HEADLINE. `adg_g_per_day` is already a rate,
-- and plan v3 records the measured consequence of averaging it: "Headline =
-- animal-weighted mean of per-animal period medians + whole-shed spans ...
-- `AVG(pairs)` reproduces the exact '133 g vs 200 g' bug killed on 2026-08-26".
-- A pair over 7 days and a pair over 70 days are not two equal observations, and
-- an animal with four rounds is not three times the animal with two. The
-- headline figure belongs to the `weighing_growth` Go reader; THIS view answers
-- the questions that are genuinely at pair grain -- which kids are losing
-- weight, gain thresholds, worst pens, and by-dimension breakdowns -- and it
-- carries `gain_kg` and `days_between` so a defensible aggregate is
-- `sum(gain_kg) * 1000 / sum(days_between)` (a duration-weighted rate) rather
-- than `avg(adg_g_per_day)`. The schema card repeats this in the planner prompt
-- and lists the column under NeverAverage so the guard-facing contract says it
-- too.
--
-- SAME-DAY PAIRS ARE EXCLUDED (`days_between > 0`): dividing by zero days is not
-- a growth rate, and the Growth Director drops them for the same reason
-- ("same-day pairs carry no growth signal"). IMPLAUSIBLE LOSSES ARE NOT
-- excluded here, unlike the Growth Director's `-$12 g/day` bad-scan cut-off:
-- that threshold is TENANT CONFIGURATION (`growth_assumptions.BadScanLossGPerDay`),
-- and a view cannot read a caller's settings. Filtering on a hardcoded copy
-- would disagree with the page the day the setting changed, and filtering
-- silently would hide bad scales from the person who needs to hear about them.
-- The raw pair is reported; `adg_g_per_day` is signed, and a question about
-- losing kids filters on it explicitly.
--
-- SHED ATTRIBUTION IS THE LATER WEIGH'S PEN, so a kid shifted mid-period counts
-- in the pen it is in now -- the same choice `firstLastPairCTE` makes
-- ("attributing the shed from the LATEST round").
--
-- projection-review: membership=the round-collapsed weighs of ceo_ai.weighing_latest_individual_weight's own source population (non-blank tag, non-rework, non-canceled campaign), paired to their immediate predecessor by lag() within each animal_key; group_key=(tenant_id, animal_key, observation_id) -- the LATER observation of the pair identifies the row uniquely, since lag() emits at most one predecessor per row; join_cardinality=identical to view 1 and bounded by the same keys: campaigns 1 (PK), campaign_sheds 0..1 (PK), locations 0..1 (PK), goat_identifiers 0..1 (UNIQUE (tenant_id, normalized_value)), goats 0..1 (PK), merge redirect 0..1 (PK) -- no join can multiply a round, and the window function adds no rows; the WHERE on prev_observation_id IS NOT NULL drops each animal's first round, which is why an n-round animal yields n-1 rows and not n; pagination=NONE, this is a view and every consumer paginates over it; scope=tenant_id, exposed as tenant_id, carried through the round CTE and present on every join predicate, and the window PARTITION includes tenant_id so one tenant's round can never be paired with another's
--
-- Ratio key sets: adg_g_per_day = (weight_kg - prev_weight_kg) * 1000 / days_between, all three from the SAME PAIR ROW -- one key set, no cross-grain division -- and days_between is guaranteed > 0 by the WHERE, so the division cannot be by zero. gain_kg and days_between are the un-divided numerator and denominator, carried so an aggregate over several pairs re-divides sums instead of averaging rates: over any set of pairs, sum(gain_kg) * 1000 / sum(days_between) has numerator and denominator ranging over that identical set, which avg(adg_g_per_day) does not.
-- ===========================================================================
CREATE OR REPLACE VIEW ceo_ai.growth_adg_pairs AS
WITH rounds AS (
    SELECT DISTINCT ON (o.tenant_id, COALESCE(lower(ck.canonical_tag), lower(btrim(o.scanned_identifier))), o.campaign_id)
        o.tenant_id,
        COALESCE(lower(ck.canonical_tag), lower(btrim(o.scanned_identifier))) AS animal_key,
        COALESCE(gc.goat_id, g.goat_id)                   AS goat_id,
        lower(btrim(o.scanned_identifier))                AS scanned_tag,
        o.campaign_id,
        c.period_start_date,
        o.observation_id,
        o.accepted_at,
        o.weight_kg,
        cs.location_id                                    AS shed_id,
        cs.display_name                                   AS shed_label,
        NULLIF(cs.partition_label, 'whole')               AS partition_label,
        cs.weighing_category,
        COALESCE(pk.name, '')                             AS park_label,
        COALESCE(gc.breed, g.breed)                       AS breed,
        COALESCE(gc.sex, g.sex)                           AS sex,
        COALESCE(gc.management_stage, g.management_stage) AS management_stage
    FROM weighing_observations o
    JOIN weighing_campaigns c
      ON c.tenant_id = o.tenant_id
     AND c.campaign_id = o.campaign_id
    LEFT JOIN weighing_campaign_sheds cs
      ON cs.tenant_id = o.tenant_id
     AND cs.campaign_shed_id = o.campaign_shed_id
    LEFT JOIN locations pk
      ON pk.tenant_id = o.tenant_id
     AND pk.location_id = c.park_id
    LEFT JOIN goat_identifiers gi
      ON gi.tenant_id = o.tenant_id
     AND gi.normalized_value = upper(lower(btrim(o.scanned_identifier)))
    LEFT JOIN goats g
      ON g.tenant_id = o.tenant_id
     AND g.goat_id = gi.goat_id
    LEFT JOIN goats gc
      ON gc.tenant_id = o.tenant_id
     AND gc.goat_id = g.merged_into_goat_id
    LEFT JOIN LATERAL (
        -- The animal's OWN canonical tag. Bounded to the one animal the scanned tag
        -- already resolved to, so it reads no wider than the joins above it.
        SELECT ci.normalized_value AS canonical_tag
        FROM goat_identifiers ci
        WHERE ci.tenant_id = o.tenant_id
          AND ci.goat_id = COALESCE(gc.goat_id, g.goat_id)
          AND ci.status = 'active'
        ORDER BY (ci.identifier_type = 'animal_identifier_1') DESC,
                 ci.is_primary_for_goat DESC,
                 ci.normalized_value
        LIMIT 1
    ) ck ON true
    WHERE btrim(o.scanned_identifier) <> ''
      AND o.verification_status <> 'rework'
      AND c.status <> 'canceled'
    ORDER BY
        o.tenant_id,
        COALESCE(lower(ck.canonical_tag), lower(btrim(o.scanned_identifier))),
        o.campaign_id,
        o.accepted_at DESC,
        o.observation_id DESC
),
paired AS (
    SELECT
        r.*,
        lag(r.observation_id) OVER w AS prev_observation_id,  -- pairing only; not projected
        lag(r.accepted_at)    OVER w AS prev_accepted_at,
        lag(r.weight_kg)      OVER w AS prev_weight_kg
    FROM rounds r
    WINDOW w AS (
        PARTITION BY r.tenant_id, r.animal_key
        ORDER BY r.period_start_date, r.accepted_at, r.observation_id
    )
)
SELECT
    p.tenant_id                                          AS tenant_id,
    p.animal_key                                         AS animal_key,
    (p.accepted_at AT TIME ZONE 'Asia/Kolkata')::date       AS weighed_on,
    ((p.accepted_at AT TIME ZONE 'Asia/Kolkata')::date
        - (p.prev_accepted_at AT TIME ZONE 'Asia/Kolkata')::date)  AS days_between,
    p.weight_kg                                          AS weight_kg,
    (p.weight_kg - p.prev_weight_kg)                     AS gain_kg,
    ((p.weight_kg - p.prev_weight_kg) * 1000.0
        / ((p.accepted_at AT TIME ZONE 'Asia/Kolkata')::date
            - (p.prev_accepted_at AT TIME ZONE 'Asia/Kolkata')::date))  AS adg_g_per_day,
    p.park_label                                         AS park_label,
    p.shed_id                                            AS shed_id,
    p.shed_label                                         AS shed_label,
    p.partition_label                                    AS partition_label,
    p.breed                                              AS breed,
    p.sex                                                AS sex,
    p.management_stage                                   AS management_stage
FROM paired p
WHERE p.prev_observation_id IS NOT NULL
  AND (p.accepted_at AT TIME ZONE 'Asia/Kolkata')::date
    > (p.prev_accepted_at AT TIME ZONE 'Asia/Kolkata')::date;

-- ===========================================================================
-- Grants, AFTER both CREATE VIEW statements and guarded on role existence --
-- the 000001 baseline block's shape, and the lesson 000080's header records.
-- ===========================================================================
-- +goose StatementBegin
DO $weighing_growth_ceo_ai_grants$
DECLARE
    r text;
BEGIN
    FOREACH r IN ARRAY ARRAY['mesha_ceo_readonly','mesha_cube_readonly'] LOOP
        IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = r) THEN
            EXECUTE format('GRANT SELECT ON ceo_ai.weighing_latest_individual_weight TO %I', r);
            EXECUTE format('GRANT SELECT ON ceo_ai.growth_adg_pairs TO %I', r);
        END IF;
    END LOOP;
END;
$weighing_growth_ceo_ai_grants$;
-- +goose StatementEnd

-- +goose Down
DROP VIEW IF EXISTS ceo_ai.growth_adg_pairs;
DROP VIEW IF EXISTS ceo_ai.weighing_latest_individual_weight;
