-- +goose Up
-- +goose NO TRANSACTION
--
-- FK PROBE INDEX FOR DELETE /app/proofs/{proof_id}.
--
-- verification_review_events.proof_id REFERENCES proof_artifacts(proof_id), but no index leads
-- with proof_id. Deleting an unattached proof fires the RI check
-- `SELECT 1 FROM ONLY verification_review_events WHERE proof_id = $1 FOR KEY SHARE`, which
-- seq-scanned the whole table (115k rows / 33 MB on STG): 25 ms hot, and the ~250 ms median
-- the DELETE statement shows in STG traces when those pages are cold. A partial btree on the
-- non-null references turns it into a single index probe. Only the access path changes; the RI
-- outcome and every row written are identical.
--
-- LOCK SAFETY: CONCURRENTLY + NO TRANSACTION. Review events are written by every verifier
-- open/scrub/close; a blocking build would stall the verification drawer.
CREATE INDEX CONCURRENTLY IF NOT EXISTS verification_review_events_proof_idx
ON public.verification_review_events (proof_id)
WHERE proof_id IS NOT NULL;

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS public.verification_review_events_proof_idx;
