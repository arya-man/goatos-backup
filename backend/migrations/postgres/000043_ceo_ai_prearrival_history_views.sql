-- +goose Up
-- +goose StatementBegin
-- ===========================================================================
-- ceo_ai PRE-ARRIVAL VACCINATION HISTORY reporting — supplier-claim trust
-- quality as a leadership read surface.
--
-- WHY THIS EXISTS
-- Migration 000041 added public.vaccination_prearrival_history_entries: the
-- reviewed pre-arrival vaccination history channel for PROCURED animals. Every
-- supplier-attested claim that arrives on the procurement PC handoff is
-- machine-validated against the published protocol and persisted either as
-- review_status='accepted' (it becomes real history and suppresses a re-dose)
-- or review_status='rejected' with a mandatory rejection_reason.
--
-- That accepted/rejected split is a genuine LEADERSHIP signal, not internal
-- plumbing: it answers "how many procured animals arrived with vaccination
-- history we could trust", "what share of supplier vaccination claims did we
-- reject", and "why are we rejecting them" — i.e. supplier data quality and
-- avoided re-injection. It is therefore covered here rather than excluded.
--
-- SOURCE: public.vaccination_prearrival_history_entries only (plus no joins —
-- the table already carries every reporting dimension). Human vaccine labels
-- are NOT available on this table; vaccine_code is the raw protocol code, so it
-- is deliberately NOT exposed as a business label column here (the assistant
-- must not print raw config tokens). See the typed placeholder below.
--
-- GRAIN: one row per
--   (tenant_id, reviewed_date_ist, source_system, schedule_path,
--    review_status, rejection_reason).
-- rejection_reason is NULL for accepted rows (accepted rows carry no reason by
-- CHECK constraint), so the accepted and rejected buckets never collapse.
--
-- SCALE: bounded by procured animals × claims per animal (a procurement-volume
-- table, not a per-animal-per-day table), read tenant-scoped with a LIMIT at
-- the toolbox/SQL call site, and the tenant predicate pushes into the base scan
-- because tenant_id is the leading GROUP BY key.
--
-- IST business calendar: reviewed_date_ist buckets reviewed_at on the
-- Asia/Kolkata business day, never a UTC instant.
-- ===========================================================================

-- projection-review: membership=all vaccination_prearrival_history_entries rows for the tenant (accepted + rejected; nothing is filtered out, so the rejected sink stays visible); group_key=(tenant_id, reviewed_date_ist, source_system, schedule_path, review_status, rejection_reason) — every count below groups on exactly that key; join_cardinality=NO joins, single base table, so no fan-out is possible; pagination=procurement-volume table, bounded, read tenant-scoped with LIMIT at the Toolbox/SQL-fallback call site; scope=tenant + review status + schedule path + reason preserved on every row so "accepted vs rejected share" and "why were claims rejected" resolve without collapsing buckets. distinct_animals uses count(DISTINCT goat_id) inside the same group, so an animal with several claims in one bucket is counted once for the animal metric while claims stays per-claim.
CREATE OR REPLACE VIEW ceo_ai.vaccination_prearrival_history_review AS
SELECT
    e.tenant_id                                                   AS tenant_id,
    (e.reviewed_at AT TIME ZONE 'Asia/Kolkata')::date             AS reviewed_date_ist,
    e.source_system                                               AS source_system,
    e.schedule_path                                               AS schedule_path,
    e.review_status                                               AS review_status,
    e.rejection_reason                                            AS rejection_reason,
    count(*)::bigint                                              AS claims,
    count(DISTINCT e.goat_id)::bigint                             AS distinct_animals,
    min((e.administered_at AT TIME ZONE 'Asia/Kolkata')::date)    AS earliest_administered_date,
    max((e.administered_at AT TIME ZONE 'Asia/Kolkata')::date)    AS latest_administered_date,
    NULL::text -- TODO(no source yet): vaccine_label — this table stores the raw
               -- protocol vaccine_code only; the human label lives on the
               -- published protocol rule and is not joinable at this grain
               -- without fanning the review buckets out per vaccine. Leadership
               -- answers therefore stay at the trust/quality grain, and raw
               -- config tokens are never surfaced.
                                                                  AS vaccine_label
FROM public.vaccination_prearrival_history_entries e
GROUP BY 1, 2, 3, 4, 5, 6;
-- +goose StatementEnd

-- ===========================================================================
-- GUARDED GRANTS — idempotent SELECT grant for the read-only reporting roles
-- if they exist (mirrors migrations 000023 / 000027).
-- ===========================================================================
-- +goose StatementBegin
DO $grants$
DECLARE
    r text;
BEGIN
    FOREACH r IN ARRAY ARRAY['mesha_ceo_readonly','mesha_cube_readonly'] LOOP
        IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = r) THEN
            EXECUTE format('GRANT SELECT ON ceo_ai.vaccination_prearrival_history_review TO %I', r);
        END IF;
    END LOOP;
END;
$grants$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP VIEW IF EXISTS ceo_ai.vaccination_prearrival_history_review;
-- +goose StatementEnd
