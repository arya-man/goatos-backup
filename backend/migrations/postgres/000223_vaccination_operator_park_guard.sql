-- +goose Up
-- Guard vaccination assignment writes at the database boundary. App code also
-- validates this, but manual SQL and repair scripts must not be able to assign
-- a park operator to another park.

CREATE OR REPLACE FUNCTION public.vaccination_operator_must_belong_to_park()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  operator_home uuid;
BEGIN
  IF NEW.operator_id IS NULL THEN
    RETURN NEW;
  END IF;

  SELECT wm.primary_location_id
    INTO operator_home
  FROM public.workforce_members wm
  WHERE wm.tenant_id = NEW.tenant_id
    AND wm.workforce_member_id = NEW.operator_id
    AND wm.status = 'active';

  IF operator_home IS NULL OR operator_home IS DISTINCT FROM NEW.park_id THEN
    RAISE EXCEPTION
      'vaccination operator % does not belong to park %',
      NEW.operator_id, NEW.park_id
      USING ERRCODE = '23514';
  END IF;

  RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS vaccination_drive_assignments_operator_park_guard_trg
  ON public.vaccination_drive_assignments;

CREATE TRIGGER vaccination_drive_assignments_operator_park_guard_trg
BEFORE INSERT OR UPDATE OF operator_id, park_id, tenant_id
ON public.vaccination_drive_assignments
FOR EACH ROW
EXECUTE FUNCTION public.vaccination_operator_must_belong_to_park();

CREATE OR REPLACE FUNCTION public.vaccination_operator_config_must_belong_to_park()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  bad_operator uuid;
BEGIN
  SELECT candidate.operator_id
    INTO bad_operator
  FROM (
    SELECT NEW.default_operator_id AS operator_id
    UNION
    SELECT unnest(COALESCE(NEW.selected_operator_ids, '{}'::uuid[]))
  ) candidate
  LEFT JOIN public.workforce_members wm
    ON wm.tenant_id = NEW.tenant_id
   AND wm.workforce_member_id = candidate.operator_id
   AND wm.status = 'active'
   AND wm.primary_location_id = NEW.park_id
  WHERE candidate.operator_id IS NOT NULL
    AND wm.workforce_member_id IS NULL
  LIMIT 1;

  IF bad_operator IS NOT NULL THEN
    RAISE EXCEPTION
      'vaccination operator config operator % does not belong to park %',
      bad_operator, NEW.park_id
      USING ERRCODE = '23514';
  END IF;

  RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS vaccination_operator_assignment_config_park_guard_trg
  ON public.vaccination_operator_assignment_config;

CREATE TRIGGER vaccination_operator_assignment_config_park_guard_trg
BEFORE INSERT OR UPDATE OF default_operator_id, selected_operator_ids, park_id, tenant_id
ON public.vaccination_operator_assignment_config
FOR EACH ROW
EXECUTE FUNCTION public.vaccination_operator_config_must_belong_to_park();

-- +goose Down
DROP TRIGGER IF EXISTS vaccination_operator_assignment_config_park_guard_trg
  ON public.vaccination_operator_assignment_config;
DROP FUNCTION IF EXISTS public.vaccination_operator_config_must_belong_to_park();

DROP TRIGGER IF EXISTS vaccination_drive_assignments_operator_park_guard_trg
  ON public.vaccination_drive_assignments;
DROP FUNCTION IF EXISTS public.vaccination_operator_must_belong_to_park();
