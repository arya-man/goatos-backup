-- +goose Up
-- +goose StatementBegin
-- ===========================================================================
-- ceo_ai CUBE SOURCE views — governed, business-language read surfaces that the
-- Cube Core semantic layer reads FROM, so Cube never touches raw public tables.
--
-- WHY THIS EXISTS
-- Cube Core connects to Postgres as the `mesha_cube_readonly` role, which by
-- design has `REVOKE ALL ON SCHEMA public` and SELECT on `ceo_ai.*` ONLY
-- (tools/dev/setup-ceo-ai-local-role.sh). But five Cube models still ran inline
-- SQL `FROM obligation_instances / goats / feed_direction_completions /
-- procurement_loads / sop_tasks` — raw public tables. Inline-SQL cubes execute
-- with the connecting role's own privileges (no view-owner indirection), so
-- every such query failed with `permission denied for table obligation_instances`
-- and the leadership assistant returned `cube: could not be retrieved.`
--
-- FIX (matches migration 000027, which already repointed the operator cube):
-- expose one thin passthrough view per cube at the SAME row grain and with the
-- SAME column names the cube models already select. These views are owned by the
-- migration role, so Postgres runs them with owner rights (default view
-- behavior, security_invoker off) and the read-only Cube role can SELECT them
-- without any grant on public. The measure FORMULAS live in the Cube models and
-- are unchanged; only the FROM source moves from raw public to ceo_ai.*.
--
-- GRAIN (unchanged, one row per):
--   ceo_ai.vaccination_obligations_base  -> obligation_instances (per obligation)
--   ceo_ai.animals_base                  -> goats               (per goat)
--   ceo_ai.feed_completions_base         -> feed_direction_completions (per completion)
--   ceo_ai.procurement_loads_base        -> procurement_loads   (per load)
--   ceo_ai.workforce_tasks_base          -> sop_tasks           (per task)
--
-- IST business calendar: business-day columns convert to Asia/Kolkata first,
-- never the UTC instant, identical to the cube inline SQL they replace.
-- ===========================================================================

CREATE OR REPLACE VIEW ceo_ai.vaccination_obligations_base AS
SELECT
    o.obligation_id                                    AS obligation_id,
    o.tenant_id                                        AS tenant_id,
    o.status                                           AS status,
    o.due_at                                           AS due_at,
    o.completed_at                                     AS completed_at,
    o.scope_id                                         AS shed_id,
    sh.name                                            AS shed_label,
    sh.parent_location_id                              AS park_id,
    pk.name                                            AS park_label,
    g.species                                          AS species,
    (o.due_at AT TIME ZONE 'Asia/Kolkata')::date       AS due_business_day,
    (o.completed_at AT TIME ZONE 'Asia/Kolkata')::date AS completed_business_day
FROM obligation_instances o
LEFT JOIN locations sh ON sh.location_id = o.scope_id
LEFT JOIN locations pk ON pk.location_id = sh.parent_location_id
LEFT JOIN goats    g  ON g.goat_id      = o.target_id;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE VIEW ceo_ai.animals_base AS
SELECT
    g.goat_id                                          AS goat_id,
    g.tenant_id                                        AS tenant_id,
    g.species                                          AS species,
    g.lifecycle_status                                 AS lifecycle_status,
    g.management_stage                                 AS management_stage,
    g.park_id                                          AS park_id,
    pk.name                                            AS park_label,
    g.shed_id                                          AS shed_id,
    sh.name                                            AS shed_label,
    g.entry_date                                       AS entry_date,
    (g.exited_at AT TIME ZONE 'Asia/Kolkata')::date    AS exit_business_day,
    g.exit_reason                                      AS exit_reason
FROM goats g
LEFT JOIN locations pk ON pk.location_id = g.park_id
LEFT JOIN locations sh ON sh.location_id = g.shed_id;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE VIEW ceo_ai.feed_completions_base AS
SELECT
    c.completion_id                                    AS completion_id,
    c.tenant_id                                        AS tenant_id,
    c.shed_id                                          AS shed_id,
    sh.name                                            AS shed_label,
    sh.parent_location_id                              AS park_id,
    pk.name                                            AS park_label,
    c.quantity_fed                                     AS quantity_fed,
    c.head_count                                       AS head_count,
    c.status                                           AS status,
    (c.fed_at AT TIME ZONE 'Asia/Kolkata')::date       AS fed_business_day
FROM feed_direction_completions c
LEFT JOIN locations sh ON sh.location_id = c.shed_id
LEFT JOIN locations pk ON pk.location_id = sh.parent_location_id;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE VIEW ceo_ai.procurement_loads_base AS
SELECT
    pl.load_id                                         AS load_id,
    pl.tenant_id                                       AS tenant_id,
    pl.status                                          AS status,
    pl.expected_count                                  AS expected_count,
    pl.source_location_id                              AS source_location_id,
    loc.name                                           AS source_label,
    (pl.purchase_date)                                 AS purchase_date,
    (pl.created_at AT TIME ZONE 'Asia/Kolkata')::date  AS entered_business_day
FROM procurement_loads pl
LEFT JOIN locations loc ON loc.location_id = pl.source_location_id;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE VIEW ceo_ai.workforce_tasks_base AS
SELECT
    t.task_id                                          AS task_id,
    t.tenant_id                                        AS tenant_id,
    t.state                                            AS state,
    t.task_type                                        AS task_type,
    t.assigned_to                                      AS operator_id,
    t.scope_id                                         AS scope_id,
    sh.name                                            AS shed_label,
    sh.parent_location_id                              AS park_id,
    pk.name                                            AS park_label,
    t.verified_at                                      AS verified_at,
    (t.due_at AT TIME ZONE 'Asia/Kolkata')::date       AS due_business_day
FROM sop_tasks t
LEFT JOIN locations sh ON sh.location_id = t.scope_id
LEFT JOIN locations pk ON pk.location_id = sh.parent_location_id;
-- +goose StatementEnd

-- ===========================================================================
-- GUARDED GRANTS — grant SELECT on the new cube source views to the read-only
-- roles if they exist (idempotent; mirrors migration 000027).
-- ===========================================================================
-- +goose StatementBegin
DO $grants$
DECLARE
    r text;
    v text;
BEGIN
    FOREACH r IN ARRAY ARRAY['mesha_ceo_readonly','mesha_cube_readonly'] LOOP
        IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = r) THEN
            FOREACH v IN ARRAY ARRAY[
                'ceo_ai.vaccination_obligations_base',
                'ceo_ai.animals_base',
                'ceo_ai.feed_completions_base',
                'ceo_ai.procurement_loads_base',
                'ceo_ai.workforce_tasks_base'
            ] LOOP
                EXECUTE format('GRANT SELECT ON %s TO %I', v, r);
            END LOOP;
        END IF;
    END LOOP;
END;
$grants$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP VIEW IF EXISTS ceo_ai.workforce_tasks_base;
DROP VIEW IF EXISTS ceo_ai.procurement_loads_base;
DROP VIEW IF EXISTS ceo_ai.feed_completions_base;
DROP VIEW IF EXISTS ceo_ai.animals_base;
DROP VIEW IF EXISTS ceo_ai.vaccination_obligations_base;
-- +goose StatementEnd
