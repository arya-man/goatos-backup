-- +goose Up
-- Shed-wise vaccination ownership: allow shed-scoped Position seats so a shed's operational Manager can
-- be a shed-scoped position holder (scope_type='shed', scope_id=<shed location id>). This keeps shed
-- manager/backup as WORKFORCE-owned roster data (no parallel vaccination-owned ownership table). Backup
-- still resolves through the existing center Backup Manager slot unless a shed-specific backup exists.
ALTER TABLE workforce_positions
  DROP CONSTRAINT IF EXISTS workforce_positions_scope_check;
ALTER TABLE workforce_positions
  ADD CONSTRAINT workforce_positions_scope_check CHECK (scope_type IN ('tenant', 'center', 'shed'));

-- +goose Down
ALTER TABLE workforce_positions
  DROP CONSTRAINT IF EXISTS workforce_positions_scope_check;
ALTER TABLE workforce_positions
  ADD CONSTRAINT workforce_positions_scope_check CHECK (scope_type IN ('tenant', 'center'));
