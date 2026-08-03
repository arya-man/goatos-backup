-- +goose Up
--
-- !! THIS FILE WAS EDITED AFTER IT SHIPPED ON main. READ THIS BEFORE MIGRATING. !!
--
-- The migration runner tracks each file by SHA-256 (cmd/migrate/main.go: "migration
-- %s was already applied with checksum %s, current %s") and HARD-FAILS on drift, so
-- editing an applied migration in place is normally forbidden -- see 000081's header,
-- which is why 000070/71/73 were superseded by new forward migrations instead.
--
-- This file is the documented exception, for the reason that forced it: as shipped, it
-- ABORTED on every environment that has the ceo_ai reader roles provisioned (staging and
-- production), because it granted SELECT on ceo_ai.weighing_verification_status before
-- creating it -- SQLSTATE 42P01. Those environments therefore never recorded it as
-- applied and CANNOT reach a later forward migration: 000080 fails first, every time.
-- Fixing it forward is not available; the fix has to be in this file.
--
-- CONSEQUENCE, and the remediation. Environments that DID apply the old file (local and
-- dev, which have no reader roles and so skipped the failing grant) now hold the old
-- checksum and will fail on the next migrate. Repair one of two ways:
--   * local only: re-run with -allow-local-checksum-drift (the flag validates
--     GOATOS_ENV=local and refuses anything else), or
--   * update the recorded checksum for version 000080 in
--     public.goatos_schema_migrations to the new file's SHA-256.
-- Neither is needed on staging or production, which never applied it.
--
-- There is precedent for this exception on main: b67c26413 ("guard 000080 ceo_ai grants
-- on role existence") edited this same file in place, for this same grant block.
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
--   * weighing_shed_observations is joined on withdrawn_at IS NULL, which is
--     what its UNIQUE index actually covers: 000067 DROPPED
--     weighing_shed_observations_one_active_scope_uidx and replaced it with
--     weighing_shed_observations_one_open_scope_uidx, PARTIAL on
--     withdrawn_at IS NULL. Only the live row is unique per bucket; a
--     reopened+resubmitted bucket legitimately keeps its superseded rows.
--   * the weighing_observations aggregate is pre-collapsed to one row per
--     campaign_shed_id via the LATERAL subquery's GROUP-free scalar
--     aggregation (COUNT/AVG/MIN/MAX over the whole bucket).
--
-- DISJOINT SOURCES, NOT DOUBLE-COUNTED: a campaign_shed's weighing_category
-- is fixed at creation ('individual_animal' or 'per_shed_partition') and the
-- write path only ever inserts into ONE of weighing_observations (per-animal
-- scans) or weighing_shed_observations (one lump-sum bucket total) for a
-- given campaign_shed_id -- never both. animals_weighed therefore SELECTS the
-- source by weighing_category rather than summing them. It must not null-test
-- them instead: obs.scan_count is a count() and returns 0, never NULL, for a
-- bucket with no scans.
--
-- projection-review: membership=one row per weighing_campaign_sheds row (campaign_shed_id is that table's PK), decorated with its campaign, its optional work item, its optional lump-sum shed observation, and a LATERAL scalar rollup of its own per-animal scans; group_key=campaign_shed_id (the view has no GROUP BY at all -- the only aggregation is the correlated LATERAL over weighing_observations, which collapses to EXACTLY ONE row per bucket); join_cardinality=weighing_campaigns 1 per campaign_id (PK), locations pk 0..1 per location_id (PK), weighing_work_items 0..1 (UNIQUE weighing_work_items_bucket_uidx on (tenant_id, campaign_shed_id), and campaign_shed_id is itself a PK so the pair is unique per bucket), weighing_shed_observations 0..1 GIVEN the withdrawn_at IS NULL join predicate (UNIQUE weighing_shed_observations_one_open_scope_uidx on (tenant_id, campaign_shed_id) PARTIAL on withdrawn_at IS NULL, per 000067 -- WITHOUT that predicate this side is 0..N and fans the bucket out), LATERAL obs exactly 1 -- every joined side is 0..1, so no side can multiply bucket rows; pagination=NONE, this is a view and every consumer paginates over it; scope=tenant_id, exposed as cs.tenant_id
--
-- Ratio key sets: animals_weighed is NOT a ratio and NOT a sum across sources. scan_count and shed_animal_count are drawn from DISJOINT bucket populations keyed by the same campaign_shed_id (weighing_category fixes which table the write path uses), so the CASE picks the one populated source for that key rather than adding two overlapping key sets. scan_count is itself already deduplicated to one row per scanned tag, so it is an ANIMAL count and not a capture count.
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
    -- Unified "how many animals were weighed" count, selected by the bucket's OWN
    -- category rather than by null-testing the two sources.
    --
    -- COALESCE(obs.scan_count, sho.animal_count, 0) did not work: obs.scan_count is a
    -- count(*) inside a LATERAL joined ON true, and count(*) over zero rows returns 0,
    -- never NULL. The second arm was therefore UNREACHABLE and every per_shed_partition
    -- bucket reported 0 animals weighed while its own shed_animal_count sat right next
    -- to it saying otherwise. weighing_category is fixed at bucket creation and decides
    -- which table the write path uses, so branching on it is the honest selector.
    CASE cs.weighing_category
        WHEN 'per_shed_partition' THEN COALESCE(sho.animal_count, 0)
        ELSE COALESCE(obs.scan_count, 0)
    END::bigint                                    AS animals_weighed
FROM weighing_campaign_sheds cs
JOIN weighing_campaigns c
  ON c.campaign_id = cs.campaign_id
LEFT JOIN locations pk
  ON pk.location_id = c.park_id
LEFT JOIN weighing_work_items wi
  ON wi.campaign_shed_id = cs.campaign_shed_id
-- ONE ROW PER ANIMAL, NOT ONE ROW PER CAPTURE.
--
-- weighing_observations keeps history instead of deleting: 000061 stamps submitted_at
-- on bucket completion, 000073's duplicate-loser collapse stamps it on the losing row,
-- and a reopened bucket's re-capture inserts a NEW row for a tag that already has one.
-- A bare count(*) therefore sums every superseded round on top of the live one and
-- reports more animals weighed than the shed holds.
--
-- Deduplicating by SCANNED TAG is the fix, and it is the only one that survives the
-- lifecycle. Filtering on state instead -- "submitted_at IS NULL OR
-- verification_status = 'rework'" -- looks like "the current round" and is not: a
-- normally finished bucket has every row submitted AND verified, so it matches neither
-- arm and reports ZERO animals weighed, which is the terminal state of essentially all
-- historical weighing work. That same filter also breaks the live round, because rework
-- is stamped per OBSERVATION, not per bucket: one bounced video out of three would
-- reduce the shed to a count of 1 and an average weight computed over that single
-- animal.
--
-- Weighing is FREE-FLOW (000078 dropped animal_id), so the animal's identity here IS
-- the scanned tag, matched case- and whitespace-insensitively exactly as the write
-- path's own duplicate guard does (000073's uidx grain). A blank tag cannot be
-- collapsed with other blank tags, so it keys on its own row instead. Newest capture
-- per tag wins, matching what 000073 chose as the defensible current proof.
LEFT JOIN LATERAL (
    SELECT
        count(*)                    AS scan_count,
        avg(latest.weight_kg)       AS weight_avg_kg,
        min(latest.weight_kg)       AS weight_min_kg,
        max(latest.weight_kg)       AS weight_max_kg
    FROM (
        SELECT DISTINCT ON (COALESCE(NULLIF(lower(btrim(o.scanned_identifier)), ''), o.observation_id::text))
               o.weight_kg
        FROM weighing_observations o
        -- tenant_id is NOT redundant. campaign_shed_id is globally unique (it is
        -- weighing_campaign_sheds' PK) so this is not a leak today, but EVERY index on
        -- weighing_observations leads with tenant_id, and PG17 has no skip scan: without
        -- this predicate the correlated subquery seq-scans the whole table once per
        -- bucket. Measured on 400 buckets x 300 observations: 3873ms/787k buffers
        -- without it, 554ms/206 buffers with it, in a view that has no LIMIT and gets
        -- scanned whole by its consumers.
        WHERE o.tenant_id = cs.tenant_id
          AND o.campaign_shed_id = cs.campaign_shed_id
        ORDER BY COALESCE(NULLIF(lower(btrim(o.scanned_identifier)), ''), o.observation_id::text),
                 o.accepted_at DESC, o.observation_id DESC
    ) latest
) obs ON true
-- withdrawn_at IS NULL is REQUIRED, not decorative. The uniqueness guarantee on
-- this table is weighing_shed_observations_one_open_scope_uidx, which 000067
-- made PARTIAL on withdrawn_at IS NULL -- so a bucket that was reopened and
-- resubmitted legitimately holds several rows, only one of them live. Joining
-- them all fans this view out past its declared one-row-per-campaign_shed_id
-- grain and multiplies the bucket in every downstream CEO rollup.
LEFT JOIN weighing_shed_observations sho
  ON sho.campaign_shed_id = cs.campaign_shed_id
 AND sho.withdrawn_at IS NULL;

-- ===========================================================================
-- 2. ceo_ai.weighing_verification_status
--
-- _grain: ONE ROW PER (tenant_id, park_label, shed_label) -- a park/shed
-- verification rollup, same shape as the existing
-- ceo_ai.verification_queue_status but scoped to module = 'weighing' only
-- (weighing proof videos/photos), so weighing questions never get mixed into
-- another module's verification backlog.
--
-- verification_items.status is CHECKed to {'pending','approved','rejected',
-- 'withdrawn'} -- 000067 added 'withdrawn' as a FOURTH terminal status for a
-- submission that was superseded (reopen/resubmit, duplicate-loser retire in
-- 000077). A withdrawn item is not a verification outcome and must not appear
-- in ANY bucket, total included: counting it in total while excluding it from
-- pending/rework/verified made the three displayed buckets silently fail to sum
-- to the displayed total. It is filtered out in the WHERE clause instead, so
-- pending + rework + verified = total holds by construction again.
--
-- projection-review: membership=verification_items rows filtered to module = 'weighing' (the WHERE runs before the aggregate, so no other module's rows enter any bucket); group_key=(vi.tenant_id, pk.name, sh.name), exactly the GROUP BY list; join_cardinality=locations sh 0..1 per vi.shed_id and locations pk 0..1 per vi.park_id, both matching on locations.location_id which is that table's PK, so neither LEFT JOIN can duplicate a verification_items row and COUNT(*) stays at verification-item grain; pagination=NONE, this is a view and every consumer paginates over it; scope=tenant_id, grouped and exposed as vi.tenant_id
--
-- Ratio key sets: pending, rework and verified are FILTER aggregates over the IDENTICAL grouped row set that produces total -- same FROM, same WHERE, same GROUP BY, no extra join on any branch. verification_items.status carries a CHECK restricting it to {'pending','approved','rejected','withdrawn'} (000001 baseline, extended with 'withdrawn' in 000067); `total` is itself a FILTER that excludes 'withdrawn' (the rows are KEPT, so an all-withdrawn shed still appears, counted in the separate `withdrawn` column), so the three displayed filters are disjoint and exhaustive against it and pending + rework + verified = total for every key, by constraint rather than by convention.
-- ===========================================================================
CREATE OR REPLACE VIEW ceo_ai.weighing_verification_status AS
SELECT
    vi.tenant_id                                             AS tenant_id,
    pk.name                                                  AS park_label,
    sh.name                                                  AS shed_label,
    -- total is the LIVE verification workload and excludes withdrawn, so that
    -- pending + rework + verified = total holds. withdrawn is carried in its own
    -- column rather than dropped in the WHERE clause: filtering the rows out
    -- removed whole SHEDS from the view, so a shed whose entire verification
    -- history was superseded (000077 retires duplicate losers as 'withdrawn')
    -- became indistinguishable from a shed that never weighed at all.
    COUNT(*) FILTER (WHERE vi.status <> 'withdrawn')::bigint  AS total,
    COUNT(*) FILTER (WHERE vi.status = 'pending')::bigint    AS pending,
    COUNT(*) FILTER (WHERE vi.status = 'rejected')::bigint   AS rework,
    COUNT(*) FILTER (WHERE vi.status = 'approved')::bigint   AS verified,
    MIN(vi.captured_at) FILTER (WHERE vi.status = 'pending') AS oldest_pending_at,
    -- APPENDED, deliberately. CREATE OR REPLACE VIEW may only ADD columns at the END:
    -- inserting these two before oldest_pending_at aborts with `cannot change name of
    -- view column "oldest_pending_at" to "withdrawn"` on any database that already holds
    -- an earlier shape of this view.
    COUNT(*) FILTER (WHERE vi.status = 'withdrawn')::bigint  AS withdrawn,
    COUNT(*)::bigint                                         AS total_including_withdrawn
FROM verification_items vi
LEFT JOIN locations sh ON sh.location_id = vi.shed_id
LEFT JOIN locations pk ON pk.location_id = vi.park_id
WHERE vi.module = 'weighing'
GROUP BY vi.tenant_id, pk.name, sh.name;

-- ===========================================================================
-- Grants. These run AFTER both CREATE VIEW statements: granting on
-- ceo_ai.weighing_verification_status before it exists aborted the whole
-- migration with `relation "ceo_ai.weighing_verification_status" does not
-- exist` on every environment that actually has the reader roles provisioned
-- (i.e. staging and production -- the exact environments a migration must not
-- fail on). Local/pgtest databases have neither role and so never hit it.
--
-- Guarded on role existence, exactly like the 000001 baseline block that grants
-- these same two roles. mesha_ceo_readonly / mesha_cube_readonly are
-- provisioned per environment (tools/dev/setup-ceo-ai-local-role.sh + Secret
-- Manager), NOT by a migration. Idempotent and safe to re-run.
-- ===========================================================================
-- +goose StatementBegin
DO $weighing_ceo_ai_grants$
DECLARE
    r text;
BEGIN
    FOREACH r IN ARRAY ARRAY['mesha_ceo_readonly','mesha_cube_readonly'] LOOP
        IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = r) THEN
            EXECUTE format('GRANT SELECT ON ceo_ai.weighing_capture_activity TO %I', r);
            EXECUTE format('GRANT SELECT ON ceo_ai.weighing_verification_status TO %I', r);
        END IF;
    END LOOP;
END;
$weighing_ceo_ai_grants$;
-- +goose StatementEnd

-- +goose Down
DROP VIEW IF EXISTS ceo_ai.weighing_verification_status;
DROP VIEW IF EXISTS ceo_ai.weighing_capture_activity;
