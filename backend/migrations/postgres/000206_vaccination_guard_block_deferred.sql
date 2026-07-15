-- +goose Up
-- Close the deferred-obligation exit race.
--
-- block_active_vaccination_for_procurement_excluded_goat() was authored in 000083, BEFORE
-- 'deferred' became a first-class obligation_instances status (added in 000097). Its blocked-status
-- list therefore omitted 'deferred'. A stale generation job that read an animal as sick/quarantined
-- BEFORE the exit could then insert a first-time 'deferred' vaccination obligation AFTER goat.exited
-- cancellation had already run, for an animal whose lifecycle_status was already sold/culled/etc.
-- Calendar's deferred catch-up branch (canonical_read: status IN ('missed','in_progress','deferred'))
-- then re-surfaced the exited animal in schedules.
--
-- Forward-only fix: CREATE OR REPLACE the guard function to also block 'deferred' inserts for
-- procurement/lifecycle-excluded animals. The existing
-- obligation_instances_procurement_vaccination_guard_trg trigger keeps pointing at the same function,
-- so no trigger churn. The vw_procurement_vaccination_excluded_goats view already covers every
-- terminal lifecycle state (dead, sold, culled, transferred, lost, merged, inactive) and is left
-- untouched. Clinical holds for still-active animals are unaffected: an 'alive'/in-care animal is not
-- in the excluded view, so it may still receive 'deferred' work.

CREATE OR REPLACE FUNCTION block_active_vaccination_for_procurement_excluded_goat()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  excluded_reason text;
  is_vaccination boolean;
BEGIN
  IF NEW.target_type <> 'goat'
     OR NEW.status NOT IN ('scheduled', 'due', 'in_progress', 'deferred', 'missed', 'waived') THEN
    RETURN NEW;
  END IF;

  SELECT EXISTS (
    SELECT 1
    FROM protocol_versions pv
    JOIN protocol_definitions pd
      ON pd.tenant_id = pv.tenant_id
     AND pd.protocol_id = pv.protocol_id
    WHERE pv.tenant_id = NEW.tenant_id
      AND pv.protocol_version_id = NEW.protocol_version_id
      AND pd.category = 'vaccination'
  ) INTO is_vaccination;

  IF NOT is_vaccination THEN
    RETURN NEW;
  END IF;

  SELECT exclusion_reason
    INTO excluded_reason
    FROM vw_procurement_vaccination_excluded_goats ex
    WHERE ex.tenant_id = NEW.tenant_id
      AND ex.goat_id = NEW.target_id
    LIMIT 1;

  IF excluded_reason IS NOT NULL THEN
    RAISE EXCEPTION 'vaccination_obligation_blocked_for_procurement_excluded_goat: goat %, reason %', NEW.target_id, excluded_reason
      USING ERRCODE = '23514';
  END IF;

  RETURN NEW;
END;
$$;

-- Cancel any 'deferred' vaccination obligations that already leaked through the pre-fix gap for
-- excluded animals. Terminally changed to 'canceled' so they cannot reappear in Calendar. Mirrors the
-- 000083 backfill; only open 'deferred' rows are touched, completed/accepted history is never deleted
-- or altered.
UPDATE obligation_instances oi
SET status = 'canceled',
    updated_at = now(),
    row_version = row_version + 1
WHERE oi.target_type = 'goat'
  AND oi.status = 'deferred'
  AND EXISTS (
    SELECT 1
    FROM protocol_versions pv
    JOIN protocol_definitions pd
      ON pd.tenant_id = pv.tenant_id
     AND pd.protocol_id = pv.protocol_id
    WHERE pv.tenant_id = oi.tenant_id
      AND pv.protocol_version_id = oi.protocol_version_id
      AND pd.category = 'vaccination'
  )
  AND EXISTS (
    SELECT 1
    FROM vw_procurement_vaccination_excluded_goats ex
    WHERE ex.tenant_id = oi.tenant_id
      AND ex.goat_id = oi.target_id
  );

-- +goose Down
-- Restore the pre-fix guard function (blocked-status list without 'deferred'). The backfilled
-- cancellations are terminal history and are intentionally not reverted.
CREATE OR REPLACE FUNCTION block_active_vaccination_for_procurement_excluded_goat()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  excluded_reason text;
  is_vaccination boolean;
BEGIN
  IF NEW.target_type <> 'goat'
     OR NEW.status NOT IN ('scheduled', 'due', 'in_progress', 'missed', 'waived') THEN
    RETURN NEW;
  END IF;

  SELECT EXISTS (
    SELECT 1
    FROM protocol_versions pv
    JOIN protocol_definitions pd
      ON pd.tenant_id = pv.tenant_id
     AND pd.protocol_id = pv.protocol_id
    WHERE pv.tenant_id = NEW.tenant_id
      AND pv.protocol_version_id = NEW.protocol_version_id
      AND pd.category = 'vaccination'
  ) INTO is_vaccination;

  IF NOT is_vaccination THEN
    RETURN NEW;
  END IF;

  SELECT exclusion_reason
    INTO excluded_reason
    FROM vw_procurement_vaccination_excluded_goats ex
    WHERE ex.tenant_id = NEW.tenant_id
      AND ex.goat_id = NEW.target_id
    LIMIT 1;

  IF excluded_reason IS NOT NULL THEN
    RAISE EXCEPTION 'vaccination_obligation_blocked_for_procurement_excluded_goat: goat %, reason %', NEW.target_id, excluded_reason
      USING ERRCODE = '23514';
  END IF;

  RETURN NEW;
END;
$$;
