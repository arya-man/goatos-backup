-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ensure_protocol_child_version_is_draft()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  parent_status text;
BEGIN
  SELECT status
  INTO parent_status
  FROM protocol_versions
  WHERE tenant_id = NEW.tenant_id
    AND protocol_version_id = NEW.protocol_version_id;

  IF parent_status IS NULL THEN
    RAISE EXCEPTION 'protocol version % does not exist for tenant %', NEW.protocol_version_id, NEW.tenant_id
      USING ERRCODE = '23503';
  END IF;

  IF parent_status <> 'draft' THEN
    RAISE EXCEPTION 'protocol version % is %, not draft; published config is immutable',
      NEW.protocol_version_id, parent_status
      USING ERRCODE = '23514';
  END IF;

  RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS protocol_rules_require_draft_version_trg ON protocol_rules;
CREATE TRIGGER protocol_rules_require_draft_version_trg
BEFORE INSERT OR UPDATE ON protocol_rules
FOR EACH ROW
EXECUTE FUNCTION ensure_protocol_child_version_is_draft();

DROP TRIGGER IF EXISTS protocol_triggers_require_draft_version_trg ON protocol_triggers;
CREATE TRIGGER protocol_triggers_require_draft_version_trg
BEFORE INSERT OR UPDATE ON protocol_triggers
FOR EACH ROW
EXECUTE FUNCTION ensure_protocol_child_version_is_draft();

CREATE OR REPLACE FUNCTION ensure_published_protocol_version_is_immutable()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF OLD.status = 'published' THEN
    IF NEW.status NOT IN ('published', 'retired') THEN
      RAISE EXCEPTION 'published protocol version % cannot move back to status %',
        OLD.protocol_version_id, NEW.status
        USING ERRCODE = '23514';
    END IF;

    IF NEW.protocol_id IS DISTINCT FROM OLD.protocol_id
      OR NEW.scope_type IS DISTINCT FROM OLD.scope_type
      OR NEW.scope_id IS DISTINCT FROM OLD.scope_id
      OR NEW.version IS DISTINCT FROM OLD.version
      OR NEW.version_label IS DISTINCT FROM OLD.version_label
      OR NEW.effective_from IS DISTINCT FROM OLD.effective_from
      OR NEW.effective_to IS DISTINCT FROM OLD.effective_to
      OR NEW.rule_dsl IS DISTINCT FROM OLD.rule_dsl
      OR NEW.proof_policy IS DISTINCT FROM OLD.proof_policy
      OR NEW.sop_version_id IS DISTINCT FROM OLD.sop_version_id
      OR NEW.drafted_by IS DISTINCT FROM OLD.drafted_by
      OR NEW.published_by IS DISTINCT FROM OLD.published_by
      OR NEW.published_at IS DISTINCT FROM OLD.published_at THEN
      RAISE EXCEPTION 'published protocol version % is immutable; create a new version for config changes',
        OLD.protocol_version_id
        USING ERRCODE = '23514';
    END IF;
  END IF;

  RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS protocol_versions_published_immutable_trg ON protocol_versions;
CREATE TRIGGER protocol_versions_published_immutable_trg
BEFORE UPDATE ON protocol_versions
FOR EACH ROW
EXECUTE FUNCTION ensure_published_protocol_version_is_immutable();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS protocol_versions_published_immutable_trg ON protocol_versions;
DROP TRIGGER IF EXISTS protocol_triggers_require_draft_version_trg ON protocol_triggers;
DROP TRIGGER IF EXISTS protocol_rules_require_draft_version_trg ON protocol_rules;
DROP FUNCTION IF EXISTS ensure_published_protocol_version_is_immutable();
DROP FUNCTION IF EXISTS ensure_protocol_child_version_is_draft();
-- +goose StatementEnd
