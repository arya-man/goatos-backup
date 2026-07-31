-- +goose Up
-- +goose NO TRANSACTION
-- Weighing roster observations keyset index.
--
-- ListScopeRoster returns two independently paginated collections: the expected
-- roster and the shed's observations. The observations read was previously
-- UNBOUNDED -- it returned every observation in the shed, which is the mobile
-- twin of a full-table read and breaks the ~20-rows-per-page rule. It is now a
-- keyset page:
--
--   WHERE  tenant_id = $1 AND campaign_id = $2 AND campaign_shed_id = $3
--     AND  (accepted_at, observation_id) > ($6, $7)
--   ORDER BY accepted_at, observation_id
--   LIMIT  $8
--
-- No committed index served that shape. The two pre-existing indexes
-- (weighing_observations_campaign_animal_idx and
-- weighing_observations_campaign_scanned_identifier_idx) are both keyed
-- (tenant_id, campaign_id, <identity>, accepted_at DESC): neither contains
-- campaign_shed_id, and both sort accepted_at DESC, so neither can serve an
-- ASC (accepted_at, observation_id) keyset scoped to one bucket. Without this
-- index the planner filters the whole campaign's observations per page and then
-- sorts -- so paginating the query without indexing it would have moved the cost
-- rather than removed it.
--
-- Column order mirrors the predicate exactly: the three equality columns first,
-- then the two ordering columns in ASC keyset order, so the index yields rows
-- already sorted and LIMIT stops early instead of materialising and sorting.
--
-- Lock-safe: CREATE INDEX CONCURRENTLY runs outside a transaction and takes only
-- SHARE UPDATE EXCLUSIVE, so operator observation writes are not blocked while it
-- builds. Additive only -- no column, constraint, or data change.
CREATE INDEX CONCURRENTLY IF NOT EXISTS weighing_observations_shed_keyset_idx
ON public.weighing_observations (tenant_id, campaign_id, campaign_shed_id, accepted_at, observation_id);

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS public.weighing_observations_shed_keyset_idx;
