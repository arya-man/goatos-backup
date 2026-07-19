-- +goose Up
-- P1: Baseline → Forward migration. Move counts_approval_requests table definition from 000001
-- to this forward migration so environments that already ran 000001 still get the table.
-- This migration includes P1 improvements: uniqueness constraint for shifting_event_id with
-- status='approved' to prevent multiple approved requests for the same shifting event.

-- Name: counts_approval_requests; Type: TABLE; Schema: public; Owner: -

-- counts_approval_requests: the Counts module's lifecycle approval workflow.
--
-- Maintainer decision (2026-07-19), superseding the previous apply-on-submit behaviour of
-- POST /app/counts/{birth,death,shifting}-events:
--
--   * BIRTH and DEATH are PENDING UNTIL APPROVED. Submitting one must NOT mutate `goats` and must
--     NOT emit goat.created / goat.exited. A kid's vaccination obligations are therefore generated
--     only on approval, and a death's open obligations are cancelled only on approval.
--   * SHIFTING already lands pending (shifting_events.authorization_state = 'pending'), so its
--     approval request LINKS to the existing row rather than duplicating the payload.
--   * Approver authority is per request_type: park_head decides shifting, ceo_internal decides
--     birth and death, operators decide nothing (permissions.CountsApproveShifting /
--     permissions.CountsApproveLifecycle).
--
-- Why the payload is stored here rather than applied eagerly: birth and death have no pending
-- representation anywhere else. `goats` is the APPLIED state, so a pending birth cannot be a goats
-- row without becoming visible to the herd register, the census, and the vaccination generator. The
-- validated request body is therefore held verbatim as JSONB and replayed through the SAME guarded
-- identity service command on approval (identity CreateAdminGoat / CriticalDeathExit) -- never a
-- bypass, so the dead+died critical-death guardrail is still enforced at apply time.
--
-- Grain: ONE row per submitted request. Not a projection, not a read model -- this is canonical
-- source state (the request and its decision), so it is rebuilt by nothing and owned here.

CREATE TABLE IF NOT EXISTS public.counts_approval_requests (
    approval_request_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    request_type text NOT NULL,

    -- Birth/death carry their validated submit body verbatim; shifting carries a small descriptor
    -- (the movement itself lives in shifting_events, referenced below) so the approvals list can be
    -- rendered without a join.
    payload jsonb DEFAULT '{}'::jsonb NOT NULL,

    -- Set for request_type='shifting' only: the already-written pending shifting_events row this
    -- request authorizes. Enforced by counts_approval_requests_shifting_link_check below.
    shifting_event_id uuid,

    -- Set for request_type='death' only: the animal the request would exit. Denormalised from
    -- payload so the pending list and the duplicate-open guard do not have to parse JSONB.
    subject_goat_id uuid,

    status text DEFAULT 'pending'::text NOT NULL,

    raised_by_user_id uuid NOT NULL,
    raised_at timestamp with time zone DEFAULT now() NOT NULL,

    decided_by_user_id uuid,
    decided_at timestamp with time zone,
    decision_reason text,

    -- What the approval actually produced, recorded in the SAME transaction as the status flip.
    -- birth/death -> ('goat', <goat_id>); shifting -> ('shifting_event', <shifting_event_id>).
    -- A non-null pair is the proof that an 'approved' row's effect committed; it is also what makes
    -- a second approve a no-op replay instead of a second application.
    applied_result_type text,
    applied_result_id uuid,

    -- Submit-time idempotency (the operator's write). Mirrors the shifting_events convention:
    -- the key identifies the request, the fingerprint identifies the payload, so an exact replay
    -- returns the original row and a same-key/different-payload replay is a conflict.
    idempotency_key text NOT NULL,
    request_fingerprint text NOT NULL,

    -- Decision-time idempotency (the approver's write). Approve/reject are themselves mutating
    -- writes and carry the full contract, so they need their own key/fingerprint pair rather than
    -- reusing the submit key.
    decision_idempotency_key text,
    decision_request_fingerprint text,

    row_version integer DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,

    CONSTRAINT counts_approval_requests_type_check
        CHECK ((request_type = ANY (ARRAY['birth'::text, 'death'::text, 'shifting'::text]))),
    CONSTRAINT counts_approval_requests_status_check
        CHECK ((status = ANY (ARRAY['pending'::text, 'approved'::text, 'rejected'::text]))),
    CONSTRAINT counts_approval_requests_payload_object_check
        CHECK ((jsonb_typeof(payload) = 'object'::text)),

    -- A shifting request MUST point at its shifting_events row; birth/death MUST NOT. This is what
    -- keeps "approve" able to dispatch on request_type without trusting the payload.
    CONSTRAINT counts_approval_requests_shifting_link_check
        CHECK (((request_type = 'shifting'::text) = (shifting_event_id IS NOT NULL))),
    -- Only a death names a subject animal up front (a birth CREATES its animal on approval).
    CONSTRAINT counts_approval_requests_subject_goat_check
        CHECK (((subject_goat_id IS NULL) OR (request_type = 'death'::text))),

    -- Reject requires a reason -- enforced in the DB, not only in Go (same rule as
    -- verification_items_reject_reason_check).
    CONSTRAINT counts_approval_requests_reject_reason_check
        CHECK (((status <> 'rejected'::text) OR ((decision_reason IS NOT NULL) AND (btrim(decision_reason) <> ''::text)))),
    -- A decided row must record WHO decided and WHEN; a pending row must record neither.
    CONSTRAINT counts_approval_requests_decision_shape_check
        CHECK ((((status = 'pending'::text) AND (decided_by_user_id IS NULL) AND (decided_at IS NULL))
             OR ((status <> 'pending'::text) AND (decided_by_user_id IS NOT NULL) AND (decided_at IS NOT NULL)))),
    -- An APPROVED row must carry the applied result. This is the schema-level half of the atomic
    -- transition rule: a row cannot read 'approved' without naming the effect that committed with it.
    CONSTRAINT counts_approval_requests_applied_result_check
        CHECK ((((status = 'approved'::text) AND (applied_result_type IS NOT NULL) AND (applied_result_id IS NOT NULL))
             OR ((status <> 'approved'::text) AND (applied_result_type IS NULL) AND (applied_result_id IS NULL)))),

    CONSTRAINT counts_approval_requests_idem_check CHECK ((btrim(idempotency_key) <> ''::text)),
    CONSTRAINT counts_approval_requests_fingerprint_check CHECK ((btrim(request_fingerprint) <> ''::text)),
    CONSTRAINT counts_approval_requests_row_version_check CHECK ((row_version >= 1)),

    PRIMARY KEY (approval_request_id),
    UNIQUE (tenant_id, approval_request_id)
);

-- P1: Completion fan-out uniqueness constraint. Only one approved request per shifting event.
-- Partial unique index: only applies to approved shifting requests.
CREATE UNIQUE INDEX IF NOT EXISTS counts_approval_requests_approved_shifting_unique
  ON public.counts_approval_requests (tenant_id, shifting_event_id)
  WHERE (status = 'approved'::text) AND (shifting_event_id IS NOT NULL);

-- P1: ListPending scope filtering index for park-scoped approvers.
-- Index on (tenant_id, status, request_type, raised_at DESC, approval_request_id DESC)
-- for pending-list filtering.
CREATE INDEX IF NOT EXISTS counts_approval_requests_pending_queue_idx
  ON public.counts_approval_requests (tenant_id, status, request_type, raised_at DESC, approval_request_id DESC)
  WHERE (status = 'pending'::text);

-- Status-queue index for decided history.
CREATE INDEX IF NOT EXISTS counts_approval_requests_status_queue_idx
  ON public.counts_approval_requests (tenant_id, status, request_type, raised_at DESC, approval_request_id DESC);

-- Idempotency key uniqueness index.
CREATE UNIQUE INDEX IF NOT EXISTS counts_approval_requests_idempotency_unique
  ON public.counts_approval_requests (tenant_id, idempotency_key);

-- Open-shifting uniqueness constraint: only one pending request per shifting event.
CREATE UNIQUE INDEX IF NOT EXISTS counts_approval_requests_open_shifting_unique
  ON public.counts_approval_requests (tenant_id, shifting_event_id)
  WHERE ((status = 'pending'::text) AND (shifting_event_id IS NOT NULL));

-- Foreign key: shifting_event_id references shifting_events.
ALTER TABLE ONLY public.counts_approval_requests
    ADD CONSTRAINT counts_approval_requests_shifting_event_fkey
    FOREIGN KEY (tenant_id, shifting_event_id) REFERENCES public.shifting_events(tenant_id, shifting_event_id) ON DELETE RESTRICT;

-- P0 (review): shifting completion/cancellation columns on the PRE-EXISTING shifting_events table.
-- shifting_execution.go writes event_status='applied' with applied_at/applied_by (completion) and
-- event_status='canceled' with canceled_at/canceled_by/cancel_reason (cancellation), each with its
-- own execution-time idempotency key + fingerprint. shifting_events pre-dates this PR on main and
-- never had these columns, so completion/cancellation would fail at runtime ("column does not
-- exist"). Add them here as a FORWARD migration (not a baseline edit) so fresh AND already-migrated
-- environments converge. Additive + nullable, so existing rows are unaffected.
ALTER TABLE public.shifting_events
    ADD COLUMN IF NOT EXISTS applied_at timestamp with time zone,
    ADD COLUMN IF NOT EXISTS applied_by uuid,
    ADD COLUMN IF NOT EXISTS canceled_at timestamp with time zone,
    ADD COLUMN IF NOT EXISTS canceled_by uuid,
    ADD COLUMN IF NOT EXISTS cancel_reason text,
    ADD COLUMN IF NOT EXISTS completion_idempotency_key text,
    ADD COLUMN IF NOT EXISTS completion_request_fingerprint text,
    ADD COLUMN IF NOT EXISTS cancel_idempotency_key text,
    ADD COLUMN IF NOT EXISTS cancel_request_fingerprint text;

-- Shape checks: an 'applied' event must carry applied_at (and only 'applied' may); a 'canceled'
-- event must carry canceled_at/by + a non-empty reason (and only 'canceled' may); and an event may
-- only be 'applied' once it was authorized. Guarded so a re-run of this migration is a no-op.
-- Added NOT VALID first (no full-table scan / no ACCESS EXCLUSIVE validation lock on the hot
-- shifting_events table), then VALIDATEd separately (SHARE UPDATE EXCLUSIVE, does not block reads/
-- writes). Existing rows trivially satisfy every check (the new columns are NULL and no row is
-- 'applied'/'canceled' yet), so validation is a formality here but the pattern stays lock-safe as
-- the table grows.
-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'shifting_events_applied_shape_check') THEN
        ALTER TABLE public.shifting_events
            ADD CONSTRAINT shifting_events_applied_shape_check
            CHECK ((((event_status = 'applied'::text) AND (applied_at IS NOT NULL))
                 OR ((event_status <> 'applied'::text) AND (applied_at IS NULL) AND (applied_by IS NULL)))) NOT VALID;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'shifting_events_canceled_shape_check') THEN
        ALTER TABLE public.shifting_events
            ADD CONSTRAINT shifting_events_canceled_shape_check
            CHECK ((((event_status = 'canceled'::text) AND (canceled_at IS NOT NULL) AND (canceled_by IS NOT NULL)
                     AND (cancel_reason IS NOT NULL) AND (btrim(cancel_reason) <> ''::text))
                 OR ((event_status <> 'canceled'::text) AND (canceled_at IS NULL) AND (canceled_by IS NULL)
                     AND (cancel_reason IS NULL)))) NOT VALID;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'shifting_events_applied_requires_authorization_check') THEN
        ALTER TABLE public.shifting_events
            ADD CONSTRAINT shifting_events_applied_requires_authorization_check
            CHECK (((event_status <> 'applied'::text) OR (authorization_state = 'authorized'::text))) NOT VALID;
    END IF;
END $$;
-- +goose StatementEnd

-- Validate the NOT VALID constraints: cheap operation on empty/unviolating rows.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'shifting_events_applied_shape_check' AND conisvalid = false) THEN
        ALTER TABLE public.shifting_events
            VALIDATE CONSTRAINT shifting_events_applied_shape_check;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'shifting_events_canceled_shape_check' AND conisvalid = false) THEN
        ALTER TABLE public.shifting_events
            VALIDATE CONSTRAINT shifting_events_canceled_shape_check;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'shifting_events_applied_requires_authorization_check' AND conisvalid = false) THEN
        ALTER TABLE public.shifting_events
            VALIDATE CONSTRAINT shifting_events_applied_requires_authorization_check;
    END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
-- Reverse of Up: drop the shifting_events completion/cancel columns (their CHECK constraints go
-- with them), then drop the counts_approval_requests table (its own indexes, constraints, and the
-- shifting_events FK go with it).
ALTER TABLE public.shifting_events
    DROP COLUMN IF EXISTS applied_at,
    DROP COLUMN IF EXISTS applied_by,
    DROP COLUMN IF EXISTS canceled_at,
    DROP COLUMN IF EXISTS canceled_by,
    DROP COLUMN IF EXISTS cancel_reason,
    DROP COLUMN IF EXISTS completion_idempotency_key,
    DROP COLUMN IF EXISTS completion_request_fingerprint,
    DROP COLUMN IF EXISTS cancel_idempotency_key,
    DROP COLUMN IF EXISTS cancel_request_fingerprint;
DROP TABLE IF EXISTS public.counts_approval_requests;
