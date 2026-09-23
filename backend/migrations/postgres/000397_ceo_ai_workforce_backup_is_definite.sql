-- +goose Up
--
-- ceo_ai.workforce_coverage_status.backup_label STOPS BEING AMBIGUOUS.
--
-- THE DEFECT, AND WHY IT KEPT COMING BACK. Asked which staff have no backup,
-- the planner wrote `backup_label IS NULL` and named TEN of sixteen roles as
-- staffing gaps when FOUR are -- among them `Backup 6`, a person who IS
-- somebody's backup. The view filled backup_label only on the
-- covered_by_backup rows, so a blank one meant "nobody is away from this
-- role", not "this role has no cover". One column carried two different
-- meanings and nothing on the column said which.
--
-- What made it dangerous is that the wrong filter RETURNS ROWS. A predicate
-- matching nothing shows up as an empty answer and gets questioned; one
-- matching everything reads as a real finding, and no downstream check -- not
-- the guard, not the composer, not the reader -- can tell it did nothing.
--
-- Three repairs were tried at the READING end, and each one lost the same
-- argument. Prose on the schema card ("blank backup_label is not the gap
-- test") was re-spelled around. A gate matching the SHAPE of the predicate was
-- re-spelled around seven ways in one review round. A gate that READ the
-- predicate as an expression and evaluated it three times was re-spelled
-- around again, twice more: a CASE with no ELSE arm, `(backup_label IS NOT
-- NULL) = false`, a wrapper on the literal side. That is not a gate losing to
-- a clever attacker. It is an arms race against a model that can spell one
-- predicate infinitely many ways, and it cannot be won at the reading end.
--
-- SO THE AMBIGUITY IS REMOVED AT THE SOURCE. backup_label is now NEVER NULL
-- and never blank. It carries one of three definite values:
--
--     <the covering person's name>   somebody is away and this person covers
--     'No backup named'              somebody is away and NOBODY covers
--     'Nobody away'                  this role is present
--
-- The 10-versus-4 defect is now INEXPRESSIBLE rather than merely refused.
-- `backup_label IS NULL`, `= ''`, `coalesce(backup_label,'') = ''`,
-- `length(backup_label) = 0`, `CASE WHEN backup_label IS NULL THEN 1 END = 1`
-- and every other spelling of "is it empty" return ZERO ROWS -- an empty
-- answer, which is the failure shape a reader CAN see. And the question the
-- leader actually asked has a correct, obvious spelling for the first time:
-- `backup_label = 'No backup named'` returns exactly the four uncovered roles,
-- the same four `coverage_status = 'uncovered_absence'` returns.
--
-- The column stays USEFUL, which is why it is given definite values rather
-- than dropped from the card. "Who is the backup for the Feed Director" is a
-- real leadership question and its answer is a person's NAME; removing the
-- column would have made it unanswerable to close a defect in a different
-- question.
--
-- coverage_status is unchanged and is still the authority. The two columns now
-- agree by CONSTRUCTION -- both are computed from the same
-- `aa.replacement_member_id` -- rather than by a sentence on a card asking the
-- reader to believe they do.
--
-- Nothing else about the view moves: same name, same column list, same order,
-- same types, same grain, same tenant scoping, same WHERE.
--
-- +goose StatementBegin
CREATE OR REPLACE VIEW ceo_ai.workforce_coverage_status AS
WITH active_absence AS (
    SELECT tenant_id, workforce_member_id, replacement_member_id
    FROM workforce_absences
    WHERE status = 'approved'
      AND now() >= starts_at AND now() < COALESCE(ends_at, 'infinity'::timestamptz)
),
work_load AS (
    -- open assigned SOP tasks per member (bounded aggregate)
    SELECT tenant_id, assigned_to AS user_scope_member,
           COUNT(*) FILTER (WHERE state NOT IN ('completed','verified','canceled'))::bigint AS active_work_count,
           COUNT(*) FILTER (WHERE state NOT IN ('completed','verified','canceled')
                             AND due_at < now())::bigint AS overdue_work_count
    FROM sop_tasks
    WHERE assigned_to IS NOT NULL
    GROUP BY tenant_id, assigned_to
)
SELECT
    wm.tenant_id                                  AS tenant_id,
    loc.name                                      AS park_label,
    COALESCE(rc.label, wm.primary_role_hint)      AS role_label,
    wm.display_name                               AS owner_label,
    -- NEVER NULL, NEVER BLANK. The arms are exhaustive and ordered so that the
    -- named-person case wins: a replacement whose own display_name is somehow
    -- absent still reads as an absence with nobody named, which is the honest
    -- answer and not a blank.
    CASE
        WHEN aa.workforce_member_id IS NULL THEN 'Nobody away'
        WHEN rep.display_name IS NOT NULL AND btrim(rep.display_name) <> '' THEN rep.display_name
        ELSE 'No backup named'
    END                                           AS backup_label,
    CASE
        WHEN aa.workforce_member_id IS NULL THEN 'present'
        WHEN aa.replacement_member_id IS NOT NULL THEN 'covered_by_backup'
        ELSE 'uncovered_absence'
    END                                           AS coverage_status,
    COALESCE(wl.active_work_count, 0)             AS active_work_count,
    COALESCE(wl.overdue_work_count, 0)            AS overdue_work_count
FROM workforce_members wm
LEFT JOIN locations        loc ON loc.location_id = wm.primary_location_id
LEFT JOIN org_role_catalog rc  ON rc.role_key     = wm.primary_role_hint
LEFT JOIN active_absence   aa  ON aa.tenant_id = wm.tenant_id AND aa.workforce_member_id = wm.workforce_member_id
LEFT JOIN workforce_members rep ON rep.workforce_member_id = aa.replacement_member_id
LEFT JOIN work_load        wl  ON wl.tenant_id = wm.tenant_id AND wl.user_scope_member = wm.user_id
WHERE wm.status = 'active';
-- +goose StatementEnd

-- +goose StatementBegin
DO $workforce_coverage_ceo_ai_grants$
DECLARE
    r text;
BEGIN
    FOREACH r IN ARRAY ARRAY['mesha_ceo_readonly','mesha_cube_readonly'] LOOP
        IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = r) THEN
            EXECUTE format('GRANT SELECT ON ceo_ai.workforce_coverage_status TO %I', r);
        END IF;
    END LOOP;
END;
$workforce_coverage_ceo_ai_grants$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE VIEW ceo_ai.workforce_coverage_status AS
WITH active_absence AS (
    SELECT tenant_id, workforce_member_id, replacement_member_id
    FROM workforce_absences
    WHERE status = 'approved'
      AND now() >= starts_at AND now() < COALESCE(ends_at, 'infinity'::timestamptz)
),
work_load AS (
    SELECT tenant_id, assigned_to AS user_scope_member,
           COUNT(*) FILTER (WHERE state NOT IN ('completed','verified','canceled'))::bigint AS active_work_count,
           COUNT(*) FILTER (WHERE state NOT IN ('completed','verified','canceled')
                             AND due_at < now())::bigint AS overdue_work_count
    FROM sop_tasks
    WHERE assigned_to IS NOT NULL
    GROUP BY tenant_id, assigned_to
)
SELECT
    wm.tenant_id                                  AS tenant_id,
    loc.name                                      AS park_label,
    COALESCE(rc.label, wm.primary_role_hint)      AS role_label,
    wm.display_name                               AS owner_label,
    rep.display_name                              AS backup_label,
    CASE
        WHEN aa.workforce_member_id IS NULL THEN 'present'
        WHEN aa.replacement_member_id IS NOT NULL THEN 'covered_by_backup'
        ELSE 'uncovered_absence'
    END                                           AS coverage_status,
    COALESCE(wl.active_work_count, 0)             AS active_work_count,
    COALESCE(wl.overdue_work_count, 0)            AS overdue_work_count
FROM workforce_members wm
LEFT JOIN locations        loc ON loc.location_id = wm.primary_location_id
LEFT JOIN org_role_catalog rc  ON rc.role_key     = wm.primary_role_hint
LEFT JOIN active_absence   aa  ON aa.tenant_id = wm.tenant_id AND aa.workforce_member_id = wm.workforce_member_id
LEFT JOIN workforce_members rep ON rep.workforce_member_id = aa.replacement_member_id
LEFT JOIN work_load        wl  ON wl.tenant_id = wm.tenant_id AND wl.user_scope_member = wm.user_id
WHERE wm.status = 'active';
-- +goose StatementEnd
