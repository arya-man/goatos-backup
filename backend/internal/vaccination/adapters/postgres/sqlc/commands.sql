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

-- name: AcceptVaccinationCompletion :exec
UPDATE vaccination_completions
SET status = 'accepted', verified_by = @verified_by, verified_at = now(),
    withdrawal_until_date = @withdrawal_until_date, row_version = row_version + 1, updated_at = now()
WHERE tenant_id = @tenant_id AND completion_id = @completion_id AND status = 'recorded';

-- name: RejectVaccinationCompletion :exec
UPDATE vaccination_completions
SET status = 'rejected', verified_by = @verified_by, verified_at = now(),
    rejection_reason = @rejection_reason, row_version = row_version + 1, updated_at = now()
WHERE tenant_id = @tenant_id AND completion_id = @completion_id AND status = 'recorded';
