-- Herd Signals: the packet-loss instrument (pkt_sn) and the gateway heartbeat (sta_gw_hb).
--
-- BOTH COLUMNS EXIST BECAUSE A PAYLOAD AUDIT OF THE LIVE GATEWAY STREAM FOUND WE WERE THROWING
-- AWAY THE ONLY TWO FACTS THAT TELL US WHETHER WE ARE HEARING EVERYTHING WE SHOULD.
--
-- 1. pkt_sn is the gateway's per-report sequence number. It is the ONLY packet-loss instrument
--    this protocol gives us: nothing else in the payload says "there was a report between these
--    two that you never received". The audit measured 0 gaps in one capture window and ~0.03%
--    loss in another, so this becomes a real health metric the moment it is stored rather than a
--    theoretical one. It was previously only stashed inside raw_payload jsonb by the MQTT bridge,
--    which cannot be aggregated or indexed, and only compared in the bridge's OWN process memory,
--    which resets on every bridge restart.
--
--    A DECREASE in pkt_sn means the GATEWAY rebooted (its counter restarted), exactly like a
--    tag's motion_count reset. It is never a negative loss: on a decrease we record the reboot
--    and re-anchor, we never subtract. Loss accounting only accrues on a forward jump, where
--    missed = new - last - 1.
--
-- 2. Heartbeats. The gateway emits pkt_type:"state" with data.state "sta_gw_hb" and a ticks_cnt
--    roughly every 5 minutes, carrying no dev_infos at all. We were DISCARDING them on the API
--    ingest path. Without them "gateway up but hearing no tags" (heartbeats arriving, zero tags
--    decoded -- a dead antenna, a shed with no animals in it, a misaimed unit) is indistinguishable
--    from "gateway down" (nothing arriving at all). A product review flagged that as a real
--    operator failure: the two demand opposite responses and the dashboard could not tell them
--    apart. ticks_cnt going BACKWARDS is a gateway reboot, same rule as pkt_sn.
--
-- last_heartbeat_at is deliberately SEPARATE from last_seen_at. last_seen_at answers "did we hear
-- anything from this gateway", which either kind of message satisfies; last_heartbeat_at answers
-- "is the gateway itself alive", which only a heartbeat satisfies. A gateway with a fresh
-- last_heartbeat_at and no recent tag packets is the "up but hearing nothing" case that could not
-- be expressed before.

-- +goose Up

ALTER TABLE public.herd_signal_packets
  ADD COLUMN IF NOT EXISTS pkt_sn bigint;

COMMENT ON COLUMN public.herd_signal_packets.pkt_sn IS
  'Gateway per-report sequence number. The only packet-loss instrument in this protocol; a decrease means a gateway reboot, never negative loss.';

ALTER TABLE public.herd_signal_gateways
  ADD COLUMN IF NOT EXISTS last_heartbeat_at timestamptz,
  ADD COLUMN IF NOT EXISTS last_ticks_cnt bigint,
  ADD COLUMN IF NOT EXISTS heartbeat_reboot_count integer NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS last_pkt_sn bigint,
  ADD COLUMN IF NOT EXISTS pkt_sn_reboot_count integer NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS packets_missed_total bigint NOT NULL DEFAULT 0;

COMMENT ON COLUMN public.herd_signal_gateways.last_heartbeat_at IS
  'Last sta_gw_hb heartbeat. Separate from last_seen_at so "up but hearing no tags" is distinguishable from "down".';
COMMENT ON COLUMN public.herd_signal_gateways.packets_missed_total IS
  'Cumulative pkt_sn gaps (new - last - 1) since first observation. Only accrues on a forward jump; a reboot re-anchors instead.';

-- +goose Down

ALTER TABLE public.herd_signal_gateways
  DROP COLUMN IF EXISTS packets_missed_total,
  DROP COLUMN IF EXISTS pkt_sn_reboot_count,
  DROP COLUMN IF EXISTS last_pkt_sn,
  DROP COLUMN IF EXISTS heartbeat_reboot_count,
  DROP COLUMN IF EXISTS last_ticks_cnt,
  DROP COLUMN IF EXISTS last_heartbeat_at;

ALTER TABLE public.herd_signal_packets
  DROP COLUMN IF EXISTS pkt_sn;
