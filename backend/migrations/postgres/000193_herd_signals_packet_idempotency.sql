-- Herd Signals: packet-level idempotency.
--
-- herd_signal_packets was write-once/append-only with no dedup key, so a replayed gateway
-- batch (same packets re-POSTed after a network retry) would double-insert every packet and
-- double-count packet_count in herd_signal_activity_windows. A gateway is expected to retry on
-- timeout, so this is not a hypothetical: it is the normal failure mode of the ingest endpoint.
--
-- (tenant_id, tag_id, received_at, motion_count) identifies "the same packet": motion_count is
-- cumulative on the tag, so two packets from the same tag at the same received_at timestamp with
-- the same motion_count are the same physical reading, never two distinct ones.
--
-- NULLS NOT DISTINCT (maintainer scale review, HIGH-severity M1): a battery/temperature-only
-- advertisement can carry NULL motion_count. Standard unique-index NULL semantics treat every
-- NULL as distinct from every other NULL, so two such packets at the same (tenant, tag,
-- received_at) would NEVER conflict and a retried batch would double-insert them -- exactly the
-- failure this migration exists to close, just for the NULL-motion_count case.

-- +goose Up
CREATE UNIQUE INDEX IF NOT EXISTS herd_signal_packets_dedup_uidx
  ON public.herd_signal_packets (tenant_id, tag_id, received_at, motion_count) NULLS NOT DISTINCT;

-- +goose Down
DROP INDEX IF EXISTS public.herd_signal_packets_dedup_uidx;
