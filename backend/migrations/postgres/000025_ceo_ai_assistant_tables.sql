-- +goose Up
-- ===========================================================================
-- ceo_ai assistant app-owned tables (public schema) — the durable state the
-- Mesha leadership assistant backend writes: audit/trace, conversation threads,
-- messages, feedback, response cache, and a persisted rate-limit window.
--
-- These are APP-OWNED (written by the Go backend service, NOT by the read-only
-- roles). mesha_ceo_readonly / mesha_cube_readonly have NO access to public
-- (REVOKE ALL ON SCHEMA public, per 000020 + setup script) — the assistant
-- reads business data only through ceo_ai.* views and writes its own state here
-- with the normal app credential.
--
-- Design honors AGENTS.md: keyset-friendly indexes (no OFFSET), tenant-scoped,
-- idempotency/soft-delete + retention where the plan requires it, jsonb for
-- step traces / tool calls / citations (INTERNAL only — never returned in the
-- leadership chat answer, per the internal-tracking rule).
-- ===========================================================================

-- ---------------------------------------------------------------------------
-- ceo_ai_assistant_audit — one tamper-evident row per assistant request.
-- Internal/admin-only. Holds the step trace + review verdict for the debug
-- surface; NEVER surfaced in the user chat answer.
-- ---------------------------------------------------------------------------
CREATE TABLE ceo_ai_assistant_audit (
    audit_id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          uuid NOT NULL REFERENCES tenants (tenant_id),
    actor_id           uuid,                         -- authenticated leadership user (sensitive)
    actor_role         text,                         -- e.g. ceo_internal
    conversation_id    uuid,                         -- FK added after ceo_ai_conversations exists
    request_id         text NOT NULL,                -- correlation id returned to the client
    question_hash      text NOT NULL,                -- sha256 of normalized question (no raw PII in indexes)
    question_redacted  text,                         -- redacted question text for debug
    route_tier         text,                         -- cube | mesha_api | mcp_toolbox | sql_fallback
    tool_called        text,                         -- resolved tool / metric / view name
    generated_sql_hash text,                         -- hash of validated fallback SQL (when used)
    source_views       text[] NOT NULL DEFAULT '{}', -- ceo_ai.* views touched
    row_count          integer,
    latency_ms         integer,
    status             text NOT NULL DEFAULT 'ok',   -- ok | rejected | error | over_budget | degraded
    rejection_reason   text,
    step_trace         jsonb NOT NULL DEFAULT '[]'::jsonb, -- INTERNAL plan/route/exec timeline
    review_verdict     text,                         -- optional MESHA_AI_REVIEW verdict (pass/flag)
    model_version      text,
    prompt_version     text,
    created_at         timestamptz NOT NULL DEFAULT now()
);

-- keyset browse of the internal audit log (newest-first), tenant-scoped.
CREATE INDEX ceo_ai_assistant_audit_tenant_created_idx
    ON ceo_ai_assistant_audit (tenant_id, created_at DESC, audit_id DESC);
CREATE INDEX ceo_ai_assistant_audit_request_idx
    ON ceo_ai_assistant_audit (tenant_id, request_id);
CREATE INDEX ceo_ai_assistant_audit_conversation_idx
    ON ceo_ai_assistant_audit (conversation_id, created_at DESC)
    WHERE conversation_id IS NOT NULL;

-- ---------------------------------------------------------------------------
-- ceo_ai_conversations — durable threads (create/list/resume/archive + purge).
-- ---------------------------------------------------------------------------
CREATE TABLE ceo_ai_conversations (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          uuid NOT NULL REFERENCES tenants (tenant_id),
    actor_id           uuid NOT NULL,                -- owning leadership user
    title              text,                         -- derived from first question; renamable
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    archived_at        timestamptz,                  -- soft-archive (user hide)
    retention_expires_at timestamptz                 -- hard-purge boundary (retention policy)
);

-- keyset list of a user's active threads, newest-updated first.
CREATE INDEX ceo_ai_conversations_owner_idx
    ON ceo_ai_conversations (tenant_id, actor_id, updated_at DESC, id DESC)
    WHERE archived_at IS NULL;
-- purge scan by retention boundary.
CREATE INDEX ceo_ai_conversations_retention_idx
    ON ceo_ai_conversations (retention_expires_at)
    WHERE retention_expires_at IS NOT NULL;

-- backfill the audit FK now that the table exists.
ALTER TABLE ceo_ai_assistant_audit
    ADD CONSTRAINT ceo_ai_assistant_audit_conversation_fk
    FOREIGN KEY (conversation_id) REFERENCES ceo_ai_conversations (id) ON DELETE SET NULL;

-- ---------------------------------------------------------------------------
-- ceo_ai_messages — the turn-by-turn message history for a thread.
-- tool_calls + citations are jsonb: citations MAY be shown to the user
-- (structured provenance chips), tool_calls are INTERNAL only.
-- ---------------------------------------------------------------------------
CREATE TABLE ceo_ai_messages (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id    uuid NOT NULL REFERENCES ceo_ai_conversations (id) ON DELETE CASCADE,
    tenant_id          uuid NOT NULL REFERENCES tenants (tenant_id),
    role               text NOT NULL,                -- user | assistant | system
    content            text NOT NULL,
    tool_calls         jsonb NOT NULL DEFAULT '[]'::jsonb, -- INTERNAL routing/exec detail
    citations          jsonb NOT NULL DEFAULT '[]'::jsonb, -- user-visible provenance
    source             text,                         -- resolved source surface for assistant turns
    mode               text,                         -- governed | operational | exploratory
    request_id         text,                         -- links to ceo_ai_assistant_audit.request_id
    created_at         timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ceo_ai_messages_role_chk CHECK (role IN ('user','assistant','system'))
);

-- ordered keyset read of a thread's messages (required index from the spec).
CREATE INDEX ceo_ai_messages_conversation_created_idx
    ON ceo_ai_messages (conversation_id, created_at, id);

-- ---------------------------------------------------------------------------
-- ceo_ai_feedback — thumbs + reason on an assistant message, for eval mining.
-- ---------------------------------------------------------------------------
CREATE TABLE ceo_ai_feedback (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    message_id         uuid NOT NULL REFERENCES ceo_ai_messages (id) ON DELETE CASCADE,
    tenant_id          uuid NOT NULL REFERENCES tenants (tenant_id),
    actor_id           uuid NOT NULL,
    rating             smallint NOT NULL,            -- +1 thumbs up, -1 thumbs down
    reason             text,
    created_at         timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ceo_ai_feedback_rating_chk CHECK (rating IN (-1, 1)),
    -- one feedback per (message, actor); a re-vote updates it.
    CONSTRAINT ceo_ai_feedback_message_actor_unique UNIQUE (message_id, actor_id)
);

CREATE INDEX ceo_ai_feedback_tenant_created_idx
    ON ceo_ai_feedback (tenant_id, created_at DESC, id DESC);

-- ---------------------------------------------------------------------------
-- ceo_ai_response_cache — semantic/response cache keyed by tenant + question
-- hash + as_of bucket. Never crosses tenants.
-- ---------------------------------------------------------------------------
CREATE TABLE ceo_ai_response_cache (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          uuid NOT NULL REFERENCES tenants (tenant_id),
    question_hash      text NOT NULL,                -- sha256(normalized question + as_of bucket)
    answer             jsonb NOT NULL,               -- cached composed answer + metadata
    source_views       text[] NOT NULL DEFAULT '{}',
    created_at         timestamptz NOT NULL DEFAULT now(),
    expires_at         timestamptz NOT NULL,         -- TTL / as_of-day rollover invalidation
    CONSTRAINT ceo_ai_response_cache_tenant_hash_unique UNIQUE (tenant_id, question_hash)
);

-- sweep expired cache rows.
CREATE INDEX ceo_ai_response_cache_expiry_idx
    ON ceo_ai_response_cache (expires_at);

-- ---------------------------------------------------------------------------
-- ceo_ai_rate_limit — persisted per-(tenant, actor) fixed-window request
-- counter so caps survive a process restart (the in-Go token bucket resets on
-- restart). Backend increments count within the current window_start bucket.
-- ---------------------------------------------------------------------------
CREATE TABLE ceo_ai_rate_limit (
    tenant_id          uuid NOT NULL REFERENCES tenants (tenant_id),
    actor_id           uuid NOT NULL,
    window_start       timestamptz NOT NULL,         -- truncated window bucket start
    count              integer NOT NULL DEFAULT 0,
    updated_at         timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, actor_id, window_start)
);

-- prune old windows.
CREATE INDEX ceo_ai_rate_limit_window_idx
    ON ceo_ai_rate_limit (window_start);

-- +goose Down
DROP TABLE IF EXISTS ceo_ai_rate_limit;
DROP TABLE IF EXISTS ceo_ai_response_cache;
DROP TABLE IF EXISTS ceo_ai_feedback;
DROP TABLE IF EXISTS ceo_ai_messages;
-- drop the audit FK before the conversations table it references
ALTER TABLE IF EXISTS ceo_ai_assistant_audit
    DROP CONSTRAINT IF EXISTS ceo_ai_assistant_audit_conversation_fk;
DROP TABLE IF EXISTS ceo_ai_conversations;
DROP TABLE IF EXISTS ceo_ai_assistant_audit;
