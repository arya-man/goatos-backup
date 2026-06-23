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
