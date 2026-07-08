// Package bulkstatus is the durable bulk status-update kernel.
//
// It lets an operator preview and then enqueue a large (up to millions of rows)
// status change across one axis (reproductive | health | exit) for many goats at
// once, without applying the change inline in the request. A commit enqueues a
// bulk_status_job plus one bulk_status_job_row per goat; a bounded, rate-limited
// worker later claims pending/retry rows with FOR UPDATE SKIP LOCKED and applies
// each row through the SAME per-goat identity transition (ReproductiveGoat /
// HealthGoat / ExitGoat) so the proper domain event, decision record, outbox
// message and guardrails all fire.
//
// Resume truth is the row_state ledger (re-scan pending|retry), never a positional
// cursor. Row-level idempotency and no-clobber are anchored on
// (job_id, goat_id, axis, target, expected_row_version): if the goat's live
// row_version no longer matches the captured expected_row_version the row is
// skipped instead of clobbering a newer write.
package bulkstatus
