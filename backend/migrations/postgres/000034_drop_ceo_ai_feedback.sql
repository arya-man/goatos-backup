-- +goose Up
DROP TABLE IF EXISTS ceo_ai_feedback;

-- +goose Down
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
