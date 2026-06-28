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

-- name: ListRecordedCompletionsByTask :many
-- SOP verify fan-out: the still-recorded vaccination completions captured under a SOP task's
-- submissions, so a task-level verify/rework can be applied per completion. Drives from the task's
-- submissions (sop_submissions_task_history_idx) -> items (sop_submission_items_submission_idx) ->
-- completions (vaccination_completions_submission_item_idx).
SELECT c.completion_id::text AS completion_id
FROM vaccination_completions c
JOIN sop_submission_items i ON i.tenant_id = c.tenant_id AND i.item_id = c.sop_submission_item_id
JOIN sop_submissions s ON s.tenant_id = i.tenant_id AND s.submission_id = i.submission_id
WHERE c.tenant_id = @tenant_id AND s.task_id = @task_id AND c.status = 'recorded'
  AND c.sop_submission_item_id IS NOT NULL
ORDER BY c.completion_id;

-- name: ListRecordedCompletions :many
-- Verification queue: completions awaiting review (status='recorded'), earliest administered first.
-- Uses vaccination_completions_review_idx (tenant_id, administered_at) WHERE status='recorded'. The
-- optional park_id scope filters by the completed goat's park so the top-bar park scope reaches the queue.
SELECT vc.completion_id::text AS completion_id, vc.obligation_id::text AS obligation_id,
       vc.goat_id::text AS goat_id, COALESCE(vc.batch_id::text, '')::text AS batch_id,
       vc.administered_at, COALESCE(vc.doses, 0)::int AS doses, COALESCE(vc.route_site, '')::text AS route_site
FROM vaccination_completions vc
LEFT JOIN goats g ON g.tenant_id = vc.tenant_id AND g.goat_id = vc.goat_id
WHERE vc.tenant_id = @tenant_id AND vc.status = 'recorded'
  AND (sqlc.narg('park_id')::uuid IS NULL OR g.park_id = sqlc.narg('park_id')::uuid)
ORDER BY vc.administered_at ASC, vc.completion_id ASC
LIMIT @row_limit;

-- name: GetLastAcceptedCompletionForGoat :one
-- Next-due / SM-7 basis: most recent accepted administration for a goat.
SELECT completion_id::text AS completion_id, obligation_id::text AS obligation_id, administered_at
FROM vaccination_completions
WHERE tenant_id = @tenant_id AND goat_id = @goat_id AND status = 'accepted'
ORDER BY administered_at DESC
LIMIT 1;

-- name: GetRecordedVaccinationCompletion :one
-- Verification preflight: read the stock/obligation context while the completion is still pending
-- review, so stock consumption can happen before the row flips to accepted.
SELECT obligation_id::text AS obligation_id,
       goat_id::text AS goat_id,
       COALESCE(batch_id::text, '')::text AS batch_id,
       COALESCE(vaccine_inventory_lot_id::text, '')::text AS vaccine_inventory_lot_id,
       COALESCE(doses, 0)::int AS doses,
       administered_at
FROM vaccination_completions
WHERE tenant_id = @tenant_id
  AND completion_id = @completion_id
  AND status = 'recorded';

-- name: GetAcceptableVaccinationCompletion :one
-- Recovery/resume read for SM-5: a retry after the completion row was already accepted must still
-- be able to finish idempotent side effects such as obligation completion, stock consumption, and
-- booster scheduling.
SELECT completion_id::text AS completion_id,
       status,
       obligation_id::text AS obligation_id,
       goat_id::text AS goat_id,
       COALESCE(batch_id::text, '')::text AS batch_id,
       COALESCE(vaccine_inventory_lot_id::text, '')::text AS vaccine_inventory_lot_id,
       COALESCE(doses, 0)::int AS doses,
       administered_at
FROM vaccination_completions
WHERE tenant_id = @tenant_id
  AND completion_id = @completion_id
  AND status IN ('recorded', 'accepted');

-- name: GetAcceptableVaccinationCompletionByIdempotency :one
-- Direct Accept recovery: if the record step succeeded but a later side effect failed, the same
-- idempotency key must resume the existing completion instead of no-oping.
SELECT completion_id::text AS completion_id,
       status,
       obligation_id::text AS obligation_id,
       goat_id::text AS goat_id,
       COALESCE(batch_id::text, '')::text AS batch_id,
       COALESCE(vaccine_inventory_lot_id::text, '')::text AS vaccine_inventory_lot_id,
       COALESCE(doses, 0)::int AS doses,
       administered_at
FROM vaccination_completions
WHERE tenant_id = @tenant_id
  AND idempotency_key = @idempotency_key
  AND status IN ('recorded', 'accepted');

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
-- handler can write a canonical deferred obligation rather than silently skipping them.
SELECT g.goat_id::text AS goat_id, g.dob, g.entry_date, g.lifecycle_status,
       COALESCE(g.health_status, '')::text AS health_status,
       COALESCE(g.reproductive_status, '')::text AS reproductive_status,
       COALESCE(shed.location_id::text, '')::text AS shed_id,
       COALESCE(park.location_id::text, '')::text AS park_id,
       COALESCE(g.sex, '')::text AS sex,
       COALESCE(g.breed, '')::text AS breed,
       COALESCE(g.management_stage, '')::text AS management_stage,
       COALESCE(loa.is_quarantine, false)::boolean AS location_is_quarantine,
       COALESCE(loa.is_icu, false)::boolean AS location_is_icu
FROM goats g
LEFT JOIN location_operational_attributes loa
  ON loa.tenant_id = g.tenant_id
 AND loa.location_id = COALESCE(g.current_location_id, g.shed_id)
LEFT JOIN locations shed
  ON shed.tenant_id = g.tenant_id
 AND shed.location_id = g.shed_id
 AND shed.location_type = 'shed'
LEFT JOIN locations park
  ON park.tenant_id = g.tenant_id
 AND park.location_id = g.park_id
 AND park.location_type = 'park'
WHERE g.tenant_id = @tenant_id
  AND (
    g.lifecycle_status IN ('alive', 'sick', 'under_treatment', 'quarantine', 'icu')
    OR COALESCE(g.health_status, '') IN ('sick', 'under_treatment', 'quarantine', 'icu')
    OR COALESCE(loa.is_quarantine, false)
    OR COALESCE(loa.is_icu, false)
  )
  AND (@stage::text = '' OR g.management_stage = @stage::text)
  AND (@sex::text = '' OR g.sex = @sex::text)
  AND (@breed::text = '' OR g.breed = @breed::text)
  AND (@health::text = '' OR COALESCE(g.health_status, '') = @health::text)
  AND (sqlc.narg('park_id')::uuid IS NULL OR g.park_id = sqlc.narg('park_id')::uuid)
  AND g.goat_id > @after_goat_id::uuid
ORDER BY g.goat_id
LIMIT @row_limit;

-- name: GetGoatForGeneration :one
-- Single-goat generation fields (incl sex/breed/stage for Go-side eligibility match on goat.created).
SELECT g.goat_id::text AS goat_id, g.dob, g.entry_date, g.lifecycle_status,
       COALESCE(g.health_status, '')::text AS health_status,
       COALESCE(g.reproductive_status, '')::text AS reproductive_status,
       COALESCE(shed.location_id::text, '')::text AS shed_id,
       COALESCE(park.location_id::text, '')::text AS park_id,
       COALESCE(g.sex, '')::text AS sex,
       COALESCE(g.breed, '')::text AS breed,
       COALESCE(g.management_stage, '')::text AS management_stage,
       COALESCE(loa.is_quarantine, false)::boolean AS location_is_quarantine,
       COALESCE(loa.is_icu, false)::boolean AS location_is_icu
FROM goats g
LEFT JOIN location_operational_attributes loa
  ON loa.tenant_id = g.tenant_id
 AND loa.location_id = COALESCE(g.current_location_id, g.shed_id)
LEFT JOIN locations shed
  ON shed.tenant_id = g.tenant_id
 AND shed.location_id = g.shed_id
 AND shed.location_type = 'shed'
LEFT JOIN locations park
  ON park.tenant_id = g.tenant_id
 AND park.location_id = g.park_id
 AND park.location_type = 'park'
WHERE g.tenant_id = @tenant_id AND g.goat_id = @goat_id::uuid;

-- name: SumAvailableStockForItem :one
-- Available (unreserved) doses for the vaccine item + earliest expiry, within an optional location.
SELECT COALESCE(SUM(quantity_in_stock - quantity_reserved), 0)::numeric AS available,
       MIN(expiry_date)::date AS earliest_expiry
FROM inventory_stock
WHERE tenant_id = @tenant_id
  AND item_id = @item_id
  AND quantity_in_stock > quantity_reserved
  AND status = 'active'
  AND (expiry_date IS NULL OR expiry_date >= CURRENT_DATE)
  AND (sqlc.narg('location_id')::uuid IS NULL OR location_id = sqlc.narg('location_id')::uuid);
