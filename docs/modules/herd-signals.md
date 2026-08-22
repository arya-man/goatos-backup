# Herd Signals — HoneyComm BLE Ear-Tag Telemetry

**Status:** Draft v1 — documentation only, no implementation committed by this
doc. **Date:** 2026-08-22.

> **Every threshold, formula, and numeric constant in this document is
> provisional and unvalidated.** Nothing here has been confirmed against
> vendor specs or observed ground truth. Treat every number as a starting
> configuration value, not a fact about the hardware or the animal.

> **Companion document — system design and scalability:**
> [`herd-signals-system-design.md`](./herd-signals-system-design.md) holds the
> serving shape rather than the product boundary: the derived load model
> (packets/s, rows/day, bytes/day at 5k–50k tags), storage tiering and the
> partitioning/retention decisions, per-endpoint read budgets against the
> repo's sub-500ms p95 policy, polling fan-out cost, failure modes and
> backpressure, the scale-out ladder with its trigger metrics, and an explicit
> built-vs-designed-not-built ledger. This document stays the authority for
> what the module may claim; that one is the authority for how it is served.

## 0. What this module is

Herd Signals ingests BLE advertisement telemetry broadcast by HoneyComm
smart ear tags, received either by fixed BLE gateways in a shed/park or by an
operator's phone during a scan. It turns raw radio packets into
low-confidence herd-monitoring signals (movement trend, presence, battery,
signal quality). It is **not** a behavior classifier, activity recognizer, or
health diagnostic system. Section 3 states explicitly what it must never
claim.

---

## 1. Raw fields exposed by the tag/gateway

This is the exhaustive list of fields the HoneyComm tag and its BLE
advertisement/gateway pipeline expose. Nothing else exists upstream — any
field not listed here is not available from the hardware today.

| Field | Example | Notes |
|---|---|---|
| Tag ID | `A0003B` | Vendor-assigned short identifier printed/encoded on the tag. |
| BLE MAC address | `F0:C9:90:A0:00:3B` | The tag's Bluetooth LE advertiser MAC. |
| RSSI | `-62 dBm` | Received signal strength, reported per receiving gateway or phone, per packet. Not a distance measurement — affected by orientation, obstruction, antenna, and gateway hardware. |
| Battery voltage | `3100 mV` (`3.1 V`) | Raw cell voltage as reported by the tag firmware. |
| Tag temperature | `25.0 C` | Temperature measured at the tag's own sensor package. This is **ambient/device temperature**, not the animal's body temperature — see Section 3. |
| Cumulative motion count | `7470 -> 7485` | A monotonically increasing counter incremented by the tag's accelerometer/motion firmware. Cumulative since last reset/reboot, not a per-interval event count. |
| Sensor status bits | `temperature sensor OK`, `accelerometer OK` | Simple health/fault flags for the tag's own sensors, reported by firmware. Not a description of the animal. |
| Gateway ID / receiver source | e.g. `gateway-042`, or `phone-scan` | Identifies which BLE receiver captured the packet. Used for coverage and localization-by-proximity only. |
| Packet received time | ISO-8601 timestamp | Server/gateway receipt time of the packet, not a tag-side event time. |
| Raw BLE advertisement payload | raw bytes | The full advertisement frame as received, retained for parsing/debugging and future re-derivation if decoding rules change. |

Nothing else is transmitted. There is no GPS, no accelerometer waveform, no
gyroscope, no rumination sensor, no proximity-to-feed sensor, and no direct
temperature-of-animal sensor.

---

## 2. Signal classification

Every derived signal in this module must be labeled with one of these four
types so that consumers (admin-web, mobile, alerts) know how much inference
sits between the radio packet and the sentence shown to a human.

| Signal | Type | Formula | What it does NOT mean |
|---|---|---|---|
| Motion-count delta | Direct | `delta = current_motion_count - previous_motion_count` (see reset rule, Section 5) | Not a count of steps, chews, or any named behavior — it is an accelerometer-event counter increment. |
| RSSI (latest, average) | Direct | As reported per packet; average over a rolling window | Not a distance in meters; not a location. |
| Battery voltage | Direct | As reported | Not a battery percentage unless a vendor-confirmed curve exists (none confirmed yet). |
| Tag temperature | Direct | As reported | Not body temperature, not fever, not a clinical signal. |
| Movement trend (active/quiet/no movement) | Derived | Bucketed from motion-count delta over a fixed window against provisional thresholds (Section 5) | Not "the goat is eating/walking/resting" — it is a bucketed magnitude of an opaque counter. |
| Stale / not seen | Derived | `now - last_packet_received_time > stale_threshold` | Not "animal missing" — it is "no BLE packet has been received," which can also mean the tag battery died, the tag fell off, or no gateway is in range. |
| Weak signal | Derived | Latest RSSI or average RSSI below a threshold (Section 5) | Not "tag is far away" in any calibrated distance sense — only that link quality is poor at this receiver. |
| Low battery | Derived | Battery voltage below a threshold (Section 5) | Not a time-to-empty estimate — no discharge curve is confirmed. |
| Sensor abnormal | Derived | Sensor status bit reports fault | Not an animal health finding — a tag hardware self-check flag only. |
| Gateway coverage | Derived | Ratio/count of mapped tags with a recent packet at a shed, grouped by gateway | Not a claim about total herd presence — only about currently-mapped, currently-broadcasting tags. |
| Possible tag removed | Correlated (weak suspicion only) | Sudden, sustained motion-count delta of ~0 combined with continued packet reception (tag still broadcasting, i.e. not merely "stale") | Not a confirmed tag-removal event — an animal standing still, or a firmware/accelerometer fault, produces the same signature. Must always be presented as a weak suspicion requiring operator visual confirmation, never an automated conclusion. |
| Feed-response correlation | Correlated | Movement-trend change observed in a time window around a feed session, correlated per shed/park, not per animal causally proven | Not "the animal ate" or "the animal responded to feed" — a correlation between two independent event streams (feed timing, motion-count delta), nothing more. |
| Post-vaccination/post-treatment activity watch | Correlated | Movement-trend change observed in a time window after a recorded vaccination/treatment event, surfaced for operator awareness | Not a diagnosis, not a clinical outcome signal, not evidence of adverse or positive response — a correlation surfaced for a human to go look. |
| Health-case activity trend | Correlated | Movement-trend series plotted against open/closed health-case windows for an animal | Not a clinical trend, not "the animal is recovering/worsening" — a time-aligned display of two independently generated data series for a human reader to interpret. |
| Unmapped smart tags | Inferred (roster/mapping only) | Tags broadcasting with no `goat_identifiers` match (Section 6) | Not "extra animal" — could be an unassigned/spare/returned tag, a data-entry gap, or a tag on an animal outside this tenant/park scope. |

Any signal not in this table does not exist in Herd Signals v1. Do not infer
new derived signals (eating, rumination, sitting, standing, lying, walking,
running, fever, disease, or any behavior/health classification) from these
fields without a new, explicitly validated design — see Section 3 and
Section 10.

---

## 3. What we do not claim

The HoneyComm tag exposes exactly the fields in Section 1: a cumulative
accelerometer-event counter, RSSI, tag temperature, battery voltage, and
sensor-fault bits. From this raw stream, Herd Signals **does not** and
**must not** claim to detect, classify, or diagnose any of the following:

- Eating
- Rumination
- Sitting
- Standing
- Lying
- Walking
- Running
- Fever
- Body temperature
- Disease
- Any other named behavior or clinical classification

**Why:** a single cumulative motion-event counter cannot be decomposed into
distinct behaviors without a calibrated, validated classification model
(typically trained on labeled accelerometer waveform data, which this
hardware does not expose — only a running count, not the waveform). RSSI adds
proximity/link-quality information, not motion semantics. Combining "some
motion happened" (an opaque count) with "signal was strong or weak" (a radio
property) can never, by itself, separate eating from walking from standing
still and chewing — all of them can produce a similar range of accelerometer
events, and none of them are separable from the fields this tag reports.
Presenting any of the above as a system-generated finding would be a
fabricated capability.

**Temperature vocabulary rule:** the field is always **"tag temperature."**
Never use "body temperature," "animal temperature," or any phrasing implying
a clinical temperature reading. The sensor is on the tag housing, not
implanted or in contact-calibrated with the animal's core.

---

## 4. Approved product vocabulary

Only use these terms in UI copy, alerts, and API responses when describing
Herd Signals output. Each maps to a signal in Section 2.

- Movement trend
- Motion-count delta
- Activity delta
- Active / quiet / no movement
- Stale / not seen
- Weak signal
- Low battery
- Sensor abnormal
- Gateway coverage
- Possible tag removed *(weak suspicion only — never an assertion)*
- Feed-response correlation *(never "eating detection")*
- Post-vaccination / post-treatment activity watch *(never "diagnosis")*

Do not introduce synonyms that imply more certainty or more specific
behavior than the underlying signal type (Section 2) supports — e.g. do not
rename "active/quiet/no movement" to "grazing/resting/sleeping."

---

## 5. Movement-delta rules (provisional)

**All numbers in this section are provisional, unvalidated, and must be
configurable, not hardcoded as product truth.**

- `motion_count` is a cumulative counter maintained by the tag firmware.
- **Delta calculation:** `delta = current_motion_count - previous_motion_count`,
  computed over the polling/observation window in use.
- **Counter reset handling:** if `current_motion_count < previous_motion_count`
  (the tag rebooted or its counter wrapped/reset), the delta for that
  interval is **0**, never a negative number. Do not attempt to infer a
  "reset-adjusted" delta by guessing a wrap point.
- **Stale threshold:** a tag is considered stale / not seen if no packet has
  been received in the last **30 minutes** (provisional, configurable).
- **Display thresholds for movement trend** (provisional, configurable):
  - Moving: delta >= 100
  - Low: delta 10–99
  - Quiet: delta 1–9
  - No movement: delta = 0
- **Weak signal threshold** (provisional, configurable): latest RSSI <= -75
  dBm, or rolling average RSSI <= -80 dBm.
- **Low battery threshold** (provisional, configurable, placeholder pending
  vendor confirmation of the discharge curve): battery voltage < 2800 mV.

None of the above thresholds have been confirmed by HoneyComm or validated
against observed herd behavior. They exist only so an initial UI/alerting
implementation has a starting configuration. See Section 10.

---

## 6. Tag -> animal mapping

A BLE tag is mapped to an animal through the existing identifier system, not
through a new tag-owning field on the animal record.

**Mapping rule:** a tag is considered mapped to an animal when there exists a
`goat_identifiers` row where:

- `normalized_value` matches the incoming `tag_id` **or** `tag_mac` (either
  identifier form is an acceptable match key), **and**
- the identifier's tenant matches the current tenant (no cross-tenant
  matching), **and**
- `status = 'active'`, **and**
- `smart_tag_capable = true`.

**Why the flag lives on the identifier, not the goat:** an animal can carry
multiple identifiers over its lifetime (RFID tags, ear tags, replacement
tags after loss, BLE-capable tags alongside non-BLE tags). "Is this a
BLE/smart tag" is a property of the specific physical identifier object, not
a property of the animal. Putting `smart_tag_capable` on the goat record
would make it ambiguous which of the animal's several identifiers is the
smart one, and would break the moment a tag is replaced or a second tag is
added. Keeping the flag on `goat_identifiers` lets the existing
identifier-lifecycle rules (issue, retire, replace) apply unchanged to smart
tags.

**Unmapped state:** a tag broadcasting with no matching active,
`smart_tag_capable` identifier row is surfaced as an **unmapped smart tag**
(Section 2 / Section 8), not silently dropped and not treated as an error.

**Conflict state:** if more than one active `goat_identifiers` row for the
same tenant matches the same `tag_id`/`tag_mac`, this is a data conflict and
must be surfaced for operator/admin resolution rather than silently
resolved by picking one row. Do not guess which animal "really" owns the tag.

---

## 7. Admin UI polling / refresh behavior

- **Live Monitor:** polls every **5 seconds** by default; operator-selectable
  options are **5s / 15s / 60s**.
- **Polling pause:** polling stops while the browser tab is hidden
  (`document.visibilityState !== 'visible'`), and resumes on the next active
  interval once the tab is visible again, to avoid wasted requests against a
  backgrounded tab.
- **Stale banner:** shown when the last successful response is older than
  **30 seconds**, regardless of the selected polling interval — this is a
  UI freshness indicator, independent of the per-tag "stale / not seen"
  signal in Section 2/5, which is about tag packet freshness, not UI fetch
  freshness.
  - Do not conflate the two "stale" concepts in code or copy: UI staleness
    is about the admin page's own data fetch; signal staleness is about
    whether a given tag has recently reported a packet.
- **Manual refresh:** always available and always bypasses the polling
  interval and the pause-on-hidden-tab behavior.
- **Timeline charts:** refresh every **60 seconds**.
- **Gateway health view:** refreshes every **15 seconds**.

---

## 8. The 12 insights

Each insight below states its formula and its Section 2 type. "Type" here
follows the same Direct / Derived / Correlated / Inferred vocabulary as
Section 2.

1. **Tags Live Now** — Direct/Derived. Count of distinct mapped tags with a
   packet received within the stale threshold (Section 5).
2. **Missing Signal** — Derived. Count/list of mapped tags whose most recent
   packet is older than the stale threshold. **Must be labeled "missing
   signal," never "missing animal."** A missing signal means the tag has not
   been heard from — it does not mean the animal is missing, since the
   animal could be present with a dead/removed/out-of-range tag, or the
   animal could genuinely be absent; the signal alone cannot distinguish
   these.
3. **Low Movement Watch** — Derived. List of mapped tags whose recent
   movement-trend bucket (Section 5) has been "quiet" or "no movement" for
   longer than a configurable watch window.
4. **High Movement Spike** — Derived. List of mapped tags whose motion-count
   delta over the current interval exceeds a configurable spike threshold
   relative to that tag's own recent baseline.
5. **Shed Signal Coverage** — Derived. Ratio of mapped tags in a shed with a
   non-stale packet to total mapped tags expected in that shed, grouped by
   receiving gateway.
6. **Weak Signal Tags** — Derived. List of mapped tags whose latest or
   rolling-average RSSI is at or below the weak-signal threshold (Section 5).
7. **Battery Attention** — Derived. List of mapped tags whose battery
   voltage is below the low-battery threshold (Section 5), or whose sensor
   status bits report a fault.
8. **Post-Vaccination Movement Watch** — Correlated. Movement-trend series
   for an animal windowed around a recorded vaccination event timestamp,
   surfaced for operator review, not a clinical signal.
9. **Health Case Activity Trend** — Correlated. Movement-trend series for an
   animal plotted against the open/close window of a health case.
10. **Feed x Activity** — Correlated. Movement-trend change windowed around
    recorded feed-session timestamps for a shed, described as feed-response
    correlation, never "eating detection."
11. **Weight x Activity** — Correlated. Movement-trend series juxtaposed with
    recorded weighing events for an animal, for trend-reading by a human,
    not an automated inference.
12. **Unmapped Smart Tags** — Inferred (mapping gap). List of `smart_tag_capable`-eligible
    BLE broadcasts with no matching active `goat_identifiers` row
    (Section 6), for operator triage (assign, retire, or ignore).

---

## 9. Vaccination BLE proof flow (design notes — replaces RFID tap)

This section documents the intended design for using BLE ear-tag proximity
instead of an RFID tap to identify the animal in a vaccination proof flow.
This is a design note, not a shipped implementation.

**Flow:**

1. Operator opens a vaccination task on an animal/shed.
2. App starts a nearby BLE scan.
3. Detected tags are sorted by RSSI (strongest first).
4. Operator physically approaches the target animal's ear tag.
5. The app highlights the strongest **mapped** tag (Section 6) in the
   nearby-tags list.
6. Operator confirms the highlighted tag is correct.
7. Camera opens for proof capture.
8. Proof record stores: `animal_id`, BLE tag ID, BLE MAC, RSSI at
   confirmation, scanner source (gateway or phone), confirmation timestamp,
   and the video proof ID.

**Safety gate — never auto-open the camera on tag visibility alone.** The
camera must not open merely because a BLE tag is detected nearby. Camera
open requires one of:

- The strongest detected tag's RSSI is **>= -60 dBm** *and* has **>= 8 dB**
  separation from the next-strongest detected tag (both provisional,
  configurable thresholds — see Section 10), **or**
- Explicit manual operator confirmation of the target tag, overriding the
  automatic RSSI gate.

This mirrors the existing product rule that high-risk animal actions require
explicit operator confirmation, not silent automation (see
`docs/features/critical-animal-action-guardrails.md` for the general
guardrail pattern this flow must not bypass).

**UI states:**

- Nearest tag locked — strongest tag passes the RSSI/separation gate and is
  highlighted, awaiting operator confirmation.
- Multiple tags nearby — no single tag has sufficient separation; operator
  must move closer or manually select.
- Move closer — no detected tag meets the minimum RSSI floor.
- Tag not mapped — strongest nearby tag has no `goat_identifiers` match
  (Section 6); flow cannot proceed via BLE and must fall back to manual
  verification.
- Bluetooth permission required — device-level BLE scanning permission is
  missing; flow cannot start.
- Scanner unavailable — BLE hardware/scan session failed to start; use
  manual verification path.

In every state where BLE cannot confidently resolve the animal, the flow
must fall back to the existing manual verification path rather than blocking
the operator or guessing.

---

## 10. Future calibration needs

None of the thresholds or formulas in this document should be treated as
more than a placeholder starting configuration until the following are
addressed:

- **Vendor confirmation of the battery discharge curve** — voltage-to-percent
  and voltage-to-time-remaining are unconfirmed; the 2800 mV low-battery
  threshold (Section 5) is a guess.
- **Vendor confirmation of `motion_count` semantics** — sampling rate,
  sensitivity, what firmware event increments the counter, and whether
  behavior differs across firmware/hardware revisions.
- **Per-shed and per-animal baselines** — a fixed global movement-delta
  threshold (Section 5) ignores that different sheds (space, stocking
  density, animal age/species) and different animals will have different
  "normal" motion-count ranges.
- **Gateway placement and RSSI calibration** — RSSI-to-distance behavior
  depends on gateway antenna placement, shed construction, and interference;
  no calibration pass against measured distances has been done.
- **Packet loss characterization** — the stale threshold (Section 5) assumes
  a packet-loss rate that has not been measured against real gateway
  density and animal density in a shed.
- **Validation against observed ground truth** — every derived/correlated
  signal (Section 2) needs a validation pass against independently observed
  animal state before any threshold graduates from provisional to trusted,
  and before any UI copy changes from hedged ("watch", "trend", "possible")
  to asserted language.

Until this calibration work is done and documented, all thresholds in this
file remain provisional, must stay configurable (not hardcoded), and must
not be cited elsewhere as validated product behavior.

## Required UI states (implementation contract)

Every Herd Signals surface must implement all of the states below. They are
product states, not review scaffolding: the review mock exposes them through a
URL hash (`#empty`, `#filtered`, `#error`, `#loading`, `#offline`, `#data`) and
`Alt+1..6` only so a reviewer can reach them without a backend. **No state
switcher, and no "mock"/"demo"/"sample"/"synthetic" wording, may appear in the
shipped UI** — in the product each state is entered by real conditions.

| State | Condition | Required treatment |
|---|---|---|
| Loading | first read in flight, no cached rows | skeleton rows, never a blank panel |
| Empty — no packets | no gateway has ever posted for the tenant | "No gateway packets yet" + what to check on the gateway |
| Empty — no mapped tags | no active `smart_tag_capable` identifier | route the reader to Tag Mapping |
| Empty — no unmapped tags | every seen tag resolves to one identifier | stated as a healthy outcome, not an error |
| Empty — no alerts | every tag within thresholds | stated as a healthy outcome |
| Empty — no battery / no history | no readings or no activity windows in scope | explain that buckets are written as packets arrive |
| Filtered to nothing | filters/search exclude every row | offer "clear filters"; claim the gateway is still receiving only when tags_seen > 0 proves it |
| Read failed | API error | say the read failed, offer retry; never render an empty table as if it were zero rows |
| Stale | last successful response older than 30s | degraded state on the live control, stale banner, manual refresh still works |
| Paused | operator paused polling, or tab hidden | polling stops, state is visible on the live control |
| Gateway offline | gateway has not posted recently | banner on that gateway, and its tags read as missing signal — never as missing animals |

Two rules that fall out of the table and must not be relaxed:

- An empty result and a failed read are different states and must never share
  a rendering. Showing a failed read as "0 tags" reports a false fact.
- A missing signal is a statement about the radio path. Its copy names the
  gateway before it names the animal.

## Seed and scale data

Seed/demo environments may carry generated tag rows so search, filters, and
pagination are exercised at realistic scale (the review mock renders ~2,000
rows against a handful of proven readings). That is a property of the seed
data, recorded here and in the seed command — it is never surfaced in UI copy,
and no screen labels its own contents as generated.

---

## 11. Test database: Ravi's OCI dev Postgres clone

Herd Signals local feature testing runs against **Ravi's personal OCI
Compute VM PostgreSQL instance** — a template-copy clone of a `goatos-stg`
database snapshot, running on Oracle Cloud, reached over an SSH tunnel. It is
**never production**, and it is **never** the local `:5433`
`goatos-local-current` stack used for other Goat OS local dev — a
completely separate database on a separate host, used specifically so heavy
DB testing does not require a laptop Docker/Colima stack.

**Connection path:**

1. Open the SSH tunnel from the maintainer's laptop:
   `/Users/ravi/mesha/tools/local/oci-goatos-a1-dev.sh tunnel`
   This forwards local `127.0.0.1:15432` to the OCI VM's
   `127.0.0.1:5432` (Postgres running in a rootful Podman container on the
   VM, not exposed publicly).
2. Source the local-only credentials env file (never copied into this repo,
   `chmod 600`, maintainer's machine only):
   `/Users/ravi/mesha/local-data/goatos-stg-to-oci/oci-goatos-db.env`
3. The resulting `DATABASE_URL` is:
   `postgres://postgres:${REMOTE_POSTGRES_PASSWORD}@127.0.0.1:15432/goatos?sslmode=disable`

**Running the Herd Signals seed:**

```bash
# 1. tunnel (separate terminal, stays open)
/Users/ravi/mesha/tools/local/oci-goatos-a1-dev.sh tunnel

# 2. credentials
source /Users/ravi/mesha/local-data/goatos-stg-to-oci/oci-goatos-db.env

# 3. seed (from the goatos worktree) — the .sh wrapper runs the .sql body
#    and refuses to run against anything but the OCI tunnel target
bash tools/local/seed-herd-signals-oci.sh
```

The wrapper (`tools/local/seed-herd-signals-oci.sh`) refuses to run against
any database other than the OCI tunnel target — it is a hard-coded safety
check, not just a convention. `tools/local/seed-herd-signals-oci.sql` is the
actual seed body and is **idempotent**: gateway insert uses
`ON CONFLICT (tenant_id, gateway_id) DO NOTHING`, tag rows use
`ON CONFLICT (tenant_id, tag_id) DO UPDATE`, packet buckets use
`ON CONFLICT (tenant_id, tag_id, bucket_start, bucket_seconds) DO NOTHING`,
and identifier mapping rows use
`ON CONFLICT (tenant_id, normalized_value) DO NOTHING` — safe to re-run
without duplicating rows or erroring on a second pass.

**Real capture provenance:** the seed loads a genuine field capture, not
synthetic data: **11,144 BLE advertisement packets** from **20 distinct
HoneyComm ear tags**, all received by **one gateway**, BLE MAC
`f130d402dcb4`. Of the 20 captured tags, **19 are mapped** to goats in the
Castro shed by the seed; tag **`A0003B` is deliberately held back
unmapped** — it is the handheld unit used for manual/scan testing (see
Section 1, `gateway-id` = `phone-scan` for that path), and its absence from
`goat_identifiers` is intentional so the seed always exercises the unmapped
smart tag state (Section 2, Insight 12) against real data rather than a
synthetic gap.

---

## 12. API surface

Registered in `backend/internal/herdsignals/adapters/http/handler.go`
(`Register(mux, h)`). As of this writing, **four endpoints exist**; this
module's contract slice in `contracts/openapi/app-api.yaml` is still being
authored concurrently in this worktree by another agent, so treat the count
and shapes below as the current backend truth, not yet the finalized
contract, and re-check `handler.go` before citing a fifth endpoint or a
locked request/response shape.

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/herd-signals/packets` | Ingest raw BLE advertisement packets from a gateway or a phone scan (`IngestPackets`). |
| `GET` | `/herd-signals/live` | List current live/latest state per mapped tag — the Live Monitor read path (`ListLive`). |
| `GET` | `/herd-signals/tags/{tag_id}/timeline` | Per-tag timeline of packets/buckets for the timeline chart (`GetTimeline`). |
| `GET` | `/herd-signals/gateways` | Gateway health/coverage listing (`ListGateways`). |

Whichever agent lands the OpenAPI contract and any additional endpoint (for
example, an explicit unmapped-tags or insights/alerts route) should update
this table in the same change, and should register/scan any new route
through `check-herd-signals-language.mjs`'s herd-signals YAML slice
detection (Section 13) rather than leaving new contract text unguarded.

---

## 13. Machine gate and agent skill

- **Guard:** `make herd-signals-language-guard`
  (`tools/agent-hooks/check-herd-signals-language.mjs`) enforces this
  document's Section 3 claim boundary as an executable check — it fails the
  build on a claim-shaped use of a banned behavior/health term, a "body
  temp[erature]" mislabel, or mock/demo/sample/synthetic wording in the
  admin-web UI. It is registered in `tools/ci/guardrail-manifest.json`,
  wired into `make guardrails` and `tools/ci/run-local-ci.sh`, and ships
  adversarial fixtures (fail + pass cases) under
  `tools/agent-hooks/test-fixtures/herd-signals-language/`. Read the
  guard's own header comment for its documented blind spots (paraphrase,
  cross-line denial scope, non-literal/templated copy) before assuming a
  clean guard run means the copy is definitely safe — it is a floor, not a
  substitute for review.
- **Agent skill:** `.agents/skills/goatos-herd-signals/SKILL.md` teaches a
  future agent the device field set, the Direct/Derived/Correlated/Inferred
  taxonomy, the movement/reset/baseline rules (including the p75-not-median
  defect this design already caught once), the like-grain comparison rule,
  the smart-tag-on-identifier-not-goat rule, the whole-filter KPI-aggregate
  rule, and this same language boundary. It is listed in `SKILLS.md`'s
  "Lens / anti-pattern skills" table so it is discoverable the same way as
  the other Herd Signals-adjacent lenses.

## Unmapped tags are the NORMAL state, not a degraded one

BLE tags are commissioned and powered up before they are attached to animals.
On staging today **no tag is mapped to any animal**, and the module must be
fully useful in exactly that state. An unmapped tag is a first-class row, not
an error and not a placeholder.

**Renders for every tag, mapped or not** — all of it comes from the packet, so
none of it may be gated on an animal:

tag ID · BLE MAC · gateway · RSSI and signal state · battery mV and battery
state · tag temperature · cumulative motion_count · 15m and 1h motion delta ·
movement state · pattern state · last seen · sensor-OK bits · the full
movement-history timeline at every bucket tier · gateway coverage and health.

**Empty only when no animal is mapped** — these are genuinely animal-derived:

display id · park / shed / operational location · the four correlated insights
(post-vaccination movement watch, health-case activity trend, feed x activity,
weight x activity).

Rules that follow, and that tests must pin:

- KPI aggregates are packet-derived and **count unmapped tags**. `tags_seen`,
  `moving`, `quiet`, `not_moving`, `stale`, `weak_signal`, `low_battery` and
  `sensor_abnormal` must never sit behind a join that drops unmapped rows —
  a staging dashboard would read all zeros. `mapped_animals` is the only
  mapped-only count; `unmapped_tags` is its complement.
- The live query is **tag-first with LEFT JOINs** to `goat_identifiers` →
  `goats` → `locations`. An inner join anywhere on that path silently empties
  the screen on staging.
- Park and shed filters cannot match an unmapped tag, which is correct — but
  the DEFAULT must be "every tag", never "every tag that resolved".
- The correlated insight cards return an explicit empty result with a stated
  reason ("no mapped animals in scope"). Never an error, never a zero that
  reads as a measurement, never a card that disappears.
- Tag Mapping is where an unmapped tag becomes actionable. It is the intended
  destination for these rows, not a warning surface.

The regression test for this is: ingest packets for a tag with no matching
`goat_identifier`, then assert the live row carries every packet-derived field,
the summary counts it, and the timeline returns its buckets.

