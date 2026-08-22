# Herd Signals — HoneyComm BLE Ear-Tag Telemetry

**Status:** Draft v1 — documentation only, no implementation committed by this
doc. **Date:** 2026-08-22.

> **Every threshold, formula, and numeric constant in this document is
> provisional and unvalidated.** Nothing here has been confirmed against
> vendor specs or observed ground truth. Treat every number as a starting
> configuration value, not a fact about the hardware or the animal.

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
