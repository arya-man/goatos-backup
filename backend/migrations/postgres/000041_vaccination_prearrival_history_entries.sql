-- BUG-017: reviewed pre-arrival accepted-history channel for procured animals.
--
-- Procurement captures a supplier-attested vaccination card on the PC handoff
-- (`procurement_pc_handoffs.trusted_vaccination_history`) and forwards it in the
-- `goat.created` payload. Until this migration nothing consumed that key, so a
-- procured animal with a genuine prior ET+TT course was scheduled from scratch
-- and re-injected.
--
-- Why a NEW table instead of `procurement_hf_vaccination_evidence`: that table is
-- the PROOF-BACKED holding-farm channel. Its trust gate requires a completed
-- `proof_artifacts` row, a `procurement_load_goats` membership with a 28-35 day
-- warm-up window, and `administered_at` inside that window. A pre-arrival supplier
-- claim has no load membership, no proof artifact, and a date BEFORE the warm-up
-- started, so it can never satisfy that gate. Forcing it through would mean
-- writing `review_status='trusted'` with no proof artifact -- exactly the "supplier
-- claim silently becomes accepted history" failure this must avoid. The two
-- channels therefore stay physically separate with separate review vocabularies.
--
-- Review gate on this channel:
--   * every claim is machine-validated against the PUBLISHED protocol rules and
--     against the animal's INDEPENDENTLY classified schedule path (DOB / entry
--     date / management stage) -- the claim's own dose code is never the evidence
--     for its own path;
--   * an accepted row must name the resolved protocol version + rule and the human
--     who accepted the intake (`reviewed_by`, nullable only for system-accepted
--     intake where procurement recorded no actor);
--   * a claim that fails validation is persisted with `review_status='rejected'`
--     and a mandatory `rejection_reason` so it is surfaced, never silently dropped,
--     and it never suppresses due work.
--
-- Only `review_status='accepted'` rows feed
-- `Repository.RecentVaccineAdministrationsForGoats`, i.e. the vaccination
-- generation engine's history input.
--
-- Lock safety: this migration only CREATEs a brand-new empty table plus its
-- indexes. No existing table is read, rewritten, or locked, so there is no
-- CONCURRENTLY / NOT VALID phase to split out and the whole thing runs in one
-- short transaction.

-- +goose Up
SET lock_timeout = '5s';

CREATE TABLE IF NOT EXISTS public.vaccination_prearrival_history_entries (
    entry_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    goat_id uuid NOT NULL,
    source_system text NOT NULL DEFAULT 'procurement_pc_handoff',
    source_event_id text NOT NULL,
    protocol_version_id uuid,
    rule_id uuid,
    vaccine_code text NOT NULL,
    dose_code text NOT NULL,
    sequence integer NOT NULL DEFAULT 0,
    administered_at timestamptz NOT NULL,
    schedule_path text NOT NULL,
    review_status text NOT NULL,
    rejection_reason text,
    reviewed_by uuid,
    reviewed_at timestamptz NOT NULL,
    claim jsonb NOT NULL,
    idempotency_key text NOT NULL,
    request_fingerprint text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT vaccination_prearrival_history_status_check
        CHECK (review_status IN ('accepted', 'rejected')),
    CONSTRAINT vaccination_prearrival_history_path_check
        CHECK (schedule_path IN ('kid', 'adult_procurement')),
    -- A rejected entry MUST carry a reason: a durable sink with no reason is the
    -- silent-drop defect one column over.
    CONSTRAINT vaccination_prearrival_history_rejected_reason_check
        CHECK (review_status <> 'rejected' OR nullif(btrim(rejection_reason), '') IS NOT NULL),
    -- An accepted entry MUST resolve to a published protocol rule; unresolved
    -- history can never be matched to a dose and would silently suppress nothing
    -- or, worse, everything.
    CONSTRAINT vaccination_prearrival_history_accepted_rule_check
        CHECK (review_status <> 'accepted'
               OR (protocol_version_id IS NOT NULL
                   AND rule_id IS NOT NULL
                   AND nullif(btrim(vaccine_code), '') IS NOT NULL
                   AND nullif(btrim(dose_code), '') IS NOT NULL)),
    CONSTRAINT vaccination_prearrival_history_claim_object_check
        CHECK (jsonb_typeof(claim) = 'object')
);

-- Write-path idempotency: a stable per-claim key plus the semantic payload
-- fingerprint. Exact replay conflicts on this index and returns the original row;
-- a same-key/different-payload replay is rejected by the writer after comparing
-- request_fingerprint.
CREATE UNIQUE INDEX IF NOT EXISTS vaccination_prearrival_history_idempotency_idx
    ON public.vaccination_prearrival_history_entries (tenant_id, idempotency_key);

-- Generation read path: accepted anchors for a page of goats, newest first.
CREATE INDEX IF NOT EXISTS vaccination_prearrival_history_accepted_goat_idx
    ON public.vaccination_prearrival_history_entries (tenant_id, goat_id, administered_at DESC)
    WHERE review_status = 'accepted';

-- Operator surfacing of the rejected sink (never silently dropped).
CREATE INDEX IF NOT EXISTS vaccination_prearrival_history_rejected_idx
    ON public.vaccination_prearrival_history_entries (tenant_id, reviewed_at DESC)
    WHERE review_status = 'rejected';

-- +goose Down
DROP TABLE IF EXISTS public.vaccination_prearrival_history_entries;
