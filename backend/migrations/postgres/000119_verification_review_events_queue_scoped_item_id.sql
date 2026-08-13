-- seed-migration-guard:ignore owner=ravi issue=maintainer-decision-2026-08-06 reason=append-only-telemetry-accrues-at-runtime-no-seed-companion expiry=2026-11-30
-- no-mismatch-review-queue:ignore: owner=ravi issue=maintainer-decision-2026-08-06 scope=verifier-watch-telemetry-not-a-reconciliation-queue expiry=2026-11-30
-- +goose Up
--
-- Real bug found driving the browser (2026-08-06): the frontend's queue-level `queue_opened`
-- telemetry event fires BEFORE any item exists (it fires on landing on the queue screen), so it
-- has no real item_id. The client sent the literal string "queue" as a placeholder. Migration
-- 000115 declared item_id `uuid NOT NULL`, so every batch containing that event failed to
-- PARSE as JSON-with-a-UUID-field and the whole batch died -- taking every item-scoped event
-- riding in the same batch down with it. See backend/internal/verification/app/review_events.go
-- (fixed alongside this migration) for the app-layer half of this fix.
--
-- FIX: item_id becomes NULLABLE, but the nullability is NOT unconstrained -- a CHECK enforces
-- the exact rule the app layer also enforces (belt-and-suspenders, matching this repo's
-- idempotency/validation convention of never trusting the app layer alone against direct SQL
-- writes): item_id IS NULL if and only if event_type = 'queue_opened'. Every other registered
-- event type (item_opened, video_play/pause/seek_attempt/ended, proof_switched,
-- fullscreen_toggled, verdict_recorded) is intrinsically about ONE item and stays NOT NULL.
--
-- QUEUE-SCOPED EVENT SCOPE KEY (documented per the fix request): a queue_opened row's identity
-- is (tenant_id, actor_id, session_id, occurred_at) -- there is no item yet. Attribution for a
-- CEO-facing "queue opened -> item opened -> verdict recorded" FUNNEL comes from the event's
-- OWN payload, not from item_id: the client is required (app-layer validation) to carry
-- `category` in payload for a queue_opened event, and MAY carry `park_id`/`shed_id` when the
-- queue view was scoped to one. This keeps queue_opened joinable to the SAME (category, park)
-- dimensions ceo_ai.verifier_review_integrity groups its item-scoped rows by, without needing a
-- fake item_id to hang it on.
--
-- WHY THIS DOES NOT TOUCH ceo_ai.verifier_review_integrity (migration 000118): every CTE in that
-- view (event_stats, item_facts) is built by joining verification_review_events to
-- verification_items ON jf.item_id = vi.item_id, which is a UUID equality -- a NULL item_id row
-- can never satisfy `NULL = vi.item_id` (SQL NULL comparison semantics), so queue_opened rows
-- are automatically excluded from the per-item aggregate and cannot inflate per-item facts. No
-- view migration needed; this comment IS the proof, and
-- backend/internal/ceoai/reporting/verifier_review_integrity_test.go gets a new adversarial case
-- asserting it.
--
-- LOCK SAFETY: DROP NOT NULL is a fast catalog-only change (no table rewrite). The CHECK
-- constraint is added NOT VALID first (no full-table scan under the lock) and validated in a
-- separate statement that only takes a SHARE UPDATE EXCLUSIVE lock (does not block concurrent
-- reads/writes) -- the standard hot-table-safe two-step from
-- .agents/skills/db-migration-safety/SKILL.md, applied here even though
-- verification_review_events is not (yet) in validate-hot-index-migrations.sh's hot_tables set,
-- because it is an append-only ingest table expected to grow fast.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '30s';

-- seed-migration-guard:ignore owner=ravi issue=maintainer-decision-2026-08-06 reason=append-only-telemetry-accrues-at-runtime-no-seed-companion expiry=2026-11-30
-- no-mismatch-review-queue:ignore: owner=ravi issue=maintainer-decision-2026-08-06 scope=verifier-watch-telemetry-not-a-reconciliation-queue expiry=2026-11-30
ALTER TABLE public.verification_review_events
    ALTER COLUMN item_id DROP NOT NULL;

-- seed-migration-guard:ignore owner=ravi issue=maintainer-decision-2026-08-06 reason=append-only-telemetry-accrues-at-runtime-no-seed-companion expiry=2026-11-30
-- no-mismatch-review-queue:ignore: owner=ravi issue=maintainer-decision-2026-08-06 scope=verifier-watch-telemetry-not-a-reconciliation-queue expiry=2026-11-30
ALTER TABLE public.verification_review_events
    ADD CONSTRAINT verification_review_events_item_id_scope_check
    CHECK (
        (event_type = 'queue_opened' AND item_id IS NULL)
        OR (event_type <> 'queue_opened' AND item_id IS NOT NULL)
    ) NOT VALID;

-- seed-migration-guard:ignore owner=ravi issue=maintainer-decision-2026-08-06 reason=append-only-telemetry-accrues-at-runtime-no-seed-companion expiry=2026-11-30
-- no-mismatch-review-queue:ignore: owner=ravi issue=maintainer-decision-2026-08-06 scope=verifier-watch-telemetry-not-a-reconciliation-queue expiry=2026-11-30
ALTER TABLE public.verification_review_events
    VALIDATE CONSTRAINT verification_review_events_item_id_scope_check;

-- +goose Down
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '30s';

-- seed-migration-guard:ignore owner=ravi issue=maintainer-decision-2026-08-06 reason=append-only-telemetry-accrues-at-runtime-no-seed-companion expiry=2026-11-30
-- no-mismatch-review-queue:ignore: owner=ravi issue=maintainer-decision-2026-08-06 scope=verifier-watch-telemetry-not-a-reconciliation-queue expiry=2026-11-30
ALTER TABLE public.verification_review_events
    DROP CONSTRAINT IF EXISTS verification_review_events_item_id_scope_check;

-- Restoring NOT NULL requires no existing NULL item_id rows; this Down is only ever exercised on
-- a rollback of THIS migration in the same deploy, before any queue_opened row has been written
-- in production, matching this repo's other reversible-only-immediately-after Down conventions.
-- seed-migration-guard:ignore owner=ravi issue=maintainer-decision-2026-08-06 reason=append-only-telemetry-accrues-at-runtime-no-seed-companion expiry=2026-11-30
-- no-mismatch-review-queue:ignore: owner=ravi issue=maintainer-decision-2026-08-06 scope=verifier-watch-telemetry-not-a-reconciliation-queue expiry=2026-11-30
ALTER TABLE public.verification_review_events
    ALTER COLUMN item_id SET NOT NULL;
