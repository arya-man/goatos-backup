# Customer Promise Safety Findings

Status: source finding for future Phase 8/P3/P5 implementation.

This summarizes customer/festival promise-safety scenarios that Goat OS must
support. It is not production architecture by itself. Goat OS must implement
the same safety logic as durable, proof-backed, event-driven, million-scale
workflows.

Latest source review note:

```text
Feed exposure must be a booking/substitute gate, and promised-weight shortfall
must be a readiness risk. Keep both as Goat OS requirements.
The internal validator scenarios are acceptance cases for Goat OS future
Phase 3/5/8/10 work, not Phase 1 runtime behavior.
```

## What The Promise Safety Flow Must Answer

The flow asks whether a goat promised to a customer or festival can really be
delivered safely on the delivery date.

In farm language, it checks:

```text
Is this the correct physical goat?
Is it already promised to someone else?
Will it be alive, eligible, outside quarantine/withdrawal, and safe on delivery day?
Is the trusted weight and price defensible?
If the promised goat fails, is there a safe substitute?
Can every answer point back to source evidence?
```

## Decision Logic To Preserve

The solution is valuable as a reference for decision logic:

```text
identity resolution:
  resolves multiple IDs/tags into a canonical animal and avoids merging on weak
  farm-number alone. Retagged animals, duplicate records across systems,
  transferred animals, ambiguous references, and stale tag reads must become
  explicit resolution states instead of silent merges.

delivery-date eligibility:
  evaluates health, dead/sold status, quarantine, min age, and treatment
  withdrawal against delivery date, not the current machine date. Withdrawal
  boundary rules must be explicit and tested; if a rule says a withdrawal window
  ending on delivery day is still blocked, the eligibility engine must enforce
  that exact boundary.

double-booking:
  enforces one canonical goat -> one active promise. Existing duplicate promises,
  phantom/unresolved promise references, void promises that should not reserve,
  booked-dead goats, booked-ineligible goats, and ambiguous booked references
  all become reconciliation items with evidence.

trusted weight and pricing:
  filters bad telemetry, uses trusted weight, applies rate/premium/surge, and
  shows evidence for ignored readings. Double-occupancy weight spikes,
  implausible drops, stale readings, unresolved tag reads, missing trusted
  weight, and unit conversion issues must not flow into pricing or readiness as
  trusted facts.

reconciliation:
  flags double bookings, unresolved references, ineligible promised goats,
  field-report deaths/treatments, active listings for bad goats, listed phantom
  goats, and feed exposure.

feed trace:
  uses movement history to determine whether a goat was in a contaminated
  feed/shed/date window and blocks booking/substitution when uncleared exposure
  makes the goat unsafe. The implementation must trace historical movement, not
  only current shed: a goat now in a clean shed may still have been exposed, and
  a goat now in the bad shed may have entered after the contamination window.
  Exposure that has a valid clearance event before delivery should not remain a
  permanent blocker.

replacement:
  proposes substitute goats by breed, promised weight, eligibility, and
  availability. A replacement/substitute must pass the same readiness path as a
  fresh allocation.

promised-weight risk:
  checks whether the current/projected weight can still satisfy the customer
  promise instead of treating booked weight as static truth.

evidence:
  verdicts link back to source records instead of being unsupported claims.

field reports:
  unstructured reports can contain deaths, treatments, tag corrections, and
  low-confidence ambiguous notes that are missing from structured systems. Death
  and treatment facts can affect promise safety, but ambiguous/low-confidence
  notes route to review and must not silently become canonical truth.

lineage:
  impossible parentage is a lower-priority genetics/data-quality warning, but it
  must be preserved for Phase 6/P10 genetics and audit workflows.
```

## Acceptance Cases Future Phases Must Not Fail

These are implementation acceptance cases for the future promise-safety slice.
They are intentionally phrased as behavior, not source-file instructions.

```text
identity:
  retagged goat -> old tag telemetry/report evidence resolves to the current
  canonical goat with confidence/evidence.
  duplicate record across systems -> linked only by strong evidence, not by weak
  farm-number alone.
  ambiguous farm/tag reference -> needs_review; never silent merge.
  transferred goat -> movement history follows the goat across farms/sheds.

booking/allocation:
  one live goat cannot have two active promises.
  phantom/unresolved promise reference does not reserve a goat and is surfaced.
  void/cancelled promise status does not reserve inventory.
  booked goat that is dead, ineligible, ambiguous, or unresolved becomes a
  high-risk reconciliation item.

eligibility:
  delivery-date rules use the configured delivery/event date, not machine clock.
  medicine withdrawal, quarantine, condition resolution, min age, min/trusted
  weight, dead/sold state, and no-trusted-weight all contribute to readiness.
  exact boundary dates follow policy exactly, including inclusive/exclusive
  delivery-day withdrawal behavior.

listings:
  listed/offered goat already booked -> blocked/review.
  listed/offered goat ineligible -> blocked/review.
  listed/offered goat phantom/unresolved -> review.

telemetry/pricing:
  double-occupancy spikes and implausible drops are excluded from trusted weight.
  stale telemetry and unresolved tag reads cannot create trusted price/weight.
  ignored readings remain visible as evidence.
  existing promises can be audited against the pricing policy/rate at booked_at.

feed recall:
  exposure is based on movement-over-time through bad feed/shed/date windows.
  current-shed-only logic is wrong in both directions: it misses goats that left
  after exposure and falsely flags goats that entered after the window.
  uncleared exposure blocks fresh allocation and substitution.
  cleared exposure is no longer a sale blocker after the clearance event.

field reports:
  death only in a field report can block an otherwise active structured goat.
  treatment/withdrawal only in a field report can affect delivery eligibility.
  tag-correction reports are identity evidence, not automatic truth.
  ambiguous low-confidence reports route to human review.

lineage:
  impossible parentage remains a genetics/data-quality finding, not a Phase 8
  sale blocker by itself.
```

## Source Data Shapes To Preserve As Test Fixtures

These source shapes are useful as Phase 8/P10 design fixtures and test cases.
They should not be imported as production data, but their semantics are good
acceptance-test material.

```text
goatos_animals:
  canonical animal rows, RFID/tags, status, age, breed, sex, current farm/shed.

legacy_cpt_records:
  older CPT records, day-first dates, pounds-to-kg conversion, legacy identity
  joins and ambiguity cases.

festival_bookings:
  existing customer promises, booked_on date, promised weight, status, and
  animal references that may be stale, ambiguous, phantom, duplicated, or voided.

exchange_listings:
  listed/offered goats that may already be booked, ineligible, or unresolved.

health_ledger:
  death/sold/quarantine/treatment/withdrawal/clearance events used for
  delivery-day eligibility and feed-contamination clearance.

goatsense_telemetry:
  noisy weight readings, including bad bridge-scale readings that must be
  filtered before pricing.

pricing_inputs:
  breed rates, premiums, and festival surge inputs effective by date.

animal_movements:
  farm/shed movement history used to reconstruct location on a past date.

feed_shed_log:
  feed batch, shed, date, and contamination window source for feed exposure.

breeding_ledger:
  dam/sire/kid lineage and family-history context for customer views and
  genetics.

procurement_batches:
  source/procurement context for goat origin and cost lineage.

field_reports:
  unstructured/mixed-language operational notes that may contain deaths,
  movements, treatments, tag corrections, weights, births, or ambiguous
  low-confidence notes not present in structured systems.
```

## Production Gaps To Avoid

These must not ship in Goat OS:

```text
runtime state:
  in-memory bookings/replacements disappear on restart.

multi-instance concurrency:
  process-local locks do not protect multiple API instances.

feed gate:
  feed exposure must be a booking/substitute gate, not only a discrepancy flag.
  Goat OS should keep the stricter behavior.

promised-weight risk:
  current/projected weight shortfall must be enforced as a readiness risk.

price audit:
  existing bookings were not audited against the rate/policy effective on
  booked_at.

field-message ingestion:
  death/treatment/tag-correction detection was thin, not full multilingual
  evidence processing.

runtime cache shape:
  feed-sale blocking must not use one global context-free cache for safety
  decisions. Goat OS should compute from tenant/policy-scoped facts or use
  invalidated/projection-backed caches.
```

## Goat OS Requirements Carried Forward

These requirements are now part of Goat OS planning:

```text
Phase 1:
  stable goat_id, identifiers, no-silent-merge, merge redirects, evidence,
  decision records, audit log, source_record_ids, location history.

Phase 3:
  health, quarantine, treatment, medicine withdrawal, death, feed-clearance
  state as source-backed facts, including field-report-only death/treatment
  facts routed through proof/review before canonical state changes.

Phase 5:
  movement/feed/weight history, historical feed exposure trace, and trusted
  weight policy that excludes spikes, stale readings, unresolved tags, and
  missing-trust cases.

Phase 8:
  Postgres-enforced allocation uniqueness on the live survivor goat_id.
  delivery-date eligibility and readiness policy.
  feed-contamination clearance as a sale/allocation and substitute blocker.
  promised-weight fulfillment risk.
  phantom/ambiguous/void promise reconciliation.
  exchange/listing availability computed from truth, not copied status.
  booking-date price audit.
  replacement/substitution must re-run full readiness.
  continuous promise-monitoring sweeper over open bookings until dispatch/exit.

Phase 10:
  governed dashboards for promise risk, reconciliation backlog, weight risk,
  price-audit failures, feed-exposure risk, replacement availability, and
  customer-safe views. Lineage/parentage and genetics data-quality warnings roll
  up here and into Phase 6 genetics.
```

## Scale Difference

The decision logic should be lifted as reference logic, but not its runtime
shape.

```text
static demo:
  small local files, in-memory state, one Node process, no durable event stream.

Goat OS:
  Postgres constraints and transactions
  idempotency keys
  outbox/Pub/Sub events
  partitioned event/audit/movement histories
  proof-gated canonical changes
  scheduled/event-triggered re-evaluation sweepers
  Cube-governed analytics
  1 lakh to 1 million goat scale without full-herd request scans
```

The key product difference is continuity: static review answers whether a
promise is safe at one snapshot; Goat OS must keep that promise safe as facts
change until dispatch or exit.
