-- +goose Up
-- An operator belongs to EXACTLY ONE park, and nothing may hand them work in another one.
--
-- This lived only in the service before, which is not a guarantee: a seed, an HRMS import, a
-- backfill or a hand-written UPDATE all bypass it. The observed failure was a CPT-scoped operator
-- assigned a CBE shed and accepted silently -- the work list filters on operator_user_id alone and
-- the write only checks that the caller is the assignee, so that person both SAW and could weigh
-- another park's shed. The rule belongs where every writer meets it.
--
-- WHO may hold a bucket in a park:
--   * anyone with an ACTIVE tenant-scoped grant -- the weighing director covers CBE and CPT both,
--     and the vaccination director likewise for his own module;
--   * anyone with an ACTIVE park-scoped grant naming THAT park.
-- An unassigned bucket (NULL operator) is allowed: it is work nobody has been given yet.
--
-- Deliberately NOT enforced as a foreign key or CHECK: the answer depends on another table
-- (user_scope_grants) and on grant status, which a CHECK cannot see.

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION weighing_operator_park_bound_guard() RETURNS trigger AS $$
DECLARE
  park uuid;
BEGIN
  IF NEW.operator_user_id IS NULL THEN
    RETURN NEW;
  END IF;

  -- park_id is denormalised onto the bucket; fall back to the campaign when a writer omits it.
  park := NEW.park_id;
  IF park IS NULL THEN
    SELECT c.park_id INTO park
    FROM weighing_campaigns c
    WHERE c.campaign_id = NEW.campaign_id AND c.tenant_id = NEW.tenant_id;
  END IF;
  IF park IS NULL THEN
    RETURN NEW;
  END IF;

  IF EXISTS (
    SELECT 1 FROM user_scope_grants g
    WHERE g.tenant_id = NEW.tenant_id
      AND g.user_id = NEW.operator_user_id
      AND g.status = 'active'
      AND (g.scope_type = 'tenant' OR (g.scope_type = 'park' AND g.scope_id = park))
  ) THEN
    RETURN NEW;
  END IF;

  RAISE EXCEPTION 'weighing: operator % is not scoped to park % (operators are single-park; only a tenant-scoped director spans parks)',
    NEW.operator_user_id, park
    USING ERRCODE = '23514';
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS weighing_campaign_sheds_operator_park_bound ON weighing_campaign_sheds;

CREATE TRIGGER weighing_campaign_sheds_operator_park_bound
  BEFORE INSERT OR UPDATE OF operator_user_id, park_id, campaign_id ON weighing_campaign_sheds
  FOR EACH ROW EXECUTE FUNCTION weighing_operator_park_bound_guard();

-- +goose Down
DROP TRIGGER IF EXISTS weighing_campaign_sheds_operator_park_bound ON weighing_campaign_sheds;
DROP FUNCTION IF EXISTS weighing_operator_park_bound_guard();
