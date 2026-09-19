-- +goose Up
-- ===========================================================================
-- D0 tenant isolation (docs/ceo-ai/plan-v3-one-brain-two-doors.md, "DB role"
-- row): mesha_ceo_readonly holds SELECT on ceo_ai.* ONLY.
--
-- Migration 000031 (collapsed into the 000001 baseline around the
-- "Collapsed from 000031" block) granted the two assistant roles SELECT on ALL
-- TABLES IN SCHEMA public because governed Cube metrics read raw public tables.
-- That reasoning holds for mesha_cube_readonly (Cube Core's DB user) and is
-- left untouched here. It does NOT hold for mesha_ceo_readonly: that role is
-- the model-drafted SQL fallback + MCP Toolbox reader, and every read it makes
-- is supposed to be confined to the governed ceo_ai.* views by the sqlguard
-- allowlist. Holding public.* SELECT as well meant that any guard bypass would
-- expose every raw operator table of every tenant. This is a blast-radius
-- reduction, not a CEO restriction: the ceo_ai views run with their OWNER's
-- privileges, so the role still sees exactly the governed projections.
--
-- The one server-authored ("trusted") read that used to join public.* directly
-- (healthIssueSQL, backend/internal/ceoai/app/natural_sql.go) now reads
-- ceo_ai.source_entry_health_status, which this migration extends with the
-- park_location_id column that read needs for park scoping (append-only; the
-- existing column order is preserved so CREATE OR REPLACE is safe). The
-- rebuild also collapses arrival_intake_reviews to the latest review per load
-- (the baseline joined it 0..N and doubled a re-reviewed load's row).
--
-- Guarded on role existence, exactly like the baseline grant blocks: the
-- readonly roles are provisioned per environment
-- (tools/dev/setup-ceo-ai-local-role.sh + Secret Manager), not by a migration.
-- Idempotent and safe to re-run. Proven by
-- backend/internal/ceoai/reporting/tenant_views_test.go
-- (TestAssistantRoleCannotReadPublic, TestEveryCeoAiViewHasTenantID,
--  TestTrustedSQL_HealthIssue_TenantScoped).
-- ===========================================================================

-- projection-review: membership=procurement_loads rows (one row per load); group_key=(tenant_id, load_id) — hc and ev CTEs pre-aggregate the many sides (procurement_source_health_checks, procurement_hf_vaccination_evidence) to exactly one row per (tenant_id, load_id) before the join, so the load grain never fans out; join_cardinality=parties/locations are 1:0..1 by primary key, arrival_intake_reviews is 0..N per load (a re-review appends a row, no unique on (tenant_id, load_id)) and is collapsed to the LATEST review by (reviewed_at DESC, review_id DESC) LIMIT 1 inside a LATERAL before the join — the baseline view LEFT JOINed it directly and doubled a re-reviewed load; pagination=view is the whole per-load set, consumers page with ORDER BY health_blockers DESC LIMIT 50; scope=tenant_id column exposed on every row, park_location_id appended (D0) for the park-scoped trusted read.
-- Grain proofs: backend/internal/ceoai/reporting/tenant_views_test.go
-- (TestSourceEntryHealthStatusOneToManyReviews, TestSourceEntryHealthStatusPageBoundary,
--  TestTrustedSQL_HealthIssue_TenantScoped).
-- +goose StatementBegin
CREATE OR REPLACE VIEW ceo_ai.source_entry_health_status AS
WITH hc AS (
    SELECT tenant_id, load_id,
           COUNT(*) FILTER (WHERE health_state IN ('blocked','failed','sick','quarantine'))::bigint AS health_blockers
    FROM procurement_source_health_checks
    GROUP BY tenant_id, load_id
),
ev AS (
    SELECT tenant_id, load_id,
           COUNT(*)::bigint AS evidence_total,
           COUNT(*) FILTER (WHERE review_status IN ('pending','unreviewed','needs_review'))::bigint AS evidence_pending
    FROM procurement_hf_vaccination_evidence
    GROUP BY tenant_id, load_id
)
SELECT
    l.tenant_id                                   AS tenant_id,
    ('Load ' || left(l.load_id::text, 8) ||
        COALESCE(' · ' || to_char(l.purchase_date, 'DD Mon'), '')) AS load_label,
    COALESCE(pt.display_name, loc.name)           AS source_label,
    COALESCE(air.expected_count, l.expected_count) AS animals_expected,
    air.arrived_count                             AS animals_received,
    air.matched_count                             AS animals_accepted,
    air.rejected_count                            AS animals_rejected,
    COALESCE(hc.health_blockers, 0)               AS health_blockers,
    CASE
        WHEN ev.evidence_total IS NULL OR ev.evidence_total = 0 THEN 'no_evidence'
        WHEN ev.evidence_pending > 0 THEN 'evidence_pending'
        ELSE 'evidence_complete'
    END                                           AS evidence_status,
    -- D0 (000359): appended so the trusted source-entry health read can scope
    -- by park without touching public.* itself.
    air.park_location_id                          AS park_location_id
FROM procurement_loads l
LEFT JOIN parties   pt  ON pt.party_id     = l.source_party_id
LEFT JOIN locations loc ON loc.location_id = l.source_location_id
LEFT JOIN LATERAL (
    SELECT r.expected_count, r.arrived_count, r.matched_count, r.rejected_count, r.park_location_id
    FROM arrival_intake_reviews r
    WHERE r.tenant_id = l.tenant_id AND r.load_id = l.load_id
    ORDER BY r.reviewed_at DESC, r.review_id DESC
    LIMIT 1
) air ON true
LEFT JOIN hc ON hc.tenant_id = l.tenant_id AND hc.load_id = l.load_id
LEFT JOIN ev ON ev.tenant_id = l.tenant_id AND ev.load_id = l.load_id;
-- +goose StatementEnd

-- +goose StatementBegin
DO $d0_grants$
DECLARE
    r text := 'mesha_ceo_readonly';
    owner_role text;
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = r) THEN
        -- 1. Drop the public.* read the 000031 block granted. Schema USAGE on
        --    public is kept: it grants nothing readable by itself, and revoking
        --    it is not needed for the views (they resolve with owner rights).
        EXECUTE format('REVOKE SELECT ON ALL TABLES IN SCHEMA public FROM %I', r);
        -- 2. Stop future public tables from auto-granting, for every owner that
        --    currently has a default-privilege entry naming this grantee (the
        --    baseline used the migration role; the grant script may have added
        --    others via ALTER DEFAULT PRIVILEGES FOR ROLE <owner>).
        FOR owner_role IN
            SELECT DISTINCT pg_get_userbyid(d.defaclrole)
            FROM pg_default_acl d
            JOIN pg_namespace n ON n.oid = d.defaclnamespace
            WHERE n.nspname = 'public' AND d.defaclobjtype = 'r'
        LOOP
            BEGIN
                EXECUTE format('ALTER DEFAULT PRIVILEGES FOR ROLE %I IN SCHEMA public REVOKE SELECT ON TABLES FROM %I', owner_role, r);
            EXCEPTION WHEN insufficient_privilege THEN
                RAISE WARNING 'D0: could not revoke default public SELECT for future tables owned by % from %; re-run tools/dev/grant-assistant-public-read.sh as a member of that owner', owner_role, r;
            END;
        END LOOP;
        EXECUTE format('ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE SELECT ON TABLES FROM %I', r);
        -- 3. Keep (re-assert) the governed reporting schema.
        EXECUTE format('GRANT USAGE ON SCHEMA ceo_ai TO %I', r);
        EXECUTE format('GRANT SELECT ON ALL TABLES IN SCHEMA ceo_ai TO %I', r);
        EXECUTE format('ALTER DEFAULT PRIVILEGES IN SCHEMA ceo_ai GRANT SELECT ON TABLES TO %I', r);
    END IF;
END;
$d0_grants$;
-- +goose StatementEnd

-- The Cube role keeps SELECT on the extended view too (mirrors the baseline
-- per-view grant blocks; a no-op where the role does not exist).
-- +goose StatementBegin
DO $view_grants$
DECLARE
    r text;
BEGIN
    FOREACH r IN ARRAY ARRAY['mesha_ceo_readonly','mesha_cube_readonly'] LOOP
        IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = r) THEN
            EXECUTE format('GRANT SELECT ON ceo_ai.source_entry_health_status TO %I', r);
        END IF;
    END LOOP;
END;
$view_grants$;
-- +goose StatementEnd

-- +goose Down
-- Restore the 000031 posture for mesha_ceo_readonly (public.* SELECT, current +
-- future tables of the migration role). The view keeps park_location_id: it is
-- an appended column and dropping it would break any already-deployed reader.
-- +goose StatementBegin
DO $d0_grants_down$
DECLARE
    r text := 'mesha_ceo_readonly';
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = r) THEN
        EXECUTE format('GRANT USAGE ON SCHEMA public TO %I', r);
        EXECUTE format('GRANT SELECT ON ALL TABLES IN SCHEMA public TO %I', r);
        EXECUTE format('ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO %I', r);
    END IF;
END;
$d0_grants_down$;
-- +goose StatementEnd
