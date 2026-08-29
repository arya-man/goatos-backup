# HoneyComm BLE Gateway — Live MQTT Stream Enumeration

Capture date: 2026-08-22, 22:37:28–22:46:12 local receipt (8m44s window).
Source: live Mosquitto broker at 192.168.0.5:1883 (no TLS, no auth), subscription `#`,
`mosquitto_sub -v`, each line stamped with the local receive clock at read time.
Raw capture: `/Users/ravi/goatos-work/herd-signals-proof/mqtt/capture.raw` (527 messages);
analyzer: `/Users/ravi/goatos-work/herd-signals-proof/mqtt/analyze.py`.
Companion doc (CSV-era audit, 11,801 reports): `herd-signals-gateway-payload.md`.

This doc enumerates everything the gateway emits over MQTT so the bridge design can prove
what is captured and what is silently dropped. Language note: everything here is BLE tag
telemetry — RSSI, tag temperature, cumulative accelerometer motion counts, battery voltage.
Nothing in this stream is a direct observation of the animal.

## 1. Stream shape

| Metric | Value |
|---|---|
| Topics observed on `#` | `GwData` only (527/527 messages). `SrvData` / `AlphaCmd` (the gateway's subscriptions) carried nothing during the window — no server was publishing. |
| Message rate | 1.01 msg/s (527 msgs / 524.0 s) — one `scan_report` per second |
| Message size (payload bytes) | min 152, p50 2,942, mean 2,962, max 4,007; 1,561,174 bytes total ≈ 2.98 KB/s ≈ 257 MB/day per gateway |
| Non-JSON payloads | 0 |
| Gateways | 1 (`gw_addr` `"f130d402dcb4"`, constant) |

## 2. Mechanical key enumeration per `pkt_type`

Two `pkt_type` values observed: `scan_report` (525) and `state` (2). No unexpected types —
both match the CSV-era audit. Every key path below is from walking all 527 parsed messages;
no other key exists at any nesting level.

### 2a. `pkt_type: "scan_report"` (525 messages, 1/s)

| Key path | Type | Distinct | Min/Max or example | Presence | Varies |
|---|---|---|---|---|---|
| `pkt_type` | str | 1 | `"scan_report"` | 100% | constant |
| `gw_addr` | str | 1 | `"f130d402dcb4"` | 100% | constant (per-gateway) |
| `time` | str | 525 | `"2026-08-22 22:37:28"` — zone-less wall clock, 1 s resolution | 100% | per message |
| `msec` | str | 1 | `"714"` — envelope msec was CONSTANT this window (per-second scheduler tick) | 100% | constant here (65–1000 in the longer CSV-era capture) |
| `data.flags` | int | 1 | 5 | 100% | constant |
| `data.pkt_sn` | int | 525 | 738..1262, +1 each | 100% | monotonic sequence |
| `data.report_type` | str | 1 | `"adv_only"` | 100% | constant |
| `data.pkt_total` | int | 1 | 1 | 100% | constant |
| `data.pkt_index` | int | 1 | 0 | 100% | constant |
| `data.dev_total` | int | 11 | 15..25 | 100% | per message; equal to `dev_num` in all 525 |
| `data.dev_num` | int | 11 | 15..25 | 100% | per message |
| `data.dev_infos[]` | array | — | 15–25 rows | 100% | per message |

### 2b. `pkt_type: "state"` gateway heartbeats (2 messages)

Observed at `22:39:57` and `22:44:57` gateway clock — exactly 5 minutes apart, confirming the
5-minute heartbeat cadence (the ≥6–8 min window was chosen to catch two, and did).

| Key path | Type | Values | Notes |
|---|---|---|---|
| `pkt_type` | str | `"state"` | |
| `gw_addr` | str | `"f130d402dcb4"` | |
| `time` / `msec` | str | per message | |
| `data.flags` | int | 0 | |
| `data.state` | str | `"sta_gw_hb"` | heartbeat marker |
| `data.msgId` | int | 56, 57 | +1 per heartbeat |
| `data.ticks_cnt` | int | 56, 57 | equals `msgId`; uptime in 5-min ticks (56 ≈ 4h40m up); a reset to low value = reboot |

### 2c. `data.dev_infos[]` row keys (9,660 rows)

| Key | Type | Distinct | Range/example | Notes |
|---|---|---|---|---|
| `addr` | str | 67 | `"f0c990a00036"` | BLE MAC, lowercase hex |
| `rssi` | int | 57 | -91..-32 | dBm at this gateway |
| `time` | str | 525 | envelope format | per-row scan instant; row−envelope offset -0.996..0.000 s (mean -0.228 s) |
| `msec` | str | 966 | `"000"`..`"999"` | full per-detection ms resolution |
| `name` | str | 5 | `"null"` (literal), `"mTnA"`, `"i2224e-s"`, `"OnePlus Nord Buds 3"`, `"TVSBT20002283"` | advertised local name |
| `adv_raw` | str | 137 | hex | complete advertisement payload |

## 3. Device rows: ours vs foreign

- 9,660 device rows, 67 distinct `addr`.
- **20 HoneyComm ear tags** (`f0c990a0002a`..`f0c990a00041`, prefix `f0c990a0`): 764 rows (7.9%).
- **47 foreign devices**: 8,896 rows (92.1%) — **mean 16.9 foreign rows discarded per report**.
  Foreign = phones/earbuds (named rows include `"OnePlus Nord Buds 3"`, `"TVSBT20002283"`),
  Apple continuity frames (`FF4C00` manufacturer data), and randomised-MAC beacons.

**Bridge predicate** (all three held with zero exceptions in this capture; any one suffices,
using all three plus a length check is the robust form):

1. `addr` starts with `f0c990a0` — 764/764 of ours, 0/8,896 foreign.
2. `adv_raw` starts with `02010615164CAB` (flags AD + 21-byte service-data AD, UUID `0xAB4C`) —
   764/764 ours; **0 foreign rows contain `164CAB` anywhere**.
3. `name == "mTnA"` — 764/764 ours; 0 foreign.
4. `len(adv_raw) == 62` hex chars (31 bytes) — 764/764.

Recommended: accept iff `adv_raw` prefix `02010615164CAB` AND 31-byte length; log a warning if
the `addr` prefix or name disagrees (would indicate new firmware or spoofing). A foreign
advertisement can never be mistaken for ours under predicate 2: no foreign frame carried the
`0xAB4C` service UUID in 8,896 rows.

## 4. `adv_raw` decode — independent re-verification (764 rows, all 31 bytes)

Layout confirmed byte-for-byte against the prior audit:
`02 01 06 | 15 16 4C AB | b7 | b8 | b9 | mac[10..15] | b16(part of mac) | b17 | b18 b19 | b20 | b21..b24 | 05 09 6D 54 6E 41`

| Bytes | Meaning | This capture (independent) | Verdict |
|---|---|---|---|
| 0–6 | flags AD + service-data header `164CAB` | constant | confirmed |
| 7 | frame/protocol version | constant `0x01` | confirmed constant, semantics still vendor-unconfirmed |
| 8 | battery decivolts | `0x1f`(31)=3.1 V (19 tags), `0x20`(32)=3.2 V (tag `...2e` only) | **confirmed** — plausible coin-cell values, consistent per tag across the window |
| 9 | battery percent | constant `0x64` (100) | confirmed constant |
| 10–15 | tag MAC embedded | equals row `addr` in **764/764** rows (`f0c990a0xxxx`) | **confirmed** — printed tag id = last 3 MAC bytes (e.g. `a00036`) |
| 16 | last MAC byte | 20 distinct values = the 20 tags | part of MAC, confirmed |
| 17 | sensor_state | constant `0x0a` (bits 1 and 3 set) | confirmed; the other 6 bits never observed set |
| 18 | temperature integer | `0x18`–`0x1a` (24–26) | **confirmed** — 24.7–26.4 °C tag temperature, consistent with indoor ambient; per-tag spread ≤0.2 °C across the window |
| 19 | temperature fraction | only multiples of 10 (`00`,`0a`,`14`..`5a`) | confirmed — native resolution 0.1 °C |
| 20 | reserved | constant `0x00` | still unexplained (constant) |
| 21–24 | motion_count, 32-bit **big-endian** | 5,843..11,171; bytes 21–22 constant `0000`, variation entirely in 23–24 — exactly what big-endian predicts for counts <65,536 | **confirmed** — 0 decreases in any tag's sequence (20 tags, 29–43 readings each); one tag (`...3b`) advanced 8095→8111 in-window, the rest were static |
| 25–30 | `05 09 6D 54 6E 41` | complete-local-name AD = `"mTnA"` | confirmed |

Unexplained bytes remaining: **b7 (const 0x01), b9 (const 100), b20 (const 0x00)**, and 6 of 8
bits in b17 — all constants so far, all recoverable later from stored `raw_adv`.

## 5. The clock

Comparing gateway-claimed time (`time`+`msec`) against the local receive clock stamped per line:

| Measurement | Value |
|---|---|
| envelope_time − receipt_time | min +8996.7 s, max +8999.9 s, **mean +8999.80 s ≈ +02:29:59.8** |
| Drift across window | first-half mean +8999.84 s, second-half +8999.76 s → **-0.08 s over 262 s** — within scheduling jitter, i.e. **CONSTANT, not drifting** |
| dev row time vs envelope time | -0.996..0.000 s (mean -0.228 s): rows are stamped when heard, envelope when reported |

**Resolution of the +02:30:00 vs +2h33m discrepancy**: the live offset is +02:30:00 (9,000 s)
to within 0.2 s, constant across the window — matching the CSV-era measurement. The "+2h33m"
spot check was not reproduced; at 0.08 s measured drift over 4.4 minutes the clock cannot have
drifted 3 minutes since the CSV capture, so that spot check was a measurement artifact (likely
compared against an unsynced clock or included pipeline latency). Treat the gateway clock as a
**constant +02:30:00 misconfiguration** (timezone/offset error, NTP either absent or disciplined
to the wrong base), and never trust `time` for ordering — `received_at` (server clock) is truth,
per migration 000196.

## 6. `pkt_sn` — loss instrument

- 525 consecutive `scan_report` messages: `pkt_sn` 738 → 1262, strictly +1 each.
- **Gaps: 0. Missing serials: 0. Resets: 0. Observed loss rate 0/525 = 0.000%** over 8m44s on
  the local network. (CSV-era longer capture measured ~0.03% with 1 reset over 11,801 reports —
  a reset means gateway reboot; the bridge must treat `pkt_sn` decrease as reboot, not loss.)

## 7. Loss accounting — gateway emission → schema → IngestPacket → bridge

Persistence targets: `backend/migrations/postgres/000192_herd_signals.sql` (+000193 dedup,
000194 motion_delta_1h, 000195 gap_delta, 000196 device_seen_at),
`backend/internal/herdsignals/domain/types.go` (`IngestPacket`, which now HAS per-packet
`gateway_seen_at` and free-form `raw_payload` — both were missing in the CSV era).
"Bridge captures" assumes the planned bridge does what `IngestPacket.RawPayload`'s doc comment
says: store `gw_addr`, `pkt_sn`, and raw dev_info context in `raw_payload`.

| Gateway field | In schema? | In `IngestPacket`? | Bridge would capture? | Status | Cost if lost |
|---|---|---|---|---|---|
| `dev_infos[].addr` | `tag_mac` | `tag_mac` | yes | **CAPTURED** | — |
| `dev_infos[].rssi` | `rssi_dbm` | `rssi_dbm` | yes | **CAPTURED** | — |
| `dev_infos[].adv_raw` | `raw_adv` (verbatim) | `raw_adv` | yes | **CAPTURED** — saving grace: b7/b9/b20/b17-bits and any future re-decode stay recoverable | — |
| adv b8 battery | `battery_mv` | `battery_mv` | yes | CAPTURED (0.1 V native) | — |
| adv b18/b19 tag temperature | `tag_temperature_c numeric(5,2)` | `tag_temperature_c` | yes | CAPTURED (0.1 °C native) | — |
| adv b21–24 motion_count | `motion_count bigint` | `motion_count` | yes | CAPTURED | — |
| adv b17 sensor_state | `sensor_state` + 2 bools | `sensor_state`, both OK bools | yes | CAPTURED | — |
| `dev_infos[].time`+`msec` (per row) | `device_seen_at timestamptz` (000196) + `gateway_seen_at` | `seen_at` + per-packet `gateway_seen_at` | yes — **fixed since the CSV-era audit**; bridge must combine `time`+`msec` per ROW (ms resolution exists: 966 distinct msec values) and correct/annotate the +02:30:00 offset | CAPTURED (diagnostic only; decisions use server `received_at`) | — |
| envelope `gw_addr` | no column populated (`herd_signal_gateways.ble_mac` exists, never fed) | only via `raw_payload` | yes IF bridge stores it in `raw_payload` per the doc comment | **DROPPED-RECOVERABLE** (from raw_payload) once bridge complies; today nothing writes it | Medium — only in-band gateway identity; needed to detect swapped/mis-attributed gateways. Bridge should also upsert `herd_signal_gateways.ble_mac` |
| `data.pkt_sn` | no column | only via `raw_payload` | yes IF stored in `raw_payload` (doc comment says it will be) | **DROPPED-RECOVERABLE** via raw_payload, but gap/reset ANALYSIS is lost unless something reads it | **High** — the only packet-loss and reboot instrument (0.000% here, 0.03% + 1 reset in CSV era). Recommend bridge computes gap/reset events, not just stores the number |
| heartbeats `state`/`msgId`/`ticks_cnt` | no table/column | no field — an `IngestRequest` has no heartbeat concept | **NO** — bridge as planned drops the entire `state` message class | **DROPPED-LOST FOREVER** | **High** — 5-min heartbeats are the natural feed for `herd_signal_gateways.last_seen_at`/`status`, and `ticks_cnt` reset = reboot detection; without them gateway liveness only moves when tag packets arrive. Cheapest fix: bridge maps heartbeat → gateway upsert (last_seen_at, plus ticks_cnt in a raw/diagnostic field) |
| envelope `time`+`msec` | — | envelope `gateway_seen_at` (batch fallback) | yes | CAPTURED (diagnostic) | — |
| `data.flags` | no | `raw_payload` | if stored | DROPPED-RECOVERABLE | Low — 2 values ever observed (5 scan / 0 state) |
| `data.report_type` | no | `raw_payload` | if stored | DROPPED-RECOVERABLE | Low — constant `"adv_only"`; a change would signal a gateway mode switch. Bridge should assert == `adv_only` |
| `data.pkt_total`/`pkt_index` | no | `raw_payload` | if stored | DROPPED-RECOVERABLE | Low today (always 1/0) — but a multi-part report would otherwise be silently truncated; bridge should assert 1/0 and alarm otherwise |
| `data.dev_total`/`dev_num` | no | `raw_payload` | if stored | DROPPED-RECOVERABLE | Low — equal in 525/525; a mismatch = gateway-side truncation. Assert equality |
| `dev_infos[].name` | no | no | filtered on, not stored | DROPPED-RECOVERABLE (constant `"mTnA"` is derivable from `adv_raw` bytes 25–30) | Low |
| Foreign device rows (47 addrs, 8,896 rows = 92.1% of rows, mean 16.9/report) | no | no | **NO** — filtered by the §3 predicate | **DROPPED-LOST FOREVER** (accepted) | Low-Medium — ambient BLE noise by design; but this includes any future tag firmware that changes the service UUID, and any third-party asset beacon we might later care about. The MQTT capture file is the only record |
| `SrvData` / `AlphaCmd` topics | — | — | not subscribed | nothing observed (0 messages) | Unknown — capture on `#` again once a server/commander actually publishes |

**Ranked losses** (worst first):
1. **Gateway heartbeats** — lost forever under the current bridge plan; costs gateway
   liveness/reboot detection. Needs an explicit bridge→gateway-upsert path.
2. **`pkt_sn` analysis** — number is storable in `raw_payload`, but loss-rate/reboot signals
   evaporate unless the bridge computes them at ingest time.
3. **`gw_addr`→`ble_mac`** — storable in `raw_payload`; also feed `herd_signal_gateways.ble_mac`.
4. Envelope bookkeeping (`flags`, `report_type`, `pkt_total`, `pkt_index`, `dev_total`,
   `dev_num`) — cheap to keep in `raw_payload`; their real value is as ingest assertions.
5. Foreign rows — accepted discard; unrecoverable by design.

## 8. Multi-gateway caveat (unchanged from CSV-era audit)

One `gw_addr` in the entire capture. The dedup key (000196: `tenant_id, tag_id,
device_seen_at, motion_count`) still has no gateway component, so the moment a second gateway
hears the same advertisement in the same instant, the second copy is silently discarded and
per-gateway RSSI (the input any location estimate needs) is destroyed. Latent, not active.
