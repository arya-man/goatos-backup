-- name: ListVaccinationCompletionsByGoat :many
-- Goat Passport history. Uses vaccination_completions_goat_history_idx (tenant_id, goat_id, administered_at DESC).
SELECT completion_id::text AS completion_id, obligation_id::text AS obligation_id,
       COALESCE(batch_id::text, '')::text AS batch_id, administered_at, status,
       COALESCE(doses, 0)::int AS doses, COALESCE(route_site, '')::text AS route_site,
       adverse_reaction, withdrawal_until_date
FROM vaccination_completions
WHERE tenant_id = @tenant_id AND goat_id = @goat_id
ORDER BY administered_at DESC
LIMIT @row_limit;

-- name: GetLastAcceptedCompletionForGoat :one
-- Next-due / SM-7 basis: most recent accepted administration for a goat.
SELECT completion_id::text AS completion_id, obligation_id::text AS obligation_id, administered_at
FROM vaccination_completions
WHERE tenant_id = @tenant_id AND goat_id = @goat_id AND status = 'accepted'
ORDER BY administered_at DESC
LIMIT 1;

-- name: CountEligibleGoats :one
-- Live impact: alive goats matching a rule's eligibility dims within an optional park scope.
-- Optional text dims use the ('' OR col = @x) idiom; park scope via a nullable narg uuid.
SELECT count(*)::bigint AS total
FROM goats
WHERE tenant_id = @tenant_id
  AND lifecycle_status = 'alive'
  AND (@stage::text = '' OR management_stage = @stage::text)
  AND (@sex::text = '' OR sex = @sex::text)
  AND (@breed::text = '' OR breed = @breed::text)
  AND (@health::text = '' OR COALESCE(health_status, '') = @health::text)
  AND (sqlc.narg('park_id')::uuid IS NULL OR park_id = sqlc.narg('park_id')::uuid);

-- name: CountCatchupGoats :one
-- Eligible goats that already have an accepted completion (next-due from last accepted, not DOB).
SELECT count(DISTINCT g.goat_id)::bigint AS total
FROM goats g
JOIN vaccination_completions vc
  ON vc.tenant_id = g.tenant_id AND vc.goat_id = g.goat_id AND vc.status = 'accepted'
WHERE g.tenant_id = @tenant_id
  AND g.lifecycle_status = 'alive'
  AND (@stage::text = '' OR g.management_stage = @stage::text)
  AND (@sex::text = '' OR g.sex = @sex::text)
  AND (@breed::text = '' OR g.breed = @breed::text)
  AND (@health::text = '' OR COALESCE(g.health_status, '') = @health::text)
  AND (sqlc.narg('park_id')::uuid IS NULL OR g.park_id = sqlc.narg('park_id')::uuid);

-- name: CountEligibleShedScopes :one
-- Estimated drive batches = distinct sheds holding eligible goats (one shed drive per shed).
SELECT count(DISTINCT shed_id)::bigint AS total
FROM goats
WHERE tenant_id = @tenant_id
  AND lifecycle_status = 'alive'
  AND shed_id IS NOT NULL
  AND (@stage::text = '' OR management_stage = @stage::text)
  AND (@sex::text = '' OR sex = @sex::text)
  AND (@breed::text = '' OR breed = @breed::text)
  AND (@health::text = '' OR COALESCE(health_status, '') = @health::text)
  AND (sqlc.narg('park_id')::uuid IS NULL OR park_id = sqlc.narg('park_id')::uuid);

-- name: ListEligibleGoatsForGeneration :many
-- Chunked (keyset) listing of the in-care cohort for SM-1 generation. Cursor by goat_id over the
-- (tenant_id, goat_id) unique index. Includes defer-state goats (icu/quarantine/sick) so the
-- handler can emit a visible deferred obligation rather than silently skipping them.
SELECT goat_id::text AS goat_id, dob, entry_date, lifecycle_status,
       COALESCE(shed_id::text, '')::text AS shed_id,
       COALESCE(park_id::text, '')::text AS park_id
FROM goats
WHERE tenant_id = @tenant_id
  AND lifecycle_status IN ('alive', 'sick', 'under_treatment', 'quarantine', 'icu')
  AND (@stage::text = '' OR management_stage = @stage::text)
  AND (@sex::text = '' OR sex = @sex::text)
  AND (@breed::text = '' OR breed = @breed::text)
  AND (sqlc.narg('park_id')::uuid IS NULL OR park_id = sqlc.narg('park_id')::uuid)
  AND goat_id > @after_goat_id::uuid
ORDER BY goat_id
LIMIT @row_limit;

-- name: GetGoatForGeneration :one
-- Single-goat generation fields (incl sex/breed/stage for Go-side eligibility match on goat.created).
SELECT goat_id::text AS goat_id, dob, entry_date, lifecycle_status,
       COALESCE(shed_id::text, '')::text AS shed_id,
       COALESCE(park_id::text, '')::text AS park_id,
       COALESCE(sex, '')::text AS sex,
       COALESCE(breed, '')::text AS breed,
       COALESCE(management_stage, '')::text AS management_stage
FROM goats
WHERE tenant_id = @tenant_id AND goat_id = @goat_id::uuid;

-- name: SumAvailableStockForItem :one
-- Available (unreserved) doses for the vaccine item + earliest expiry, within an optional location.
SELECT COALESCE(SUM(quantity_in_stock - quantity_reserved), 0)::numeric AS available,
       MIN(expiry_date)::date AS earliest_expiry
FROM inventory_stock
WHERE tenant_id = @tenant_id
  AND item_id = @item_id
  AND quantity_in_stock > quantity_reserved
  AND (sqlc.narg('location_id')::uuid IS NULL OR location_id = sqlc.narg('location_id')::uuid);
