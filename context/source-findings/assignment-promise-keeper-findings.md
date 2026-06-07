# Assignment Promise Keeper Findings

Status: source finding for future Phase 8/P3/P5 implementation.

This summarizes the local candidate assignment solution and what Goat OS should
carry forward. It is not production architecture by itself. The assignment was a
snapshot demo over small local files; Goat OS must implement the same safety
logic as durable, proof-backed, event-driven, million-scale workflows.

## What The Assignment Was About

The assignment asks whether a goat promised to a customer or festival can really
be delivered safely on the delivery date.

In farm language, it checks:

```text
Is this the correct physical goat?
Is it already promised to someone else?
Will it be alive, eligible, outside quarantine/withdrawal, and safe on delivery day?
Is the trusted weight and price defensible?
If the promised goat fails, is there a safe substitute?
Can every answer point back to source evidence?
```

## Candidate Solution Strengths

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
  feed/shed/date window.

replacement:
  proposes substitute goats by breed, promised weight, eligibility, and
  availability.

evidence:
  verdicts link back to source records instead of being unsupported claims.
```

## Source Data Shapes From The Assignment

These assignment sources are useful as Phase 8/P10 design fixtures and test
cases. They should not be imported as production data, but their semantics are
good acceptance-test material.

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

## Conscious Demo Cuts

These were acceptable for a take-home but must not ship in Goat OS:

```text
runtime state:
  in-memory bookings/replacements disappear on restart.

multi-instance concurrency:
  process-local locks do not protect multiple API instances.

feed gate:
  feed exposure was mainly a discrepancy flag; a contaminated goat could still
  enter a new booking or substitute path unless eligibility blocks it.

promised-weight risk:
  current/projected weight shortfall against the promise was documented but not
  fully implemented.

price audit:
  existing bookings were not audited against the rate/policy effective on
  booked_at.

field-message ingestion:
  death detection was regex/thin, not full multilingual evidence processing.
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

The assignment logic should be lifted as reference logic, but not its runtime
shape.

```text
assignment:
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

The key product difference is continuity: the assignment answers whether a
promise is safe at one snapshot; Goat OS must keep that promise safe as facts
change until dispatch or exit.
