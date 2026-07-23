-- seed-migration-guard:ignore owner=ravi issue=CASCADE-DUR-01 reason=operational-outbox-event-index-no-seed-data-coupling expiry=2026-12-31
-- Cascade outbox idempotency indexes (one per event type).
--
-- The vaccination auto-cascade producers enqueue their trigger events into
-- outbox_messages with `ON CONFLICT (tenant_id, idempotency_key) WHERE
-- event_type = '<type>' DO NOTHING` for at-least-once-safe re-enqueue:
--   - vaccination.capacity.changed   (vaccination-execution: operator/cap config change)
--   - vaccination.roster.changed     (workforce/execution: default-operator / roster change)
--   - vaccination.leave.changed      (workforce: shed-scoped leave apply)
--
-- The only pre-existing partial unique index on (tenant_id, idempotency_key)
-- (outbox_messages_verification_idempotency_idx_v2, migration 000004) covers
-- ONLY the verification.* event types. With no arbiter index for the cascade
-- event types, every ON CONFLICT insert above fails with 42P10 "no unique or
-- exclusion constraint matching the ON CONFLICT specification" — the whole
-- business write (leave apply, config upsert) rolls back.
--
-- PostgreSQL can only INFER a PARTIAL unique index as an ON CONFLICT arbiter
-- when the ON CONFLICT WHERE predicate matches the index predicate. The existing
-- producers for vaccination.completed / obligation.missed / config.changed each
-- pair a single-event-type partial index with a single-event-type
-- `WHERE event_type = '<type>'` ON CONFLICT clause. Follow that proven pattern:
-- one single-predicate partial unique index per cascade event type (a
-- multi-type ANY(...) index would rely on PG's fragile predicate-implication
-- prover for ScalarArrayOpExpr).

-- +goose Up
-- +goose NO TRANSACTION
-- CONCURRENTLY cannot run inside a transaction; hot table => NO TRANSACTION.

-- Dedup guard: a populated database could already hold duplicate
-- (tenant_id, idempotency_key) rows among the cascade event types (rows written
-- before this index existed). A CREATE UNIQUE INDEX CONCURRENTLY would fail on
-- such rows. Keep the earliest row per (tenant_id, event_type, idempotency_key)
-- among the covered event types and delete the rest. One-way, non-reversible,
-- scoped to the covered event types only.
-- +goose StatementBegin
DO $$
BEGIN
  DELETE FROM public.outbox_messages dupe
  USING (
    SELECT outbox_id
    FROM (
      SELECT outbox_id,
             row_number() OVER (
               PARTITION BY tenant_id, event_type, idempotency_key
               ORDER BY created_at ASC, outbox_id ASC
             ) AS rn
      FROM public.outbox_messages
      WHERE idempotency_key IS NOT NULL
        AND event_type = ANY (ARRAY[
          'vaccination.capacity.changed'::text,
          'vaccination.roster.changed'::text,
          'vaccination.leave.changed'::text
        ])
    ) ranked
    WHERE ranked.rn > 1
  ) losers
  WHERE dupe.outbox_id = losers.outbox_id;
END
$$;
-- +goose StatementEnd

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS outbox_messages_capacity_changed_idempotency_idx
  ON public.outbox_messages USING btree (tenant_id, idempotency_key)
  WHERE (event_type = 'vaccination.capacity.changed');

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS outbox_messages_roster_changed_idempotency_idx
  ON public.outbox_messages USING btree (tenant_id, idempotency_key)
  WHERE (event_type = 'vaccination.roster.changed');

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS outbox_messages_leave_changed_idempotency_idx
  ON public.outbox_messages USING btree (tenant_id, idempotency_key)
  WHERE (event_type = 'vaccination.leave.changed');

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS public.outbox_messages_capacity_changed_idempotency_idx;
DROP INDEX CONCURRENTLY IF EXISTS public.outbox_messages_roster_changed_idempotency_idx;
DROP INDEX CONCURRENTLY IF EXISTS public.outbox_messages_leave_changed_idempotency_idx;
