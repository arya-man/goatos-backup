-- name: RecordVaccinationCompletion :one
-- Idempotent on (tenant_id, idempotency_key); returns no row on replay.
INSERT INTO vaccination_completions (
  tenant_id, obligation_id, batch_id, goat_id, sop_submission_item_id, vaccine_inventory_lot_id,
  doses, dose_ml_given, route_site, adverse_reaction, adverse_reaction_problem_id, cold_chain_verified,
  administered_at, status, withdrawal_until_date, recorded_by, idempotency_key
) VALUES (
  @tenant_id, @obligation_id, @batch_id, @goat_id, @sop_submission_item_id, @vaccine_inventory_lot_id,
  @doses, @dose_ml_given, @route_site, @adverse_reaction, @adverse_reaction_problem_id, @cold_chain_verified,
  @administered_at, @status, @withdrawal_until_date, @recorded_by, @idempotency_key
)
ON CONFLICT (tenant_id, idempotency_key) DO NOTHING
RETURNING completion_id::text AS completion_id;

-- name: AcceptVaccinationCompletion :many
-- SM-5 verify (accept). Acts only on a still-recorded row, returning its verification context so the
-- caller can complete the obligation + consume stock. Idempotent: an already-accepted/rejected row
-- matches nothing → no rows → caller no-ops.
UPDATE vaccination_completions
SET status = 'accepted', verified_by = @verified_by, verified_at = now(),
    withdrawal_until_date = @withdrawal_until_date, row_version = row_version + 1, updated_at = now()
WHERE tenant_id = @tenant_id AND completion_id = @completion_id AND status = 'recorded'
RETURNING obligation_id::text AS obligation_id,
          goat_id::text AS goat_id,
          COALESCE(batch_id::text, '')::text AS batch_id,
          COALESCE(vaccine_inventory_lot_id::text, '')::text AS vaccine_inventory_lot_id,
          COALESCE(doses, 0)::int AS doses,
          administered_at;

-- name: RejectVaccinationCompletion :execrows
-- SM-5 verify (rework). Acts only on a still-recorded row. Idempotent: returns 0 rows on replay.
UPDATE vaccination_completions
SET status = 'rejected', verified_by = @verified_by, verified_at = now(),
    rejection_reason = @rejection_reason, row_version = row_version + 1, updated_at = now()
WHERE tenant_id = @tenant_id AND completion_id = @completion_id AND status = 'recorded';
