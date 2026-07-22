-- +goose Up
-- ===========================================================================
-- ceo_ai_conversations idempotent-create key.
--
-- 000021 shipped ceo_ai_conversations without an idempotency key. The assistant
-- persistence layer's create path is required to be replay-safe (a retried
-- POST /conversations must return the original thread, never fork a second one),
-- which needs a stable per-(tenant, actor) key plus a partial unique index.
--
-- Additive + lock-safe: ADD COLUMN of a nullable text is a metadata-only change
-- (no table rewrite), and the table is new so the unique index build is trivial.
-- NULLs stay distinct under the partial index, so keyless creates never collide;
-- only a keyed (tenant, actor, key) create is deduplicated on replay.
-- ===========================================================================

ALTER TABLE ceo_ai_conversations
    ADD COLUMN IF NOT EXISTS idempotency_key text;

CREATE UNIQUE INDEX IF NOT EXISTS ceo_ai_conversations_idem_uq
    ON ceo_ai_conversations (tenant_id, actor_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS ceo_ai_conversations_idem_uq;
ALTER TABLE ceo_ai_conversations DROP COLUMN IF EXISTS idempotency_key;
