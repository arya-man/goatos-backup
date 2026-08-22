---
name: goatos-herd-signals
description: Use when building, reviewing, or testing the Herd Signals module (HoneyComm BLE ear-tag telemetry) — backend/internal/herdsignals/**, apps/admin-web/features/herd-signals/**, the herd-signals slice of contracts/openapi/app-api.yaml, mock/herd-signals-mock.html, or docs/modules/herd-signals.md. Encodes the device field set, the Direct/Derived/Correlated/Inferred signal taxonomy, the movement-delta rules (cumulative counter, reset-to-zero, p75 baseline — NOT median), like-grain comparison, the smart-tag-lives-on-identifier rule, and the product claim boundary. Machine gate — make herd-signals-language-guard.
---

# Herd Signals

Canonical contract: `docs/modules/herd-signals.md`. If code, a doc, or your
plan disagrees with it, the contract wins. Every threshold/formula in that
doc is explicitly provisional (Section 10) — do not cite a number from it as
validated product truth, but DO treat every rule of *shape* (taxonomy, reset
handling, baseline statistic, mapping ownership, grain-matching) as binding.

## What the tag can physically report (Section 1)

Exhaustive. Nothing else exists upstream of these fields:

- Tag ID, BLE MAC address
- RSSI (per packet, per receiving gateway/phone — not a distance)
- Battery voltage (raw mV, not a percentage — no confirmed discharge curve)
- **Tag temperature** — the tag housing's own sensor. Never "body
  temperature"; the tag has no animal-contact sensor.
- Cumulative `motion_count` — a monotonically increasing accelerometer-event
  counter, incremented by firmware. Not steps, not chews, not any named
  behavior.
- Sensor status bits (temperature sensor OK/fault, accelerometer OK/fault)
- Gateway ID / receiver source (fixed gateway or `phone-scan`)
- Packet received time (server/gateway receipt time, not tag-side event time)
- Raw BLE advertisement payload (retained for re-derivation)

No GPS, no accelerometer waveform, no gyroscope, no rumination sensor, no
feed-proximity sensor, no direct animal-temperature sensor. If a design
needs any of those, it needs new hardware, not a cleverer formula over these
fields.

## The four-tier signal taxonomy (Section 2)

Every derived signal must be labeled with exactly one of these, because the
label tells the reader how much inference sits between the radio packet and
the sentence shown on screen:

| Tier | Meaning | Examples |
|---|---|---|
| **Direct** | As reported by the tag, no transformation beyond arithmetic on one field | motion-count delta, RSSI, battery voltage, tag temperature |
| **Derived** | A threshold/bucket/ratio computed from one or more Direct fields, still about the radio/hardware, not the animal | movement trend bucket, stale/not-seen, weak signal, low battery, sensor abnormal, gateway coverage |
| **Correlated** | Two independently generated event streams placed side by side in time, presented for a human to interpret — never asserted as causal | possible tag removed (weak suspicion only), feed-response correlation, post-vaccination/treatment activity watch, health-case activity trend |
| **Inferred** | A mapping/roster-gap conclusion, not a behavioral one | unmapped smart tags |

A new signal that doesn't fit one of the 12 insights in Section 8 does not
exist in Herd Signals v1. Do not invent a 13th without updating the doc and
this skill together — see "Future calibration needs" (Section 10) for what a
new signal needs before it graduates from idea to shipped.

## Movement rules — the part that actually shipped a bug once

- `motion_count` is **cumulative**, never a per-interval count on its own.
  `delta = current_motion_count - previous_motion_count` over the
  observation window.
- **Counter reset:** if `current_motion_count < previous_motion_count` (tag
  rebooted, counter wrapped), the delta for that interval is **0**, never
  negative. Never try to guess a "reset-adjusted" delta.
- **No-movement vs missing-signal are different facts and must never
  collapse into one state:**
  - No movement = a packet WAS received and its delta is 0. The tag is
    alive and reporting; the counter simply didn't advance.
  - Missing/stale signal = no packet has been received within the stale
    threshold. The tag might be dead, removed, out of range, or the animal
    might be fine with a lost link — the signal cannot distinguish these,
    and copy must say "missing signal" / "stale," never "missing animal."
  - A gap (`packet_count == 0` for a bucket) and a zero delta are the same
    trap in a different shape: a gap bucket means NO DATA, a zero-delta
    bucket means DATA THAT SAYS "no motion." Averaging/baselining code that
    treats a gap as a zero reading silently drags every derived threshold
    down. Exclude gap buckets from any baseline or trend computation instead
    of zero-filling them.
- **Movement-trend buckets** (provisional thresholds, Section 5): quiet
  watch and inactive/no-movement are DISTINCT states from a spike; a spike
  is a delta far above that tag's own recent baseline, not a fixed global
  number.
- **quiet_watch / inactive / spike / recovered** describe a state
  *transition* over a watch window, not a single reading: quiet_watch and
  inactive both come from a "quiet"/"no movement" bucket persisting past a
  configurable watch window (Insight 3, Section 8); spike comes from a
  delta exceeding a multiple of the animal's own p75 baseline (Insight 4);
  recovered is the transition back out of a watch/spike state once readings
  return to the animal's normal band. None of these are single-sample
  facts — a lone quiet bucket is not a "quiet watch," it has to persist.

## Baseline is p75 of non-gap buckets over 24h — NOT the median

This is a design bug that actually shipped in the review mock and was
caught: a resting animal's MEDIAN bucket is 0 (animals rest for large
fractions of a day), so a median-based "compare current to baseline"
comparison fires constantly — every reading above the trivially-low median
reads as a false spike. The correct baseline statistic is the **p75
(75th-percentile) of non-gap buckets over a rolling 24h window**
(`backend/internal/herdsignals/domain/motion.go`, `percentile75`). p75 sits
above the long resting stretches and only trips on genuinely elevated
activity. Gap buckets (no packet received) are excluded from the baseline
computation entirely — see the gap-vs-zero-delta rule above; folding gaps in
as zeros would corrupt the p75 the same way it would corrupt a mean.

If you are implementing or reviewing ANY baseline/threshold comparison in
this module and it uses `median(...)` or an unweighted mean over buckets
that can be gaps, stop and check against `percentile75` in
`backend/internal/herdsignals/domain/motion.go` before shipping it.

## Like-grain comparison

Never compare a value on one time grain against a threshold computed on
another without normalizing first. A 15-minute bucket's delta is not
directly comparable to a 5-minute bucket's threshold — you'd need `3x` the
5-minute threshold (or bucket both to the same grain before comparing). This
applies anywhere a spike/quiet/stale threshold is evaluated against a
window whose length can vary (variable poll interval, gateway vs.
phone-scan cadence, backfilled vs. live buckets).

## Smart-tag capability lives on the identifier, never on the goat

`smart_tag_capable` is a column on `goat_identifiers`, never on `goats`. An
animal can carry several identifiers over its lifetime (RFID, ear tags,
replacement tags, a BLE tag alongside a non-BLE one) — "is this specific
physical tag BLE-capable" is a property of ONE identifier row, not of the
animal. Putting the flag on the goat record makes it ambiguous which of the
animal's several identifiers is the smart one, and breaks the moment a tag
is replaced.

**Mapping rule** (Section 6): a tag is mapped to an animal when a
`goat_identifiers` row exists where `normalized_value` matches the incoming
`tag_id` OR `tag_mac`, the tenant matches, `status = 'active'`, AND
`smart_tag_capable = true`. An unmapped broadcast is surfaced as an
**unmapped smart tag** (Insight 12), never silently dropped, never treated
as an error. Two active rows matching the same tag/tenant is a **conflict**
that must be surfaced for operator resolution — never silently resolved by
picking one.

## KPI summaries are whole-filter server-side aggregates — never page sums

Any headline count/ratio (Tags Live Now, Shed Signal Coverage, Battery
Attention, etc. — the 12 insights in Section 8) must be computed by the
server over the FULL filtered result set, never by summing whatever rows
happen to be on the currently-rendered page. A paginated admin-web list
showing "20 of 2,000 tags" must not derive its headline "Weak Signal Tags:
3" by counting only the visible 20 — that's a page sum, and it silently
changes the number as the operator paginates or changes page size. Compute
the aggregate query against the same WHERE clause as the list, independent
of `LIMIT`/`OFFSET`.

## The language boundary (Section 3/4) — this is what the guard enforces

Herd Signals must never claim to detect, classify, or diagnose: eating,
rumination, sitting, standing, lying, walking, running, fever, body
temperature, disease, or any other named behavior/clinical classification.
A single cumulative motion-event counter plus RSSI cannot be decomposed into
distinct behaviors without a calibrated model trained on waveform data this
hardware does not expose.

**Forbidden vocabulary** (as a positive claim, not inside a denial): eating,
rumination, sitting/standing/lying/walking/running, fever, "body
temperature" (as a label — the correct field name is always "tag
temperature"), disease, diagnosis.

**Approved vocabulary** (Section 4): movement trend, motion-count delta,
activity delta, active/quiet/no movement, stale/not seen, weak signal, low
battery, sensor abnormal, gateway coverage, possible tag removed (weak
suspicion only), feed-response correlation (never "eating detection"),
post-vaccination/post-treatment activity watch (never "diagnosis"). Do not
invent synonyms that imply more certainty than the signal's tier supports
(e.g. never rename "active/quiet/no movement" to "grazing/resting/sleeping"
— that's a paraphrase around the same banned claim).

**Required UI-state hygiene:** no state switcher, and no "mock"/"demo"/
"sample"/"synthetic" wording, may appear in the shipped UI (the review mock's
`#empty`/`#filtered`/`#error`/`#loading`/`#offline`/`#data` hash states and
`Alt+1..6` are reviewer-only scaffolding, never shipped). Seed data may be
generated at realistic scale; the shipped product must never say so.

**Machine gate:** `make herd-signals-language-guard`
(`tools/agent-hooks/check-herd-signals-language.mjs`) scans
`backend/internal/herdsignals/**`, `apps/admin-web/features/herd-signals/**`,
the herd-signals slice of `contracts/openapi/app-api.yaml`, and
`mock/herd-signals-mock.html` for claim-shaped banned-term usage, "body
temp[erature]" mislabeling, and mock/demo/sample/synthetic UI copy. It
matches claim-shaped usage (asserting a behavior), not the bare word — the
same words used to DENY a capability, or listed inside a documented
banned-terms table, are allowed and self-tested as such. It cannot catch
paraphrase (e.g. "grazing" instead of "eating") or claims assembled across
variables/templates — see the guard's own header comment for its full,
explicit blind-spot list. Review still has to read for paraphrase; the
guard only catches the literal terms.

**The lesson underneath this guard's three review rounds, worth keeping in
mind for any future edit to it:** every real defect found was never about
the banned-word list — it was about the SCOPE the negation is judged in.
Round 1 judged negation per physical LINE, and failed the product's own
mandatory disclaimer the moment JSX wrapped it across two lines. Round 2
widened that to a flat N-line WINDOW, which fixed the wrap but then let an
unrelated "no"/"not" in a fully-unrelated, already-ended PRIOR sentence
silently suppress a genuine claim just by being nearby. Round 3 replaced
the line/window scope with a SENTENCE scope (walk outward until a real
sentence boundary — punctuation+capital, blank line, JSX tag edge, list
item — including boundaries that fall mid-line, not just at line ends). If
this guard grows a fourth defect, look first at whether the negation-scope
boundary logic in `buildSentenceWindow`/`splitIntoSentenceFragments` is
wrong for some new shape of prose, before touching the banned-term list —
that is where every prior bug actually lived.

## Escape hatch (used sparingly, must be complete)

`herd-signals-language:ignore: owner=<name> issue=<url|id> scope=<why> expiry=<YYYY-MM-DD>`
on the offending line. All four fields are required or the guard ignores the
exception and still flags the line.
