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
  farm-number alone.

delivery-date eligibility:
  evaluates health, dead/sold status, quarantine, min age, and treatment
  withdrawal against delivery date, not the current machine date.

double-booking:
  enforces one canonical goat -> one active promise inside the demo process.

trusted weight and pricing:
  filters bad telemetry, uses trusted weight, applies rate/premium/surge, and
  shows evidence for ignored readings.

reconciliation:
  flags double bookings, unresolved references, ineligible promised goats,
  field-report deaths, active listings for bad goats, and feed exposure.

feed trace:
  uses movement history to determine whether a goat was in a contaminated
  feed/shed/date window and blocks booking/substitution when uncleared exposure
  makes the goat unsafe.

replacement:
  proposes substitute goats by breed, promised weight, eligibility, and
  availability.

promised-weight risk:
  checks whether the current/projected weight can still satisfy the customer
  promise instead of treating booked weight as static truth.

evidence:
  verdicts link back to source records instead of being unsupported claims.
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
  animal references that may be stale or ambiguous.

exchange_listings:
  listed/offered goats that may already be booked or ineligible.

health_ledger:
  death/sold/quarantine/treatment/withdrawal/clearance events used for
  delivery-day eligibility.

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
  movements, treatments, weights, or births not present in structured systems.
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
  death detection was regex/thin, not full multilingual evidence processing.

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
  state as source-backed facts.

Phase 5:
  movement/feed/weight history and trusted-weight policy.

Phase 8:
  Postgres-enforced allocation uniqueness on the live survivor goat_id.
  delivery-date eligibility and readiness policy.
  feed-contamination clearance as a sale/allocation and substitute blocker.
  promised-weight fulfillment risk.
  booking-date price audit.
  replacement/substitution must re-run full readiness.
  continuous promise-monitoring sweeper over open bookings until dispatch/exit.

Phase 10:
  governed dashboards for promise risk, reconciliation backlog, weight risk,
  price-audit failures, feed-exposure risk, replacement availability, and
  customer-safe views.
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
