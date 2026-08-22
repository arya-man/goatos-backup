# HoneyComm BLE Gateway Payload — Field Coverage Audit

Audit date: 2026-08-22. Sources (live capture, still appending — counts are per-read snapshots):

- `/Users/ravi/mesha/local-data/honeycomm-gateway-capture/raw_scan_reports.ndjson` — authority. First read: 11,729 records / 196,564 device rows; second read minutes later: 11,801 records. Envelope time range `2026-08-22 18:22:08` .. `21:38:10` (gateway clock).
- `/Users/ravi/mesha/local-data/honeycomm-gateway-capture/decoded_ear_tags.csv` — 18,355 rows at read time, which exactly equalled the count of HoneyComm-tag advertisement rows in the NDJSON at the same moment (18,355). The CSV keeps ONLY HoneyComm ear-tag rows; all other BLE devices and all gateway heartbeat records are absent from it.

Persistence targets compared: `backend/migrations/postgres/000191_herd_signals.sql` (+000192 dedup index, 000193 motion_delta_1h), `backend/internal/herdsignals/domain/types.go` (`IngestPacket`), `backend/cmd/seed-herd-signals-oci/main.go`, `backend/internal/herdsignals/app/service.go`.

## 1. Complete NDJSON key inventory (mechanical enumeration, all 11,729 records)

### 1a. Envelope keys — `pkt_type: "scan_report"` (11,690 records)

| Key (verbatim) | Type | Distinct | Min/Max or example | Presence | Varies? |
|---|---|---|---|---|---|
| `pkt_type` | str | 2 | `"scan_report"`, `"state"` | 100% | per record type |
| `gw_addr` | str | 1 | `"f130d402dcb4"` | 100% | constant (single gateway in capture) |
| `time` | str | 11,690 | `"2026-08-22 18:22:08"` — zone-less local wall clock, 1 s resolution, no epoch, no offset | 100% | per report |
| `msec` | str | 65–1000 | `"820"` — zero-padded string milliseconds | 100% | per report |
| `data.flags` | int | 2 | 5 (scan_report), 0 (state) | 100% | 2 values only |
| `data.pkt_sn` | int | 11,213+ | 0 .. 11,326 (grew during audit) | scan_report only | monotonic sequence: 2 gaps, 3 missing serials, 1 reset across 11,801 reports |
| `data.report_type` | str | 1 | `"adv_only"` | scan_report only | constant |
| `data.pkt_total` | int | 1 | 1 | scan_report only | constant |
| `data.pkt_index` | int | 1 | 0 | scan_report only | constant |
| `data.dev_total` | int | 15 | 10–24 | scan_report only | per report; always equal to `dev_num` (0 mismatches in 11,801 reports) |
| `data.dev_num` | int | 15 | 10–24 | scan_report only | per report |
| `data.dev_infos` | array | — | 10–24 device rows | scan_report only | per report |

### 1b. Envelope keys — `pkt_type: "state"` gateway heartbeats (39 records)

Emitted every 5 minutes (`18:24:56`, `18:29:56`, ...):

| Key | Type | Distinct | Values | Notes |
|---|---|---|---|---|
| `data.state` | str | 1 | `"sta_gw_hb"` | gateway heartbeat marker |
| `data.msgId` | int | 39 | 5..43, +1 each | heartbeat sequence |
| `data.ticks_cnt` | int | 39 | 5..43, equal to `msgId` | uptime counter in 5-minute ticks; a reset to a low value = gateway reboot |
| `data.flags` | int | 1 | 0 | |

### 1c. Per-device row keys inside `data.dev_infos[]` (196,564 rows, 434 distinct `addr`)

| Key (verbatim) | Type | Distinct | Min/Max or example | Notes |
|---|---|---|---|---|
| `addr` | str | 434 | `"f0c990a00033"` | BLE MAC, lowercase hex. 20 addrs are HoneyComm ear tags (`f0c990a0xxxx`); 414 are ambient devices (phones, beacons, etc.) |
| `rssi` | int | 62 | -93 .. -32 | dBm at this gateway |
| `time` | str | 11,693 | same format as envelope `time` | per-row scan instant; differs from envelope time by 0 s (172,678 rows) or -1 s (23,886 rows) — the row is stamped when heard, the envelope when reported |
| `msec` | str | 1000 | `"771"` | per-row milliseconds — full ms resolution exists per detection |
| `name` | str | 20 | `"null"` (literal string), `"mTnA"`, `"i2224e-s"` | BLE advertised local name. All 18,355 HoneyComm tag rows carry `"mTnA"` (constant model string, not an identity) |
| `adv_raw` | str | 1,208 | hex string | COMPLETE advertisement payload bytes (see 1d) |

No other keys exist at any nesting level. There is no explicit timezone, epoch, uptime, TX power, channel, MAC-address-type, PHY, or firmware field anywhere in the JSON — anything of that kind exists only inside `adv_raw`.

### 1d. HoneyComm ear-tag `adv_raw` byte layout (verified against 18,355 rows; all exactly 31 bytes, 93 distinct strings)

`02 01 06 | 15 16 4C AB | b7 b8 b9 b10 | mac[6] | b17 b18 b19 b20 | b21..b24 | 05 09 6D 54 6E 41`

| Bytes | Meaning | Observed values | CSV/persist coverage |
|---|---|---|---|
| 0–2 `020106` | AD flags | constant | inside stored `adv_raw` |
| 3–6 `15164CAB` | Service-data AD, UUID 0xAB4C, len 21 | constant | inside `adv_raw` |
| b7 | frame/protocol version (presumed) | 1 (constant, 18,355/18,355) | not decoded; recoverable from `adv_raw` |
| b8 | battery, decivolts | 31 (17,391 rows), 32 (964 rows) → 3.1/3.2 V | decoded → `battery_v` → `battery_mv` (lossless) |
| b9 | battery percent (presumed) | 100 (constant) | not decoded; recoverable |
| b10 | reserved/unknown | 0 (constant) | not decoded; recoverable |
| b11–16 | tag MAC | equals row `addr` in all 18,355 rows (0 mismatches) | `tag_mac` |
| b17 | `sensor_state` bitfield | 10 = `0b00001010` — the ONLY value ever observed | decoded (see §3) |
| b18 | tag temperature, integer °C | 24–28 | decoded |
| b19 | tag temperature fraction, /100 | only multiples of 10 observed ({0,10,...,90}) → effective 0.1 °C resolution; CSV's 1-decimal is lossless in practice | decoded |
| b20 | reserved/unknown | 0 (constant) | not decoded; recoverable |
| b21–24 | `motion_count`, 32-bit big-endian | max observed 11,171; strictly monotonic per tag, ZERO resets/rollbacks across 20 tags over 3h16m | decoded → bigint (lossless; counter width is 32-bit, rollover at 2^32) |
| 25–30 | Complete Local Name AD: `"mTnA"` | constant | inside `adv_raw` |

Tag identity beyond printed id + MAC: none. `printed_id` (`A00033`) is exactly the last 3 MAC bytes uppercased — derived, not independent.

## 2. Gateway clock skew (verified from raw data)

Pairing all 18,355 CSV rows (`packet_time` = gateway per-row time+msec vs `received_at` = server wall clock, whole seconds):

- skew = gateway − server: min 8,997.29 s, max 9,000.82 s, mean 8,999.84 s, median 8,999.85 s ≈ **+02:30:00**.
- Drift check: mean of first quartile 8,999.64 s vs last quartile 9,000.02 s over ~3h16m → no monotonic drift; the ±1 s spread is jitter plus the 1-second truncation of `received_at`. **This is a fixed offset, not a drifting clock** — consistent with the gateway clock being set to UTC+8 (IST+02:30), i.e. a timezone misconfiguration (common vendor default), not broken NTP.
- Per-device `time` vs envelope `time`: delta is 0 s (172,678 rows) or −1 s (23,886 rows) — same clock, row stamped at hearing, envelope at reporting.
- Loader treatment confirmed correct in direction: `received_at` = server time (IST-interpreted, `captureZone` in main.go), `gateway_seen_at` = gateway clock verbatim.

## 3. `sensor_state` bit analysis

Observed value: **10 = `0b00001010` in 18,355/18,355 rows** (CSV column `sensor_state` = "10" in every row). Bits 1 and 3 are set; the code decodes exactly two booleans (`temp_sensor_ok`, `accel_sensor_ok`), both always true. Bits 0, 2, 4, 5, 6, 7 have never been observed set, so no observed value implies ignored semantics today — but the field is a full byte and the meaning of the other six bits is unconfirmed by vendor documentation. The raw byte IS persisted (`sensor_state smallint`), so future bit semantics are recoverable for stored packets.

## 4. Capture-vs-persist comparison

Legend: column names refer to `herd_signal_packets` unless noted.

| Gateway field | Our column | Lossless? | Matters? |
|---|---|---|---|
| envelope `pkt_type` | DROPPED | — | Low alone, but `state` heartbeats are dropped wholesale (below) |
| envelope `gw_addr` | DROPPED (gateway_id is an operator-supplied flag; `ble_mac` column on `herd_signal_gateways` exists but is never populated from capture) | no | Medium — the gateway's self-reported BLE MAC is the only in-band gateway identity; needed to detect a mis-attributed or swapped gateway |
| envelope `time`+`msec` | partially → per-row `packet_time` in CSV | no | see gateway_seen_at row |
| `data.pkt_sn` | DROPPED | no | **High** — the only packet-loss instrument. Measured: 2 gaps / 3 missing serials / 1 reset across 11,801 reports (~0.03% report loss). Unrecoverable once the NDJSON is gone |
| `data.flags` | DROPPED | no | Low — 2 values observed; cheap to keep in raw_payload |
| `data.report_type` | DROPPED | no | Low — constant `"adv_only"`; would signal a gateway mode change |
| `data.pkt_total` / `pkt_index` | DROPPED | no | Low today (always 1/0) — but nonzero values would mean multi-part reports we would silently truncate; worth an ingest assertion |
| `data.dev_total` / `dev_num` | DROPPED | no | Low — always equal; a mismatch would indicate gateway-side truncation |
| heartbeats: `data.state`, `data.msgId`, `data.ticks_cnt` | DROPPED entirely (never reach the CSV) | no | **High** — 5-minute heartbeats are the natural feed for `herd_signal_gateways.last_seen_at`/`status`, and `ticks_cnt` reset = reboot detection. Currently gateway last_seen only moves when tag packets arrive |
| per-device `addr` | `tag_mac` | yes | — |
| per-device `rssi` | `rssi_dbm` | yes | — (single-gateway RSSI; per-gateway RSSI matrix impossible until dedup key carries gateway, see §5) |
| per-device `time`+`msec` | **MANGLED** → `gateway_seen_at` | no | **High** — two independent losses: (1) CSV `received_at` truncates server time to whole seconds while gateway per-row `msec` proves sub-second detection exists; (2) `IngestPacket` has NO per-packet gateway timestamp — the loader collapses `packet_time` to the batch MAX (batch=200 packets, i.e. minutes) and `service.go` stamps that one request-level value onto every packet's `gateway_seen_at`. The stored `gateway_seen_at` is therefore up to minutes wrong per packet |
| per-device `name` | DROPPED | derivable from `adv_raw` | Low — constant `"mTnA"` model string on tags; recoverable |
| per-device `adv_raw` | `raw_adv` | **yes — complete 31-byte advertisement stored verbatim** | This is the saving grace: b7/b9/b10/b20, the name AD, and any future re-decode are recoverable for HoneyComm rows that reach the CSV |
| non-HoneyComm device rows | DROPPED before CSV (178,209 of 196,564 rows, 414 addrs) | no | Accept — ambient BLE noise; but note it is unrecoverable and includes any future non-`4CAB` tag firmware |
| CSV `gateway_ip` | DROPPED by loader | no | Low — network diagnostics only |
| adv b8 battery | `battery_mv` | yes (0.1 V resolution is the sensor's own) | — |
| adv b18/b19 temperature | `tag_temperature_c numeric(5,2)` | yes (fraction only ever multiples of 10 → 0.1 °C native) | — |
| adv b21–24 `motion_count` | `motion_count bigint` | yes | monotonic, 0 resets observed, max 11,171, 32-bit width |
| adv b17 `sensor_state` | `sensor_state smallint` + 2 booleans | yes (raw byte kept) | — |
| — | `raw_payload jsonb` | always `{}` — `service.go:80` hardcodes empty map | The column built to hold envelope/extra fields is never populated |

Not in the payload at all (so not droppable, but confirms limits): TX power, channel, MAC address type, PHY, firmware/hardware version, advertisement type — none exist in the JSON; TX power/flags only insofar as they appear inside `adv_raw` AD structures (they do not for the HoneyComm frame).

## 5. Multi-gateway overlap

The entire capture contains exactly ONE `gw_addr` (`f130d402dcb4`). Zero instances of the same `(addr, time, msec, adv_raw)` heard by more than one receiver in 196,564 rows. So the dedup key `(tenant_id, tag_id, received_at, motion_count)` without `gateway_id` (000192) is not dropping anything **today** — but the moment a second gateway is deployed, a same-second copy of the same advertisement (same motion_count) at the other receiver WILL be silently discarded, destroying exactly the per-gateway RSSI data any location estimate needs. This is a latent, not active, loss.

## 6. Ranked additions

1. **Per-packet gateway timestamp** — add `gateway_time`/`msec` to `IngestPacket` and stop collapsing to batch max in the loader/service. Current `gateway_seen_at` is up to minutes wrong per packet; unrecoverable once captures rot.
2. **`pkt_sn` (report sequence)** — persist per report (or per packet as `report_sn`). Only possible packet-loss measurement; already demonstrated 3 lost reports + 1 sequence reset in this capture.
3. **Gateway heartbeats** — ingest `pkt_type:"state"` (`state`, `msgId`, `ticks_cnt`) into `herd_signal_gateways.last_seen_at`/`status` + a small heartbeat log; `ticks_cnt` reset = reboot detection.
4. **Populate `raw_payload`** — the jsonb column exists and is always `{}`. Storing the envelope fields (`gw_addr`, `flags`, `report_type`, `pkt_total`, `pkt_index`, `dev_total`, `dev_num`, per-row `name`, `gateway_ip`) there closes every remaining Low-severity drop with no schema change.
5. **Add `gateway_id` to the dedup unique index** before a second gateway is deployed (`(tenant_id, gateway_id, tag_id, received_at, motion_count)`), or the second receiver's copies — the per-gateway RSSI signal — are silently dropped.
6. **Sub-second `received_at` in the capture writer** — the CSV truncates server time to 1 s; the dedup key then also operates at 1 s grain. Emit fractional seconds.
7. **`gw_addr` → `herd_signal_gateways.ble_mac`** on ingest, so the in-band gateway identity is verified against the registered one.
8. (No action, documented) `sensor_state` bits other than 1 and 3 have never been observed set; raw byte is stored, so future semantics are recoverable. b7/b9/b10/b20 are constant and recoverable from stored `raw_adv`.

## 7. Proof artifacts

Scripts: `/Users/ravi/goatos-work/herd-signals-proof/enumerate.py`, `/Users/ravi/goatos-work/herd-signals-proof/decode2.py`.
