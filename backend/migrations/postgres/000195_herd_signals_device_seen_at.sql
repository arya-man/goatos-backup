-- Herd Signals: received_at becomes server-stamped; the caller-supplied timestamp survives only
-- as a diagnostic and as the dedup key's identity of "the same physical packet".
--
-- Security review (HIGH): received_at was set from the ingest request's caller-supplied
-- per-packet `seen_at` field. Staleness, gap detection (IsGapDelta), the packet dedup key
-- (000192/000194), and the advance-only "latest" guard in updateTagLatest all keyed off
-- received_at -- so all four were attacker/clock controlled. A far-future value would
-- permanently freeze a tag's live state (the advance-only guard rejects every subsequent real
-- packet as "not newer"), and a chosen timestamp could fabricate or suppress gap_delta at will.
--
-- received_at is now stamped from the SERVER clock at ingest time (one value per ingest call,
-- applied to every packet in the batch -- they were received together). The caller's claimed
-- timestamp is preserved verbatim in device_seen_at for diagnostics ONLY; no staleness/gap/
-- ordering/advance-only decision in this module reads it.
--
-- Consequence for the dedup key: 000192's unique index was (tenant_id, tag_id, received_at,
-- motion_count) precisely BECAUSE received_at used to be the device's own capture instant, so a
-- retried batch (same physical packets re-POSTed after a network timeout) carried the same
-- received_at and collided correctly. Now that received_at is stamped fresh per ingest call, a
-- retry would get a NEW received_at and the old key would silently stop deduplicating retries --
-- reintroducing the exact double-count bug 000192 closed. The dedup identity moves to
-- device_seen_at (the one piece of this row that IS stable across a retry), used HERE ONLY for
-- "is this the same physical packet" -- never for a decision.

-- +goose Up
ALTER TABLE public.herd_signal_packets
  ADD COLUMN IF NOT EXISTS device_seen_at timestamptz;

DROP INDEX IF EXISTS public.herd_signal_packets_dedup_uidx;

CREATE UNIQUE INDEX IF NOT EXISTS herd_signal_packets_dedup_uidx
  ON public.herd_signal_packets (tenant_id, tag_id, device_seen_at, motion_count) NULLS NOT DISTINCT;

-- +goose Down
DROP INDEX IF EXISTS public.herd_signal_packets_dedup_uidx;

CREATE UNIQUE INDEX IF NOT EXISTS herd_signal_packets_dedup_uidx
  ON public.herd_signal_packets (tenant_id, tag_id, received_at, motion_count) NULLS NOT DISTINCT;

ALTER TABLE public.herd_signal_packets
  DROP COLUMN IF EXISTS device_seen_at;
