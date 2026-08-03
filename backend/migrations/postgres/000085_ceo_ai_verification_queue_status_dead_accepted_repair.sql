-- +goose Up
--
-- ceo_ai.verification_queue_status: the `accepted` column has been dead since the
-- 000001 baseline, and this repairs it forward.
--
-- WHAT WAS WRONG. The column was defined as
--   COUNT(*) FILTER (WHERE vi.status IN ('accepted','verified'))
-- while verification_items.status carries a CHECK constraint admitting ONLY
-- 'pending' / 'approved' / 'rejected' (000001 baseline), extended with
-- 'withdrawn' in 000067. NEITHER literal the filter tests for is a legal value
-- of that column, so the FILTER can never match a row and `accepted` returns 0
-- for every tenant, every module and every park -- not "nothing was accepted",
-- but "this column is broken". The vocabulary drifted: the queue's approval
-- verdict is spelled 'approved' on the row and 'accepted' in the view's column
-- name, and nothing ever reconciled the two because a silently-zero count looks
-- exactly like an empty backlog. 000080's weighing sibling view was written
-- against the real constraint and uses 'approved' correctly; this brings the
-- shared cross-module view to the same truth.
--
-- WHY THE FIX IS CORRECT. 'approved' is the ONLY status the verdict path can
-- write for an accepted proof -- it is not merely the likeliest spelling, it is
-- enforced: verification_items_closed_approved_check (000001) requires
-- status = 'approved' for any closed row, so the accepted terminal state is
-- pinned to that literal by constraint, not by convention. Because the old
-- predicate matched nothing at all, no environment holds a non-zero historical
-- `accepted` that this could contradict; the number can only move from a
-- uniformly wrong 0 to the real count.
--
-- WITHDRAWN, AND WHY IT MATCHES 000080. 'withdrawn' (000067) marks a submission
-- that was superseded -- reopen/resubmit, or the duplicate-loser retirement in
-- 000077 -- and is not a verification outcome. 000080's
-- ceo_ai.weighing_verification_status settled the treatment and this view now
-- follows it exactly, because a leader must not get two different answers to the
-- same question depending on which view the assistant picked:
--   * the ROWS ARE KEPT (no WHERE filter). Filtering them out deletes whole
--     scopes from the view, so a shed whose entire verification history was
--     superseded becomes indistinguishable from one that never submitted proof.
--   * `total` EXCLUDES withdrawn via its own FILTER, so the displayed buckets
--     sum to the displayed total by construction.
--   * `withdrawn` and `total_including_withdrawn` are carried as their own
--     columns, so nothing is hidden -- it is merely kept out of the live
--     workload.
--
-- ONE DELIBERATE DIVERGENCE FROM 000080: COLUMN NAMES. 000080 names its buckets
-- rework/verified; this view has shipped pending/rejected/accepted since 000001
-- and keeps those names. Renaming them here would be a breaking change for a
-- reason unrelated to the defect: docs/ceo-ai/mcp-toolbox-tools.yaml's
-- mesha_verification_queue tool SELECTs `rejected` and `accepted` by name, and
-- assistant-authored SQL validated by internal/ceoai/sqlguard addresses these
-- columns directly. The DEFECT is the predicate, not the spelling, so only the
-- predicate changes. The semantics are now identical across the two views:
-- rejected == 000080's rework, accepted == 000080's verified.
--
-- APPEND-ONLY, NOT A REWRITE. CREATE OR REPLACE VIEW may only ADD columns at the
-- END of the list -- inserting `total` before `pending` aborts with
-- `cannot change name of view column "pending" to "total"` on every database
-- that already holds the 000001 shape, which is all of them. `accepted` is
-- therefore repaired IN PLACE at its existing position (same name, same type,
-- new predicate) and the three new columns are appended after owner_label. This
-- also means no DROP VIEW is needed, so no dependent object is invalidated and
-- no grant is lost.
--
-- LOCK SAFETY. CREATE OR REPLACE VIEW takes an ACCESS EXCLUSIVE lock on the view
-- only -- no table DDL, no rewrite, no backfill, nothing touching the
-- verification_items write path. lock_timeout/statement_timeout still bound the
-- acquisition in case a long-running assistant query is mid-scan of the view.
--
-- RE-RUNNABLE. CREATE OR REPLACE is idempotent by definition, and the grant
-- block is guarded on role existence exactly like the 000001 baseline and 000080
-- (the reader roles are provisioned per environment by
-- tools/dev/setup-ceo-ai-local-role.sh + Secret Manager, never by a migration),
-- so a second apply is a no-op.

SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '30s';

-- ===========================================================================
-- ceo_ai.verification_queue_status
--
-- _grain: ONE ROW PER (tenant_id, area, park_label, shed_label) verification
-- rollup, unchanged from 000001. `area` is COALESCE(vi.vertical,
-- 'verification'), so this stays the CROSS-MODULE backlog -- vaccination,
-- counts, feed and weighing all land in it, separated by `area`. 000080's
-- weighing view is the module-scoped companion, not a replacement.
--
-- projection-review: membership=all verification_items rows, no WHERE clause (withdrawn rows are KEPT and excluded from `total` by FILTER instead, so a scope whose entire history was superseded still appears); group_key=(vi.tenant_id, COALESCE(vi.vertical,'verification'), pk.name, sh.name), exactly the GROUP BY list; join_cardinality=locations sh 0..1 per vi.shed_id and locations pk 0..1 per vi.park_id, both matching on locations.location_id which is that table's PK, so neither LEFT JOIN can duplicate a verification_items row and COUNT(*) stays at verification-item grain; pagination=NONE, this is a view and every consumer paginates over it; scope=tenant_id, grouped and exposed as vi.tenant_id
--
-- Ratio key sets: pending, rejected and accepted are FILTER aggregates over the IDENTICAL grouped row set that produces total -- same FROM, same WHERE (there is none), same GROUP BY, no extra join on any branch. verification_items.status carries a CHECK restricting it to {'pending','approved','rejected','withdrawn'} (000001 baseline, extended with 'withdrawn' in 000067); `total` is itself a FILTER that excludes 'withdrawn' (the rows are kept and counted in the separate `withdrawn` column), so the three displayed filters are disjoint and exhaustive against it and pending + rejected + accepted = total for every key, by constraint rather than by convention. total_including_withdrawn = total + withdrawn for every key, for the same reason.
-- ===========================================================================
-- +goose StatementBegin
CREATE OR REPLACE VIEW ceo_ai.verification_queue_status AS
SELECT
    vi.tenant_id                                                        AS tenant_id,
    COALESCE(vi.vertical, 'verification')                               AS area,
    pk.name                                                             AS park_label,
    sh.name                                                             AS shed_label,
    COUNT(*) FILTER (WHERE vi.status = 'pending')::bigint               AS pending,
    COUNT(*) FILTER (WHERE vi.status = 'rejected')::bigint              AS rejected,
    -- WAS `vi.status IN ('accepted','verified')`, which matched no legal value
    -- and returned 0 forever. 'approved' is the accepted terminal state, pinned
    -- by verification_items_closed_approved_check.
    COUNT(*) FILTER (WHERE vi.status = 'approved')::bigint              AS accepted,
    MIN(vi.captured_at) FILTER (WHERE vi.status = 'pending')            AS oldest_pending_at,
    NULL::text                                                          AS owner_label,  -- TODO(source): owner is the shed position holder; operator_id is sensitive
    -- APPENDED, deliberately, and in this order. See the header: CREATE OR
    -- REPLACE VIEW can only add columns at the end, so `total` cannot sit where
    -- it reads best (before pending) without aborting on every existing
    -- database.
    COUNT(*) FILTER (WHERE vi.status <> 'withdrawn')::bigint            AS total,
    COUNT(*) FILTER (WHERE vi.status = 'withdrawn')::bigint             AS withdrawn,
    COUNT(*)::bigint                                                    AS total_including_withdrawn
FROM verification_items vi
LEFT JOIN locations sh ON sh.location_id = vi.shed_id
LEFT JOIN locations pk ON pk.location_id = vi.park_id
GROUP BY vi.tenant_id, COALESCE(vi.vertical, 'verification'), pk.name, sh.name;
-- +goose StatementEnd

-- ===========================================================================
-- Grants. CREATE OR REPLACE preserves the existing ACL, so these are strictly
-- belt-and-braces for an environment whose reader role was provisioned after
-- 000001 ran. Guarded on role existence for the reason 000080's header records
-- at length: an unguarded grant on a role that does not exist aborts the whole
-- migration, and local/pgtest databases have neither role.
-- ===========================================================================
-- +goose StatementBegin
DO $verification_queue_status_grants$
DECLARE
    r text;
BEGIN
    FOREACH r IN ARRAY ARRAY['mesha_ceo_readonly','mesha_cube_readonly'] LOOP
        IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = r) THEN
            EXECUTE format('GRANT SELECT ON ceo_ai.verification_queue_status TO %I', r);
        END IF;
    END LOOP;
END;
$verification_queue_status_grants$;
-- +goose StatementEnd

-- +goose Down
--
-- Restores the 000001 definition VERBATIM, dead `accepted` predicate and all --
-- a Down migration's job is to return the schema to the previous shape, not to
-- keep the bug fixed. DROP first because the down shape has FEWER columns, and
-- CREATE OR REPLACE cannot remove a column (`cannot drop columns from view`).
-- The reader grants are re-applied after the recreate for the same reason: DROP
-- discards the ACL.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '30s';

DROP VIEW IF EXISTS ceo_ai.verification_queue_status;

-- +goose StatementBegin
CREATE VIEW ceo_ai.verification_queue_status AS
SELECT
    vi.tenant_id                                                        AS tenant_id,
    COALESCE(vi.vertical, 'verification')                               AS area,
    pk.name                                                             AS park_label,
    sh.name                                                             AS shed_label,
    COUNT(*) FILTER (WHERE vi.status = 'pending')::bigint               AS pending,
    COUNT(*) FILTER (WHERE vi.status = 'rejected')::bigint              AS rejected,
    COUNT(*) FILTER (WHERE vi.status IN ('accepted','verified'))::bigint AS accepted,
    MIN(vi.captured_at) FILTER (WHERE vi.status = 'pending')            AS oldest_pending_at,
    NULL::text                                                          AS owner_label
FROM verification_items vi
LEFT JOIN locations sh ON sh.location_id = vi.shed_id
LEFT JOIN locations pk ON pk.location_id = vi.park_id
GROUP BY vi.tenant_id, COALESCE(vi.vertical, 'verification'), pk.name, sh.name;
-- +goose StatementEnd

-- +goose StatementBegin
DO $verification_queue_status_grants_down$
DECLARE
    r text;
BEGIN
    FOREACH r IN ARRAY ARRAY['mesha_ceo_readonly','mesha_cube_readonly'] LOOP
        IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = r) THEN
            EXECUTE format('GRANT SELECT ON ceo_ai.verification_queue_status TO %I', r);
        END IF;
    END LOOP;
END;
$verification_queue_status_grants_down$;
-- +goose StatementEnd
