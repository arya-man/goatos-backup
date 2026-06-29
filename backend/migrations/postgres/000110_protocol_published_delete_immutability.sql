-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ensure_protocol_child_version_is_draft()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  parent_status text;
  child_tenant_id uuid;
  child_version_id uuid;
BEGIN
  IF TG_OP = 'DELETE' THEN
    child_tenant_id := OLD.tenant_id;
    child_version_id := OLD.protocol_version_id;
  ELSE
    child_tenant_id := NEW.tenant_id;
    child_version_id := NEW.protocol_version_id;
  END IF;

  SELECT status
  INTO parent_status
  FROM protocol_versions
  WHERE tenant_id = child_tenant_id
    AND protocol_version_id = child_version_id;

  IF parent_status IS NULL THEN
    RAISE EXCEPTION 'protocol version % does not exist for tenant %', child_version_id, child_tenant_id
      USING ERRCODE = '23503';
  END IF;

  IF parent_status <> 'draft' THEN
    RAISE EXCEPTION 'protocol version % is %, not draft; published config is immutable',
      child_version_id, parent_status
      USING ERRCODE = '23514';
  END IF;

  IF TG_OP = 'DELETE' THEN
    RETURN OLD;
  END IF;
  RETURN NEW;
END;
$$;

-- Historical drafts/dev rows could have age-window repeat values from the early CHECK below.
-- The generator never executed those values, and new authoring now rejects them. Normalize before
-- the immutability triggers are installed so tightening the CHECK cannot abort on existing data.
UPDATE protocol_rules
SET repeat_until_after_age = COALESCE(NULLIF(repeat_until_after_age, ''), repeat),
    eligibility_json = COALESCE(eligibility_json, '{}'::jsonb) ||
      jsonb_build_object('_legacy_unsupported_repeat', repeat),
    repeat = 'none'
WHERE repeat IN ('until_age', 'after_age');

DROP TRIGGER IF EXISTS protocol_rules_require_draft_version_trg ON protocol_rules;
CREATE TRIGGER protocol_rules_require_draft_version_trg
BEFORE INSERT OR UPDATE OR DELETE ON protocol_rules
FOR EACH ROW
EXECUTE FUNCTION ensure_protocol_child_version_is_draft();

DROP TRIGGER IF EXISTS protocol_triggers_require_draft_version_trg ON protocol_triggers;
CREATE TRIGGER protocol_triggers_require_draft_version_trg
BEFORE INSERT OR UPDATE OR DELETE ON protocol_triggers
FOR EACH ROW
EXECUTE FUNCTION ensure_protocol_child_version_is_draft();

CREATE OR REPLACE FUNCTION prevent_published_protocol_version_delete()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF OLD.status IN ('published', 'retired') THEN
    RAISE EXCEPTION 'published or retired protocol version % is immutable; create a new draft version',
      OLD.protocol_version_id
      USING ERRCODE = '23514';
  END IF;
  RETURN OLD;
END;
$$;

DROP TRIGGER IF EXISTS protocol_versions_published_no_delete_trg ON protocol_versions;
CREATE TRIGGER protocol_versions_published_no_delete_trg
BEFORE DELETE ON protocol_versions
FOR EACH ROW
EXECUTE FUNCTION prevent_published_protocol_version_delete();

ALTER TABLE protocol_rules DROP CONSTRAINT IF EXISTS protocol_rules_repeat_check;
ALTER TABLE protocol_rules
ADD CONSTRAINT protocol_rules_repeat_check
CHECK (repeat IN ('none', 'every_n_days', 'yearly'));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE protocol_rules DROP CONSTRAINT IF EXISTS protocol_rules_repeat_check;
ALTER TABLE protocol_rules
ADD CONSTRAINT protocol_rules_repeat_check
CHECK (repeat IN ('none', 'every_n_days', 'yearly', 'until_age', 'after_age'));

DROP TRIGGER IF EXISTS protocol_versions_published_no_delete_trg ON protocol_versions;
DROP FUNCTION IF EXISTS prevent_published_protocol_version_delete();

DROP TRIGGER IF EXISTS protocol_triggers_require_draft_version_trg ON protocol_triggers;
CREATE TRIGGER protocol_triggers_require_draft_version_trg
BEFORE INSERT OR UPDATE ON protocol_triggers
FOR EACH ROW
EXECUTE FUNCTION ensure_protocol_child_version_is_draft();

DROP TRIGGER IF EXISTS protocol_rules_require_draft_version_trg ON protocol_rules;
CREATE TRIGGER protocol_rules_require_draft_version_trg
BEFORE INSERT OR UPDATE ON protocol_rules
FOR EACH ROW
EXECUTE FUNCTION ensure_protocol_child_version_is_draft();
-- +goose StatementEnd
