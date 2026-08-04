-- +goose Up
-- +goose NO TRANSACTION
--
-- W-18: a verdict that is recorded but not yet APPLIED must be a real state.
--
-- Verdicts are applied ASYNCHRONOUSLY. The API's in-process bus deliberately
-- does not receive verification.verdict.* (backend/internal/bootstrap/api.go
-- "this API in-process bus does NOT receive the async verdict events"); the
-- module appliers run on the durable bus in cmd/outbox-relay and
-- cmd/domain-event-consumer. That design is correct and is NOT changed here.
--
-- What was wrong is the READ side. RecordVerdict flips
-- verification_items.status pending -> approved/rejected and the verifier's
-- queue (filtered on status='pending') drains to empty IMMEDIATELY, while the
-- producing module's own record is untouched until the durable event is
-- consumed. A human reads an empty queue as "done". If the relay is stopped,
-- lagging, or the event lands in the DLQ, the queue is empty and NOTHING was
-- applied -- and there is no signal anywhere that says so. The queue emptied on
-- SUBMISSION of a verdict rather than on APPLICATION of it, which is a lie.
--
-- This adds the missing acknowledgement so the in-between is nameable:
--
--   applier_ack_expected  the producing module declared, at enqueue time, that
--                         it runs an applier which acks. Opt-IN, default false:
--                         a module that does not ack yet must not have every one
--                         of its decided items read as "still being applied"
--                         forever. Weighing opts in
--                         (weighing/adapters/verificationbridge). Other
--                         producers opt in when they wire their own ack.
--   applied_at            when the producing module's applier confirmed it wrote
--                         the outcome onto its OWN record. Stamped only by
--                         MarkVerdictApplied, called by the applier AFTER its
--                         transaction commits.
--   applied_by_module     which module acked, so an ack can never be attributed
--                         to a module that did not send it.
--
-- There is NO second writer of verdict outcomes: the applier remains the single
-- writer of the producing module's state, and this ack is a downstream receipt
-- of that same write, not a parallel copy of it.
--
-- BACKFILL: none. Every existing row keeps applier_ack_expected=false, so no
-- historical item changes how it reads. Only items enqueued after this
-- migration by an opted-in producer can enter the awaiting-application state.
-- Inventing an applied_at for historical rows would assert an application that
-- was never observed.
--
-- LOCK SAFETY: NO TRANSACTION for the whole file so the CONCURRENTLY index
-- below can run at all. The three ADD COLUMNs take a brief ACCESS EXCLUSIVE
-- lock each but rewrite nothing -- a NOT NULL column with a constant default is
-- metadata-only since PG 11, and the two nullable columns always were. Each
-- autocommits on its own, so a failure part-way leaves the earlier statements
-- applied and the IF NOT EXISTS guards make a re-run a no-op.
ALTER TABLE public.verification_items
  ADD COLUMN IF NOT EXISTS applier_ack_expected boolean NOT NULL DEFAULT false;

ALTER TABLE public.verification_items
  ADD COLUMN IF NOT EXISTS applied_at timestamptz;

ALTER TABLE public.verification_items
  ADD COLUMN IF NOT EXISTS applied_by_module text;

-- An ack must name its module: a stamped applied_at with no module would be an
-- application nobody can be held to. Dropped first so a re-run is idempotent.
ALTER TABLE public.verification_items
  DROP CONSTRAINT IF EXISTS verification_items_applied_ack_complete_chk;

-- NOT VALID first, then VALIDATE: a plain ADD CONSTRAINT CHECK would hold
-- ACCESS EXCLUSIVE on verification_items for a full-table scan, and that table is
-- on the verifier queue path for every module. NOT VALID takes the lock only for
-- the catalog write; VALIDATE takes SHARE UPDATE EXCLUSIVE, which does not block
-- reads or writes. The scan is trivially satisfiable anyway -- both columns are
-- NULL in every pre-existing row -- but the LOCK is the reason, not the scan.
ALTER TABLE public.verification_items
  ADD CONSTRAINT verification_items_applied_ack_complete_chk
  CHECK ((applied_at IS NULL) = (applied_by_module IS NULL)) NOT VALID;

ALTER TABLE public.verification_items
  VALIDATE CONSTRAINT verification_items_applied_ack_complete_chk;

-- The awaiting-application read: "decided, ack expected, not yet acked, still
-- open". PARTIAL and narrow on purpose -- this index exists only to serve that
-- one bounded queue read and must not grow into a general verification_items
-- index. CONCURRENTLY so no ACCESS EXCLUSIVE lock is held on
-- verification_items, which is on the hot verifier queue path for every module.
CREATE INDEX CONCURRENTLY IF NOT EXISTS verification_items_awaiting_application_idx
ON public.verification_items (tenant_id, module, verified_at DESC, item_id)
WHERE applier_ack_expected
  AND applied_at IS NULL
  AND closed_at IS NULL
  AND status <> 'pending';

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS public.verification_items_awaiting_application_idx;

ALTER TABLE public.verification_items
  DROP CONSTRAINT IF EXISTS verification_items_applied_ack_complete_chk;

ALTER TABLE public.verification_items
  DROP COLUMN IF EXISTS applied_by_module;

ALTER TABLE public.verification_items
  DROP COLUMN IF EXISTS applied_at;

ALTER TABLE public.verification_items
  DROP COLUMN IF EXISTS applier_ack_expected;
