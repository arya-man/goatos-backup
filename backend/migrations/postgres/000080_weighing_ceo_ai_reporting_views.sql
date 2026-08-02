-- +goose Up
--
-- Leadership assistant coverage for Weighing (WEIGHING-COVERAGE-01).
--
-- Today the CEO/CXO assistant cannot answer a single weighing question:
-- docs/ceo-ai/coverage-matrix.md records weighing as EXCLUDED except
-- `GET /weighing/campaigns`, with individual observations deferred as
-- "operational execution records". This migration closes that gap by adding
-- two `ceo_ai.*` reporting views over WEIGHING'S OWN TABLES ONLY.
--
-- HARD CONSTRAINT — WEIGHING IS FREE-FLOW AND FULLY ISOLATED (maintainer
-- decision, re-enforced 2026-08-03; see 000078/000079 and
-- tools/agent-hooks/check-weighing-free-flow-guard.mjs). A weighing scan has
-- no expected animal set. weighing_observations.animal_id was DROPPED
-- (000078); weighing_expected_animals was DROPPED OUTRIGHT (000079). These
-- views therefore NEVER join goats, goat_identifiers, herd_animals,
-- protocol_rules, or any vaccination/clinical table. They read ONLY:
--   weighing_campaigns, weighing_campaign_sheds, weighing_observations,
--   weighing_shed_observations, weighing_work_items, and verification_items
--   filtered to module = 'weighing'. `locations` is joined only for
--   park/shed display labels (the same non-herd label join every other
--   ceo_ai view uses).
--
-- LOCK SAFETY: this migration only CREATEs views (no table DDL, no backfill,
-- no lock against the hot weighing_observations/weighing_work_items write
-- paths). SET LOCAL lock_timeout/statement_timeout still bound the DDL
-- acquisition in case a concurrent long-running transaction on these tables
-- is mid-flight.

SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '30s';

-- ===========================================================================
-- 1. ceo_ai.weighing_capture_activity
--
-- _grain: ONE ROW PER weighing bucket, i.e. one row per
-- weighing_campaign_sheds.campaign_shed_id (the table's own primary key).
-- No fan-out is possible:
--   * weighing_work_items has a UNIQUE index on campaign_shed_id
--     (weighing_work_items_bucket_uidx) -- at most one row per bucket.
--   * weighing_shed_observations has a UNIQUE index on
--     (tenant_id, campaign_shed_id) (weighing_shed_observations_
--     one_active_scope_uidx) -- at most one row per bucket.
--   * the weighing_observations aggregate is pre-collapsed to one row per
--     campaign_shed_id via the LATERAL subquery's GROUP-free scalar
--     aggregation (COUNT/AVG/MIN/MAX over the whole bucket).
--
-- DISJOINT SOURCES, NOT DOUBLE-COUNTED: a campaign_shed's weighing_category
-- is fixed at creation ('individual_animal' or 'per_shed_partition') and the
-- write path only ever inserts into ONE of weighing_observations (per-animal
-- scans) or weighing_shed_observations (one lump-sum bucket total) for a
-- given campaign_shed_id -- never both. animals_weighed therefore
-- COALESCEs the two counts rather than summing them.
-- ===========================================================================
CREATE OR REPLACE VIEW ceo_ai.weighing_capture_activity AS
SELECT
    cs.tenant_id                                   AS tenant_id,
    pk.name                                        AS park_label,
    cs.display_name                                AS shed_label,
    cs.weighing_category                           AS weighing_category,
    cs.status                                       AS bucket_status,
    c.period_start_date                            AS period_start_date,
    c.period_end_date                              AS period_end_date,
    c.cadence_type                                 AS cadence_type,
    -- Work-item state (000059): one of scheduled/delayed/completed/closed/
    -- canceled. Disjoint by construction (work_state is a single column with
    -- a CHECK constraint enumerating exactly these five values), so counting
    -- buckets grouped by work_state always sums to the total bucket count.
    wi.work_state                                  AS work_state,
    wi.planned_business_date                       AS planned_business_date,
    wi.due_business_date                           AS due_business_date,
    wi.delayed_since_business_date                 AS delayed_since_business_date,
    wi.rolled_forward_count                        AS rolled_forward_count,
    -- Per-animal scan rollup (individual_animal buckets only; NULL/0 for
    -- per_shed_partition buckets, which never write weighing_observations).
    COALESCE(obs.scan_count, 0)::bigint             AS scan_count,
    obs.weight_avg_kg                              AS scan_weight_avg_kg,
    obs.weight_min_kg                              AS scan_weight_min_kg,
    obs.weight_max_kg                              AS scan_weight_max_kg,
    -- Per-shed lump-sum bucket total (per_shed_partition buckets only; NULL
    -- for individual_animal buckets, which never write
    -- weighing_shed_observations).
    sho.animal_count                               AS shed_animal_count,
    sho.average_weight_kg                          AS shed_weight_avg_kg,
    sho.weight_kg                                  AS shed_total_weight_kg,
    -- Unified "how many animals were weighed" count. Exactly one of
    -- obs.scan_count / sho.animal_count is non-null for any given bucket (see
    -- DISJOINT SOURCES note above), so COALESCE never double-counts.
    COALESCE(obs.scan_count, sho.animal_count, 0)::bigint AS animals_weighed
FROM weighing_campaign_sheds cs
JOIN weighing_campaigns c
  ON c.campaign_id = cs.campaign_id
LEFT JOIN locations pk
  ON pk.location_id = c.park_id
LEFT JOIN weighing_work_items wi
  ON wi.campaign_shed_id = cs.campaign_shed_id
LEFT JOIN LATERAL (
    SELECT
        count(*)                AS scan_count,
        avg(o.weight_kg)        AS weight_avg_kg,
        min(o.weight_kg)        AS weight_min_kg,
        max(o.weight_kg)        AS weight_max_kg
    FROM weighing_observations o
    WHERE o.campaign_shed_id = cs.campaign_shed_id
) obs ON true
LEFT JOIN weighing_shed_observations sho
  ON sho.campaign_shed_id = cs.campaign_shed_id;

GRANT SELECT ON ceo_ai.weighing_capture_activity TO mesha_ceo_readonly;
GRANT SELECT ON ceo_ai.weighing_capture_activity TO mesha_cube_readonly;

-- ===========================================================================
-- 2. ceo_ai.weighing_verification_status
--
-- _grain: ONE ROW PER (tenant_id, park_label, shed_label) -- a park/shed
-- verification rollup, same shape as the existing
-- ceo_ai.verification_queue_status but scoped to module = 'weighing' only
-- (weighing proof videos/photos), so weighing questions never get mixed into
-- another module's verification backlog.
--
-- verification_items.status has a CHECK constraint restricting it to exactly
-- {'pending','rejected','approved'} (000001 baseline). pending + rework
-- (rejected) + verified (approved) is therefore always EXACTLY total -- the
-- three buckets are disjoint and exhaustive by construction, not by
-- convention.
-- ===========================================================================
CREATE OR REPLACE VIEW ceo_ai.weighing_verification_status AS
SELECT
    vi.tenant_id                                             AS tenant_id,
    pk.name                                                  AS park_label,
    sh.name                                                  AS shed_label,
    COUNT(*)::bigint                                         AS total,
    COUNT(*) FILTER (WHERE vi.status = 'pending')::bigint    AS pending,
    COUNT(*) FILTER (WHERE vi.status = 'rejected')::bigint   AS rework,
    COUNT(*) FILTER (WHERE vi.status = 'approved')::bigint   AS verified,
    MIN(vi.captured_at) FILTER (WHERE vi.status = 'pending') AS oldest_pending_at
FROM verification_items vi
LEFT JOIN locations sh ON sh.location_id = vi.shed_id
LEFT JOIN locations pk ON pk.location_id = vi.park_id
WHERE vi.module = 'weighing'
GROUP BY vi.tenant_id, pk.name, sh.name;

GRANT SELECT ON ceo_ai.weighing_verification_status TO mesha_ceo_readonly;

-- +goose Down
DROP VIEW IF EXISTS ceo_ai.weighing_verification_status;
DROP VIEW IF EXISTS ceo_ai.weighing_capture_activity;
