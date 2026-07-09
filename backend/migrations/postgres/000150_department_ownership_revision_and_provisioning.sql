-- +goose Up
-- Wire department-ownership consumption into the bootstraps (ADR review follow-up
-- P2) + open the email-sign-in provisioning path for department assignment.
-- See docs/decisions/user-module-ownership-and-nav-chrome.md:109-117.
--
-- 1) Bootstrap-revision triggers: department ownership now drives visible nav +
--    nav chrome on /admin-web/bootstrap (compiled + cached). A change to a
--    department, a department's module grants, or a member's department must
--    invalidate that cache, exactly as user_scope_grants does (000105 mirror:
--    admin_ui_bump_row_family_trg('permissions')). All three tables carry
--    tenant_id, which the shared trigger fn reads.
CREATE TRIGGER admin_ui_departments_revision_trg
  AFTER INSERT OR UPDATE OR DELETE ON departments
  FOR EACH ROW EXECUTE FUNCTION admin_ui_bump_row_family_trg('permissions');

CREATE TRIGGER admin_ui_department_module_grants_revision_trg
  AFTER INSERT OR UPDATE OR DELETE ON department_module_grants
  FOR EACH ROW EXECUTE FUNCTION admin_ui_bump_row_family_trg('permissions');

-- Only a department reassignment (or member add/remove) changes owned modules;
-- scope the UPDATE to department_id so ordinary roster edits don't churn the
-- permissions family.
CREATE TRIGGER admin_ui_workforce_members_department_revision_trg
  AFTER INSERT OR DELETE OR UPDATE OF department_id ON workforce_members
  FOR EACH ROW EXECUTE FUNCTION admin_ui_bump_row_family_trg('permissions');

-- 2) Email-sign-in department provisioning. Goat OS sign-in is email-always;
--    leadership/admin actors get a user_scope_grant at claim time but no
--    workforce_member, so the member->department->grants ownership chain has no
--    row to read for them. This nullable, non-PII code lets an approved email
--    grant carry the HR department its holder belongs to (sourced operationally
--    from the HR attendance register, never committed here). On claim, the
--    permissions layer upserts a workforce_member for the actor into this
--    department, making the ownership chain real for every principal.
--    Format mirrors departments_code_check so it can resolve departments.code.
ALTER TABLE auth_pending_email_grants
  ADD COLUMN department_code text NULL;
ALTER TABLE auth_pending_email_grants
  ADD CONSTRAINT auth_pending_email_grants_department_code_check
  CHECK (department_code IS NULL OR department_code ~ '^[a-z][a-z0-9_]*$');

-- +goose Down
ALTER TABLE auth_pending_email_grants DROP CONSTRAINT IF EXISTS auth_pending_email_grants_department_code_check;
ALTER TABLE auth_pending_email_grants DROP COLUMN IF EXISTS department_code;
DROP TRIGGER IF EXISTS admin_ui_workforce_members_department_revision_trg ON workforce_members;
DROP TRIGGER IF EXISTS admin_ui_department_module_grants_revision_trg ON department_module_grants;
DROP TRIGGER IF EXISTS admin_ui_departments_revision_trg ON departments;
