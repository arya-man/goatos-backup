-- no-mismatch-review-queue:ignore: owner=ravi issue=maintainer-decision-2026-08-06 scope=verifier-watch-telemetry-not-a-reconciliation-queue expiry=2026-11-30
-- seed-migration-guard:ignore owner=ravi issue=maintainer-decision-2026-08-06 reason=append-only-telemetry-accrues-at-runtime-no-seed-companion expiry=2026-11-30
--
-- Why both directives: this table is NOT the banned runtime review/reconcile queue (VACC-REV-10).
-- It stores the verifier's own video-watch telemetry (play/pause/seek/verdict), an append-only
-- audit stream used to prove a proof video was actually watched. It creates no work items, holds no
-- mismatched/dirty ingested rows, and nothing reads it to decide operator work. It also has no seed
-- companion by design: rows accrue only from live verifier activity, so seeding them would
-- fabricate evidence that somebody watched a video.
-- +goose Up
-- Verifier video-review analytics (CEO-visible integrity signal): can we prove a verifier
-- actually WATCHED a proof video rather than rubber-stamping it. Scope is the verifier role
-- only -- this is not a generic activity log for every role.
--
-- Append-only event ingest. The browser flushes small batches of these events periodically and
-- on unload (queue_opened, item_opened, video_play/pause/seek_attempt/ended, proof_switched,
-- fullscreen_toggled, verdict_recorded). Idempotency is per-event: client_event_id is a
-- client-minted UUID, unique per (tenant_id, client_event_id), so a replayed batch (retry after a
-- network blip) inserts nothing new -- see idempotency contract in AGENTS.md.
--
-- Derived per-(item,actor) facts (watch_ms, distinct-covered watch_fraction, play/pause/seek
-- counts, time-to-verdict, watched_full) are NOT stored here -- computed in
-- internal/verification/adapters/postgres/review_facts.go on read, backed by the indexes below.
-- They are cheap per-item aggregates (bounded by event count per item, not a whole-table scan),
-- so read-time computation is not the scale-anti-pattern this repo bans for CEO-wide aggregates;
-- the CEO-wide ceo_ai view in migration 000112 IS a stored/materialized aggregate.
CREATE TABLE public.verification_review_events (
    event_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    item_id uuid NOT NULL,
    proof_id uuid,
    actor_id uuid NOT NULL,
    session_id text NOT NULL,
    event_type text NOT NULL,
    occurred_at timestamp with time zone NOT NULL,
    payload jsonb DEFAULT '{}'::jsonb NOT NULL,
    client_event_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT verification_review_events_pkey PRIMARY KEY (event_id),
    CONSTRAINT verification_review_events_session_check CHECK (btrim(session_id) <> ''),
    CONSTRAINT verification_review_events_payload_object_check CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT verification_review_events_type_check CHECK (event_type = ANY (ARRAY[
        'queue_opened', 'item_opened', 'video_play', 'video_pause', 'video_seek_attempt',
        'video_ended', 'proof_switched', 'fullscreen_toggled', 'verdict_recorded'
    ])),
    CONSTRAINT verification_review_events_item_fkey FOREIGN KEY (tenant_id, item_id)
        REFERENCES public.verification_items (tenant_id, item_id) DEFERRABLE INITIALLY IMMEDIATE,
    CONSTRAINT verification_review_events_proof_fkey FOREIGN KEY (proof_id)
        REFERENCES public.proof_artifacts (proof_id) DEFERRABLE INITIALLY IMMEDIATE
);

-- Attach the composite unique CONSTRAINT using the index built CONCURRENTLY in migration
-- 000111 (USING INDEX skips the redundant index build, so this only takes the brief catalog
-- lock needed to record the constraint -- no CREATE-INDEX-strength table lock on the hot table).
ALTER TABLE public.verification_items
    ADD CONSTRAINT verification_items_tenant_item_unique UNIQUE
    USING INDEX verification_items_tenant_item_unique_idx;

-- Idempotent-write contract: a replayed batch with the same client-minted ids inserts nothing new.
CREATE UNIQUE INDEX verification_review_events_tenant_client_event_unique_idx
    ON public.verification_review_events USING btree (tenant_id, client_event_id);

-- Read path 1: per-(item,actor) derived facts -- fetch every event for one item ordered by time.
CREATE INDEX verification_review_events_item_actor_time_idx
    ON public.verification_review_events USING btree (tenant_id, item_id, actor_id, occurred_at);

-- Read path 2: per-actor-per-day rollups feeding the CEO aggregate (migration 000112).
CREATE INDEX verification_review_events_actor_time_idx
    ON public.verification_review_events USING btree (tenant_id, actor_id, occurred_at);

-- +goose Down
DROP INDEX IF EXISTS public.verification_review_events_actor_time_idx;
DROP INDEX IF EXISTS public.verification_review_events_item_actor_time_idx;
DROP INDEX IF EXISTS public.verification_review_events_tenant_client_event_unique_idx;
ALTER TABLE public.verification_items DROP CONSTRAINT IF EXISTS verification_items_tenant_item_unique;
DROP TABLE IF EXISTS public.verification_review_events;
