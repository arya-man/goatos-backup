# Herd Signals — HoneyComm Gateway Control Plane (MQTT Command Topics)

**Status:** Investigation notes, 2026-08-22. Read-only reconnaissance — **no
command has been published to the gateway**. Any command proposal in this doc
is **NOT YET SENT** and requires explicit maintainer approval.

Companion docs: `herd-signals.md`, `herd-signals-gateway-payload.md`,
`herd-signals-system-design.md`.

---

## 1. The topics

Broker: local Mosquitto at `192.168.0.5:1883` (no TLS, no auth).
Gateway: HoneyComm BLE gateway, client id `gw-514060-local`, MAC
`f130d402dcb4`.

| Topic | Direction (from gateway's view) | Purpose |
|---|---|---|
| `GwData` | PUBLISH | Telemetry uplink: `scan_report` packets and `state` heartbeats. |
| `SrvData` | SUBSCRIBE | Unknown. Presumed server→gateway data/response channel. |
| `AlphaCmd` | SUBSCRIBE | Unknown. Name strongly suggests a server→gateway command channel. |

That the gateway subscribes to two topics means it has a command/downlink
plane. We do not know its payload schema.

## 2. OBSERVED (live capture, 2026-08-22, ~5 min wildcard + earlier 3 h capture)

- Subscribed to `SrvData`, `AlphaCmd`, and `#` on the broker.
- `#` showed **only `GwData` traffic** — no other topics exist on this broker.
- **No retained messages** on any topic (all messages arrived with retain=0;
  nothing was delivered instantly at subscribe time on `SrvData`/`AlphaCmd`).
- **Nothing publishes to `SrvData` or `AlphaCmd`** — zero messages in the
  observation window. The gateway only subscribes there; the command plane is
  currently silent because no server-side component exists.
- `GwData` carries two `pkt_type` values:
  - `scan_report` (~1/sec): `flags`, `pkt_sn` (monotonic sequence), `report_type:"adv_only"`, `pkt_total`/`pkt_index` (multi-part reassembly support), `dev_total`/`dev_num`, and `dev_infos[]` with `addr`, `rssi`, per-device `time`/`msec`, `name`, `adv_raw`.
  - `state` heartbeat (~every 60 s): `{"pkt_type":"state", "gw_addr":"f130d402dcb4", "time":..., "data":{"msgId":56,"state":"sta_gw_hb","flags":0,"ticks_cnt":56}}` — `msgId` and `ticks_cnt` increment together, i.e. a heartbeat counter since boot/connect.
- **Clock offset re-confirmed**: broker receive time `2026-08-22T20:09:57+0530`
  vs gateway payload time `2026-08-22 22:39:57` — **+2 h 30 m exactly**,
  constant across captures.

Capture files (local scratch, maintainer's machine):
`/Users/ravi/goatos-work/herd-signals-proof/mqtt-cmd/wildcard.log`,
`cmd-topics.log`, and the earlier `/Users/ravi/goatos-work/herd-signals-proof/mqtt/capture.raw`.

## 3. INFERRED (from the report shape — not verified)

- The envelope discipline (`pkt_type` discriminator, `pkt_sn`,
  `pkt_total`/`pkt_index`, `flags`, `report_type`, named heartbeat states)
  is characteristic of a vendor-documented request/response protocol. A
  matching command schema very likely exists, probably JSON with its own
  `pkt_type`-style discriminator, addressed by `gw_addr`, sent on `AlphaCmd`
  with responses/acks returned on `GwData` or `SrvData`.
- `report_type:"adv_only"` implies other report types exist (e.g. connectable
  scans or filtered reports), which implies a configuration command to select
  them.
- **The +2:30 offset is exactly UTC+8 minus IST (UTC+5:30).** The most likely
  explanation is that the gateway's clock is set correctly in UTC (or NTP-
  synced) but its timezone is configured to UTC+8 (e.g. China Standard Time,
  a plausible factory default for this hardware). If so, the fix is a
  timezone setting, not a set-time command — and gateway UTC instants may
  actually be trustworthy once the fixed offset is removed. This remains an
  inference until the vendor confirms.
- Because nothing publishes on the downlink topics and no retained config
  exists, the gateway is running purely on its internal/factory
  configuration.

## 4. RESEARCHED (vendor documentation)

Web searches on the distinctive identifiers (`GwData` + `AlphaCmd` +
`scan_report`, `sta_gw_hb`, `adv_only` + `pkt_sn`, HoneyComm + `SrvData`,
`mTnA` + gateway) found **no credible public documentation** for this
protocol. Results were dominated by unrelated projects (OpenMQTTGateway,
Theengs, GL.iNet, Minew G1, ESP32 bridges) whose topic and payload schemas do
not match. Plainly: the HoneyComm command protocol is not publicly
documented, and nothing below is based on found documentation.

Therefore no command payload can be constructed safely today. The protocol
must come from the vendor.

## 5. Questions to put to the vendor

1. What is the command payload schema for `AlphaCmd`, and what is `SrvData`
   for? (Full downlink protocol document / SDK, including ack semantics.)
2. How do we set the gateway's **timezone** and/or clock? Is there an NTP
   server field? Our unit reports local time +2 h 30 m ahead of IST,
   consistent with a UTC+8 timezone default.
3. Can the scan report publish interval, `report_type`, and RSSI/name filters
   be configured over MQTT?
4. Can the MQTT broker address, port, credentials, and topic names be changed
   over the command plane — and is there any risk of a malformed command
   changing them unintentionally?
5. **Does the gateway buffer scan reports during a broker/WAN outage and
   replay them on reconnect, or are they dropped?** (Open question — our
   ingestion currently assumes NO offline buffering and models coverage gaps
   accordingly. `pkt_sn` continuity across a reconnect would be the
   observable signature of buffering.)
6. Is there a harmless read-only status/version query command we can use to
   verify the command plane without changing configuration?
7. Firmware version/update process, and whether config survives power cycles.

## 6. Command proposals — **NOT YET SENT**

None are safe to send today. We found no documentation, so any payload we
publish to `AlphaCmd`/`SrvData` would be a guess against unknown firmware,
with the worst case being a reconfigured gateway that stops publishing —
losing the live feed. Per the safety rule, nothing has been published.

Proposal for maintainer consideration (still recommend vendor-doc first):

- **Passive outage-buffering test (no publish at all):** briefly stop
  Mosquitto (or disconnect the gateway's WiFi) for 2–3 minutes, restart, and
  check whether `pkt_sn` is continuous across the gap and whether payload
  `time` values inside the first post-reconnect packets predate the
  reconnect. This answers the buffering question (Q5) without sending any
  command. Requires maintainer approval since it interrupts the live feed
  for the test window.

Any actual `AlphaCmd` publish will be written up here first with the exact
payload and expected effect, and sent only after maintainer approval.

## 7. The clock question — decision

What we need from the vendor, in order of preference:

1. A **timezone setting** (most likely root cause; see §3).
2. An **NTP server field** plus timezone, so the clock self-maintains.
3. A manual set-time command (least preferred — drifts, needs re-issuing).

**Fallback (current, and stays the default until the vendor answers):**
server/broker receive time is the source of truth for every ingested packet;
the gateway-reported time is stored **uncorrected** as diagnostic data only;
nothing keys ordering, staleness, dedup, or gap detection off gateway time.
This is already how ingestion is modeled, so a vendor fix is an improvement,
not a blocker.
