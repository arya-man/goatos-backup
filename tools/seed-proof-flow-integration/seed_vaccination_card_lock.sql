-- projection-review: membership=local-dev seed rows only (fixed test UUIDs), never a serving read; group_key=per-goat obligation grain identical to the production tables it seeds; join_cardinality=inserts are keyed by deterministic ids so re-running upserts rather than multiplying rows; pagination=not a read path — no pages served from this script; scope=throwaway local tenant seed, tenant-scoped ids throughout.
-- Seed script for the vaccination card-lock invariant fix (proof-flow-integration).
--
-- Extends the existing seeded batch e2000000-0000-4000-8000-000000000102 (protocol
-- fmd_kid_12w, scope=Castro shed inside Channapatna park, conducted_by=Amit Kumar
-- 993c2084-4918-5ebd-a7b2-d94e70fe9894) to the exact field-bug regression shape at the
-- backend's own ANIMAL grain (executionDisplayCounts / animal_rollup in
-- backend/internal/vaccinationexecution/adapters/postgres/repository.go count DISTINCT
-- animals, not raw obligation_instances rows -- a goat can carry more than one obligation
-- row in the same batch, e.g. two doses of the same drive):
--
--   target=17 distinct goats, done=11 (completion evidence recorded, none accepted yet =
--   needs_review), open=6 (no completion evidence at all), NO final submission.
--
-- Steps:
--   1. Add a 'recorded' vaccination_completions row to the batch's pre-existing in_progress
--      obligations that had none (these goats already carry a DONE 'completed' obligation
--      from the original seed, so this is evidence-completeness only, not a count change).
--   2. Add 5 brand-new obligation_instances on 5 more Castro-shed goats not yet in this
--      batch, each with a 'recorded' completion -- these become 5 of the 11 done goats.
--   3. Add 6 more brand-new obligation_instances on 6 more Castro-shed goats, left bare
--      (no completion) -- these are the 6 open goats.
--   4. Assign a TEMP-CL-### temporary_tag identifier to every goat in the batch that does
--      not already have an active one, numbered by first-obligation order.
--
-- Idempotent: safe to re-run. Uses ON CONFLICT DO NOTHING on all inserts and only fills
-- gaps (goats without an active temporary_tag, obligations that don't already exist for a
-- given idempotency_key).
--
-- RUN WITH:
--   docker exec -i goatos-local-current psql -U postgres -d goatos \
--     -f tools/seed-proof-flow-integration/seed_vaccination_card_lock.sql

\set ON_ERROR_STOP on
\set batch_id 'e2000000-0000-4000-8000-000000000102'
\set shed_id '62241795-628e-58ef-9591-aa384fb0f0f7'
\set operator_id '993c2084-4918-5ebd-a7b2-d94e70fe9894'
\set tenant_id '00000000-0000-4000-8000-000000000001'

BEGIN;

\echo 'Step 0: preconditions'
SELECT batch_id, status, scope_id AS shed_id, conducted_by AS operator_id
FROM obligation_batches
WHERE batch_id = :'batch_id';

-- 1) Backfill completion evidence ONLY on the batch's original pre-existing in_progress
-- obligations (idempotency_key prefix 'local-stg-e2e-') that have none yet. Scoped by key
-- prefix so re-runs never touch the OPEN goats this script itself seeds in step 3 below.
INSERT INTO vaccination_completions (
    tenant_id, obligation_id, batch_id, goat_id, doses, dose_ml_given, route_site,
    administered_at, status, recorded_by, idempotency_key
)
SELECT
    oi.tenant_id, oi.obligation_id, oi.batch_id, oi.target_id, 1, 1.0, 'subcutaneous',
    now(), 'recorded', :'operator_id',
    'seed-vacc-card-lock-existing-' || oi.obligation_id::text
FROM obligation_instances oi
WHERE oi.batch_id = :'batch_id'
  AND oi.status = 'in_progress'
  AND oi.idempotency_key LIKE 'local-stg-e2e-%'
  AND NOT EXISTS (SELECT 1 FROM vaccination_completions vc WHERE vc.obligation_id = oi.obligation_id)
ON CONFLICT (tenant_id, idempotency_key) DO NOTHING;

-- 2) + 3) Top the batch up to exactly 17 distinct goats: 5 new DONE goats (obligation +
-- 'recorded' completion) and 6 new OPEN goats (obligation only). Guarded by the current
-- distinct-goat count so a re-run against an already-seeded batch adds nothing further --
-- without this guard, "goats not yet in the batch" is never empty on a 64-goat shed and a
-- naive re-run would keep growing the batch every time it runs.
WITH src AS (
    SELECT tenant_id, protocol_version_id, rule_id, batch_id, scope_type, scope_id, sop_task_id, due_at
    FROM obligation_instances WHERE batch_id = :'batch_id' LIMIT 1
),
current_state AS (
    SELECT COUNT(DISTINCT target_id) AS distinct_goats FROM obligation_instances WHERE batch_id = :'batch_id'
),
candidate_goats AS (
    SELECT g.goat_id, row_number() OVER (ORDER BY g.goat_id) AS rn
    FROM goats g, src, current_state
    WHERE g.shed_id = :'shed_id'
      AND g.lifecycle_status = 'alive'
      AND current_state.distinct_goats < 17
      AND g.goat_id NOT IN (SELECT target_id FROM obligation_instances WHERE batch_id = src.batch_id)
    ORDER BY g.goat_id
    LIMIT 11
)
INSERT INTO obligation_instances (
    tenant_id, protocol_version_id, rule_id, batch_id, target_type, target_id,
    scope_type, scope_id, due_at, status, sop_task_id, idempotency_key, sequence
)
SELECT
    src.tenant_id, src.protocol_version_id, src.rule_id, src.batch_id, 'goat', cg.goat_id,
    src.scope_type, src.scope_id, src.due_at, 'in_progress', src.sop_task_id,
    CASE WHEN cg.rn <= 5
        THEN 'seed-vacc-card-lock-done-' || cg.goat_id::text
        ELSE 'seed-vacc-card-lock-open-' || cg.goat_id::text
    END,
    1
FROM candidate_goats cg, src
ON CONFLICT (tenant_id, idempotency_key) DO NOTHING;

-- The 5 goats added with idempotency_key prefix 'seed-vacc-card-lock-done-' get a 'recorded'
-- completion (done). The 6 added with prefix 'seed-vacc-card-lock-open-' are DELIBERATELY
-- left bare -- do not extend this INSERT's WHERE clause to also match 'seed-vacc-card-lock-
-- open-%' or it recreates the exact field bug this seed exists to regression-test.
INSERT INTO vaccination_completions (
    tenant_id, obligation_id, batch_id, goat_id, doses, dose_ml_given, route_site,
    administered_at, status, recorded_by, idempotency_key
)
SELECT
    oi.tenant_id, oi.obligation_id, oi.batch_id, oi.target_id, 1, 1.0, 'subcutaneous',
    now(), 'recorded', :'operator_id',
    'seed-vacc-card-lock-done-completion-' || oi.obligation_id::text
FROM obligation_instances oi
WHERE oi.batch_id = :'batch_id'
  AND oi.idempotency_key LIKE 'seed-vacc-card-lock-done-%'
  AND NOT EXISTS (SELECT 1 FROM vaccination_completions vc WHERE vc.obligation_id = oi.obligation_id)
ON CONFLICT (tenant_id, idempotency_key) DO NOTHING;

-- 4) TEMP-CL-### identifiers for every goat in this batch that doesn't already have an
-- active temporary_tag, numbered by each goat's first obligation's creation order among
-- goats still missing one -- gap-filling, so numbers may not be contiguous across re-runs,
-- but every goat in the batch ends up with exactly one active TEMP-CL-### tag.
WITH unidentified AS (
    SELECT DISTINCT oi.target_id AS goat_id, MIN(oi.created_at) AS first_created
    FROM obligation_instances oi
    WHERE oi.batch_id = :'batch_id'
      AND NOT EXISTS (
          SELECT 1 FROM goat_identifiers gi
          WHERE gi.goat_id = oi.target_id AND gi.identifier_type = 'temporary_tag' AND gi.status = 'active'
      )
    GROUP BY oi.target_id
),
numbered AS (
    SELECT goat_id, row_number() OVER (ORDER BY first_created, goat_id) AS rn
    FROM unidentified
),
taken AS (
    SELECT regexp_replace(identifier_value, '^TEMP-CL-', '')::int AS n
    FROM goat_identifiers gi
    JOIN obligation_instances oi ON oi.target_id = gi.goat_id
    WHERE oi.batch_id = :'batch_id'
      AND gi.identifier_type = 'temporary_tag' AND gi.status = 'active'
      AND gi.identifier_value ~ '^TEMP-CL-[0-9]+$'
),
free_numbers AS (
    SELECT n, row_number() OVER (ORDER BY n) AS rn
    FROM generate_series(1, 999) AS n
    WHERE n NOT IN (SELECT n FROM taken)
)
INSERT INTO goat_identifiers (
    tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key,
    is_primary_for_goat, status, valid_from, source_system, normalizer_version
)
SELECT
    :'tenant_id', numbered.goat_id, 'temporary_tag',
    'TEMP-CL-' || lpad(free_numbers.n::text, 3, '0'),
    upper('TEMP-CL-' || lpad(free_numbers.n::text, 3, '0')),
    'seed-vacc-card-lock', false, 'active', now(), 'seed-vacc-card-lock', 'identifier_normalizer_v1'
FROM numbered
JOIN free_numbers ON free_numbers.rn = numbered.rn
ON CONFLICT (tenant_id, normalized_value) DO NOTHING;

COMMIT;

\echo 'Step 5: verification -- expect target=17 done=11 open=6 at ANIMAL grain (distinct goats)'
WITH per_goat AS (
    SELECT oi.target_id,
        COALESCE(BOOL_OR(oi.status = 'completed' OR vc.status IN ('recorded', 'accepted')), false) AS has_done
    FROM obligation_instances oi
    LEFT JOIN vaccination_completions vc ON vc.obligation_id = oi.obligation_id
    WHERE oi.batch_id = :'batch_id'
    GROUP BY oi.target_id
)
SELECT
    COUNT(*) AS target_count,
    COUNT(*) FILTER (WHERE has_done) AS done_count,
    COUNT(*) FILTER (WHERE NOT has_done) AS open_count
FROM per_goat;

\echo 'Seeded goat identifiers for this batch (expect 17 rows, one TEMP-CL-### each):'
SELECT gi.identifier_value
FROM goat_identifiers gi
WHERE gi.goat_id IN (SELECT DISTINCT target_id FROM obligation_instances WHERE batch_id = :'batch_id')
  AND gi.identifier_type = 'temporary_tag' AND gi.status = 'active'
ORDER BY gi.identifier_value;
