-- +goose Up
-- +goose NO TRANSACTION
-- Operator "Awaiting RFID" list index.
--
-- The counts app read GET /app/counts/goats/temporary-tagged lists goats that still carry an active
-- temporary tag so an operator can promote each to a permanent RFID. The query INNER JOINs goats to
-- their active temporary_tag identifier and keyset-scans per tenant ordered by display_id.
--
-- Without this partial index that JOIN can only use goat_identifiers_lookup_idx up to
-- (tenant_id, identifier_type) and then filters status IN heap, degrading to a scan of every
-- temporary_tag row (active or retired) per tenant. This partial index makes the active-temp-tag
-- driving set index-selective: it holds ONLY the small, live "awaiting RFID" set, so the planner
-- probes it directly and joins to goats by primary key.
--
-- Lock-safe: CREATE INDEX CONCURRENTLY runs outside a transaction and takes only SHARE UPDATE
-- EXCLUSIVE, so writes to goat_identifiers are not blocked while it builds. Additive only.
CREATE INDEX CONCURRENTLY IF NOT EXISTS goat_identifiers_active_temporary_tag_idx
ON public.goat_identifiers (tenant_id, goat_id)
WHERE identifier_type = 'temporary_tag' AND status = 'active';

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS public.goat_identifiers_active_temporary_tag_idx;
