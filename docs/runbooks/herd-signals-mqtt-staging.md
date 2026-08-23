# Herd Signals MQTT Staging

This runbook documents the HoneyComm BLE gateway MQTT path for GoatOS staging.

## Current Hardware Payload

HoneyComm ear tags advertise BLE packets. The gateway forwards scan reports as
JSON. The useful direct fields are:

- gateway BLE address
- tag BLE MAC / tag id parsed from the HoneyComm advertisement
- RSSI
- gateway packet timestamp
- tag advertisement timestamp
- tag battery voltage
- tag temperature
- sensor status bits
- cumulative motion count

The tag does not expose raw accelerometer X/Y/Z values and does not directly
emit eating, rumination, sitting, standing, walking, or clinical-state flags.
Those must remain derived/inferred from motion-count deltas and other GoatOS
records.

## Local Test Path

Local broker:

```text
host: 192.168.0.5
port: 1883
protocol: MQTT
TLS: off
publish topic: GwData
subscribe topic: SrvData
client id: gw-514060-local
```

The gateway currently publishes live packets to local Mosquitto on topic
`GwData`. This proves the gateway can use MQTT. Local Mosquitto is only for
development; it is not the GoatOS backend.

## Staging Target Shape

Cloud Run is still the GoatOS app/API surface:

- admin web: `goatos-admin-web-stg`
- API: `goatos-api-stg`
- workers: existing Cloud Run worker services

The HoneyComm gateway speaks raw MQTT over TCP. Cloud Run cannot directly accept
a raw MQTT TCP listener on port `8883`, so staging needs an MQTT broker edge.

Current staging broker edge:

```text
project: goatos-stg
region: asia-south1
zone: asia-south1-a
instance: goatos-stg-herd-signals-mqtt-1
static IP: 8.234.104.45
firewall: allow-herd-signals-mqtts-stg
public port: 8883 only
broker: Mosquitto
TLS: enabled
gateway username: gw-514060
gateway password secret: herd-signals-mqtt-gateway-514060-password
CA secret: herd-signals-mqtt-ca-crt
server cert secret: herd-signals-mqtt-server-crt
server key secret: herd-signals-mqtt-server-key
```

Verified on 2026-08-22:

- VM is `RUNNING`.
- Mosquitto is `active`.
- VM is listening on `0.0.0.0:8883`.
- Local TLS/auth publish to `GwData` succeeded using the staged CA and
  `gw-514060` credentials.

Bootstrap script:

```text
infra/herd-signals/gcp-mqtt-broker-startup.sh
```

## Data Flow

The broker does not write GoatOS data by itself.

Expected staging flow:

```text
BLE tags
  -> HoneyComm gateway
  -> MQTT over TLS to 8.234.104.45:8883
  -> topic GwData
  -> Herd Signals bridge service
  -> goatos-stg PostgreSQL
  -> goatos-api-stg
  -> stg.dashboard.mesha.sg Herd Signals UI
```

The bridge service is the missing production code. It should subscribe to
`GwData`, validate and decode HoneyComm advertisements, and write normalized
packet/latest/window rows using the existing Herd Signals repository layer.

Until that bridge is deployed, `stg.dashboard.mesha.sg` will not show live
gateway data from the GCP broker. The broker can accept packets, but the
dashboard only sees data after the bridge persists rows into `goatos-stg`
PostgreSQL and the API exposes them.

Preferred bridge placement:

```text
Cloud Run service: goatos-herd-signals-bridge-stg
runtime: Go, same repository and deployment conventions as GoatOS backend
input: MQTT subscription to GwData on 8.234.104.45:8883
output: goatos-stg PostgreSQL writes through Herd Signals repository code
```

The bridge should be stateless and restart-safe. On restart it can continue from
new MQTT packets; raw packet history is stored in Postgres, not on the broker VM.

## Bridge Responsibilities

The bridge must:

- connect to the MQTT broker with a non-gateway credential
- subscribe to `GwData`
- parse gateway JSON scan reports
- keep `received_at` from the bridge/server clock as the trusted timestamp
- store gateway packet time separately as untrusted `gateway_seen_at`
- decode only HoneyComm `mTnA` / service `0xAB4C` advertisements
- reject or quarantine unknown gateway ids
- reject or quarantine unknown tag ids unless unmapped inventory is allowed
- dedupe packets by gateway id, tag MAC, gateway packet sequence, and raw payload
- store packet history for graphing
- update latest tag state
- compute motion deltas/windows server-side
- never claim direct behavior such as eating or rumination from these packets

## Bridge Implementation Contract

Use the existing Herd Signals ingest boundary. Do not create a second write path
that bypasses domain validation.

Existing backend target:

```text
package: backend/internal/herdsignals/app
method: Service.IngestPackets(ctx, actor, domain.IngestRequest)
HTTP equivalent: POST /herd-signals/packets
```

Bridge runtime config:

```text
HERD_SIGNALS_MQTT_HOST=8.234.104.45
HERD_SIGNALS_MQTT_PORT=8883
HERD_SIGNALS_MQTT_TOPIC=GwData
HERD_SIGNALS_MQTT_USERNAME=gw-514060
HERD_SIGNALS_MQTT_PASSWORD_SECRET=herd-signals-mqtt-gateway-514060-password
HERD_SIGNALS_MQTT_CA_SECRET=herd-signals-mqtt-ca-crt
HERD_SIGNALS_TENANT_ID=<tenant id used by stg.dashboard.mesha.sg>
HERD_SIGNALS_ACTOR_ID=herd-signals-bridge
```

Keep the MQTT password secret as raw bytes without a trailing newline. Shell
tests using command substitution strip a newline, but Cloud Run env secrets
preserve it and Mosquitto will reject the password as unauthorized.

MQTT scan reports should be transformed into `domain.IngestRequest`:

```text
GatewayID: gateway hardware BLE address from gw_addr, for this unit f130d402dcb4
GatewaySeen: bridge receive time in RFC3339 unless gateway time is proven correct
Packets[]:
  TagID: parsed HoneyComm tag id, for example A0003C
  TagMAC: parsed BLE MAC, for example F0:C9:90:A0:00:3C
  RSSI: RSSI from the gateway scan item
  Battery: parsed battery millivolts from the advertisement
  TagTemperature: parsed tag temperature in Celsius
  MotionCount: parsed cumulative motion counter
  SensorState: parsed sensor status bits
  TemperatureSensorOK: parsed from sensor status bits
  AccelerometerSensorOK: parsed from sensor status bits
  RawAdv: original advertisement hex
  SeenAt: bridge receive time in RFC3339
```

Timestamp rule:

```text
received_at / SeenAt: trusted server or bridge clock
gateway_seen_at / GatewaySeen: gateway clock only after timezone/NTP is verified
```

The gateway is currently reporting times using its own configured timezone. For
staging analytics, trust the Cloud Run bridge receive time first. Keep the
gateway timestamp as diagnostic metadata until the hardware clock and timezone
are verified at the shed.

Database rule:

```text
broker accepts packet != database row exists
database row exists only after the bridge has decoded and called IngestPackets
```

The dashboard should never read MQTT directly. It reads the existing Herd
Signals API, which reads `goatos-stg` PostgreSQL.

Suggested first Cloud Run service:

```text
service: goatos-herd-signals-bridge-stg
mode: single subscriber process
min instances: 1 during hardware validation
max instances: 1 until MQTT duplicate handling is proven
egress: outbound TCP/TLS to 8.234.104.45:8883 and Cloud SQL/Postgres path used by the backend
```

Keep `max instances: 1` initially because MQTT shared-subscription semantics and
packet dedupe need to be explicit before scaling bridge replicas.

## Gateway Staging Settings

For staging, configure the physical gateway to:

```text
protocol: MQTT
host: 8.234.104.45
port: 8883
publish topic: GwData
subscribe topic: SrvData
client id: gw-514060
username: gw-514060
password: from Secret Manager secret herd-signals-mqtt-gateway-514060-password
QoS: 1 if the gateway accepts it; otherwise QoS 0
SSL/TLS: enabled
CA required: enabled
Verify: enabled if gateway accepts the CA/IP certificate
```

Upload the CA certificate from Secret Manager secret
`herd-signals-mqtt-ca-crt` into the gateway CA file field.

### Physical Gateway Recovery

The HoneyComm gateway local UI is normally reachable on the farm/site LAN at:

```text
http://192.168.0.9/network_configurations.shtml
```

If the gateway is reset or falls back to its own access point, connect to the
gateway Wi-Fi SSID `GW_*******` with password `66668888`, then open:

```text
http://10.10.10.254
```

The gateway login password is `admin`.

On 2026-08-23 the gateway was still configured for the old local UDP target:

```text
protocol: UDP
host: 192.168.0.5
port: 7628
```

That mode sends packets only to the local listener and will leave staging Herd
Signals at zero even when the gateway is powered and scanning tags.

The staging MQTT settings applied on 2026-08-23 were:

```text
protocol: MQTT
host: 8.234.104.45
port: 8883
publish topic: GwData
subscribe topic: SrvData
client id: gw-514060
username: gw-514060
password: from Secret Manager secret herd-signals-mqtt-gateway-514060-password
QoS: 1
SSL/TLS: enabled
CA needed: off temporarily
Verify: off temporarily
data mode: Json
```

Do not paste the MQTT password into this runbook. Read it from Secret Manager in
project `goatos-stg`.

The gateway UI stores server settings separately from activation. The apply
sequence that worked was:

```text
1. Set Server Settings
2. Activate Server Settings
3. Save ALL
4. Reboot
```

After reboot, verify broker-side connectivity from the MQTT VM:

```bash
gcloud compute ssh goatos-stg-herd-signals-mqtt-1 \
  --project=goatos-stg \
  --zone=asia-south1-a \
  --command='sudo journalctl -u mosquitto --since "10 minutes ago" --no-pager | tail -120'
```

Expected connection evidence looks like:

```text
New client connected ... as gw-514060 (... u'gw-514060')
```

Do not treat the dashboard's zero packet count as a gateway-configuration
failure until the Cloud Run MQTT bridge is deployed and subscribed to `GwData`.
The broker can accept the gateway before the bridge exists.

CA upload trap: the HoneyComm UI did not accept a zip containing only the raw
CA certificate. It rejected that upload with:

```text
[CA]ca_infos.json not found!
```

The temporary staging validation mode is therefore encrypted MQTT/TLS with
broker certificate verification disabled:

```text
SSL/TLS: enabled
CA needed: off
Verify: off
```

Follow-up: build the vendor-format CA zip expected by the HoneyComm UI,
including `ca_infos.json`, then switch the gateway back to:

```text
CA needed: on
Verify: on
```

## Local Plus GCP At Same Time

The gateway UI exposes one Application Server target. Do not assume the gateway
can publish to both local and GCP at the same time.

For dual delivery, use one of these:

- gateway -> local broker -> local bridge forwards to GCP broker
- gateway -> GCP broker -> local developer subscribes remotely for debugging

Do not configure two independent sources of truth for staging packets.

Recommended during development:

```text
gateway -> local broker -> local bridge -> local/OCI dev DB
```

Recommended during staging hardware validation:

```text
gateway -> GCP broker -> Cloud Run bridge -> goatos-stg DB
```

## Security Boundary

MQTT over TLS protects packets in transit and authenticates the broker
certificate when CA verification is enabled. Username/password identifies the
gateway to the broker. This is stronger than UDP JSON, but it does not prove the
BLE tag itself is cryptographically authentic.

Backend validation still matters:

- known gateway allowlist
- known tag allowlist or explicit unmapped state
- plausible voltage/temperature/motion-count ranges
- monotonic motion-count checks per tag
- replay/dedupe checks
- audit/quarantine rows for rejected packets

## Cost / Operations

The MQTT broker VM is staging infrastructure, not OCI dev infrastructure. Keep
it small and replaceable. If Cloud Run-compatible MQTT ingestion is introduced
later through a managed broker or load-balanced TCP service, this VM can be
removed after the gateway is re-pointed and packet parity is verified.

Current cost expectation for the staging broker edge in `asia-south1`:

- `e2-micro` VM running 24/7 is expected to be roughly low double-digit USD per
  month in Mumbai pricing.
- Static external IPv4 is separately billable while reserved/attached.
- 10 GB boot disk is small but billable.
- Treat the total as approximately USD 12-17/month before tax/egress until
  billing export shows real numbers.

This VM is not covered by the US-only free-tier `e2-micro` allowance because it
runs in `asia-south1`.
