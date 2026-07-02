# Calendar Ownership And Vaccination Scope

Status: Accepted
Date: 2026-06-27

## Context

Goat OS has many verticals, read models, command lenses, and workflow modules.
The Calendar must not become another dashboard or a copy of every table. It
answers one narrow question:

```text
Who must do what, when?
```

The backing product model is the protocol/obligation engine: published rules
materialize due work in Postgres, SOP tasks carry execution/proof, reminders are
derived from due work, and projections feed Control Tower / Passport / Calendar.
See `docs/protocol-engine/obligation-engine.md`.

Source anchors:

- `context/frontend/current-admin-web-scope.md` - fixed vertical/module/command
  taxonomy.
- `docs/preventive-care-vaccination/PRD.md` - Preventive Care (PC) owns Vaccination; Calendar is a module
  support surface, not a separate source of truth.
- `docs/preventive-care-vaccination/TRD.md` - Preventive Care (PC) includes feed/water testing and
  vaccination obligations come from source-backed published rules.
- `docs/protocol-engine/obligation-engine.md` - due truth lives in
  `obligation_instances`; Calendar reads Postgres/projections, not queues.
- `source-material/goatOS.docx` / `wiki/goatOS.docx` - legacy GoatOS
  vaccination flow: config -> per-goat schedule -> shed event -> completion ->
  stock, plus SOP proof and procurement/quarantine task patterns.
- Mesha wiki source files `Handbooks/Mesha-dept-directors.pdf` and
  `Handbooks/PHC_Director.pdf` - Preventive Care (PC) responsibility for vaccination calendar,
  cold chain, quarantine, feed/water testing, SOP video double verification,
  and stock anti-misuse.

## Decision

Calendar events are admitted only when all of these are true:

```text
due_at or a due window exists
owner or executable role exists
human action is required
```

Read-model data is not a Calendar event. Census totals, coverage percentages,
passport history, KPI cards, static tables, source records, and analytics
charts stay in module screens, Control Tower, Insights, or Goat Passport unless
they create a dated human action.

Pure system jobs are not Calendar events. A sweeper, replay, idempotency worker,
projection refresh, or promise-safety monitor should carry `system: true` and
be excluded from all Calendar pills. If a system job raises a human due action,
the derived human action becomes the Calendar event.

Reminder notifications are not separate Calendar events. They attach to the
same due work item. Calendar may display reminder state on an event, but must
not create a second event for the notification itself.

Director reporting cadence is cross-cutting. EOD/reporting items may use the
`HR / People` pill in v1 for workflow discipline, but must carry
`cross_cutting: true` so leadership/Control Tower views do not hide misses from
non-HR directors.

`owner_key`, `system`, and `cross_cutting` are Calendar-projection attributes.
They are not columns on `obligation_instances` today. Derive them when building
the Calendar read model from the executable owner/capability, protocol category,
job identity, and task metadata.

## Calendar Pills

Use the same owner taxonomy across the app and Calendar. Do not introduce
Calendar-only owner keys. Persist the stable `owner_key` in Calendar
projections; display labels can change.

| owner_key | Display | Calendar contents |
| --- | --- | --- |
| `all` | All | Combined scheduled work across all eligible pills. Filter-only; do not persist as an event owner. |
| `counts` | Counts / Identity | Scheduled-only identity/count work: tagging due, weekly/monthly weigh-ins, count verification, count reconciliation. Most census/passport data is not Calendar. |
| `phc` | Preventive Care (PC) | Vaccination, deworming, treatment sessions, ICU follow-ups, quarantine health checks, feed/water lab tests, post-mortem/health verification, Preventive Care (PC) biosecurity checks, SOP-video double verification, and Preventive Care (PC) stock anti-misuse investigation with a dated human action. |
| `feed` | Feed | Feed direction publish, cutoff Diff, packing, feeding sessions, feed distribution, feed-prep staging, bridge exceptions, feed execution checks. Lab QA is not Feed; see Preventive Care (PC). |
| `breeding` | Breeding | Estrus/heat windows, AI/natural breeding, pregnancy scan, kidding watch, colostrum, K1 bottle-feeding, lactation/milk sessions. Preventive Care (PC) sees only downstream health exceptions. |
| `parks` | Parks / Infra | Shed sanitation, deep clean, panel clean, shifting/movement, physical isolation moves, trough/water availability, infra inspection, ground execution proof. |
| `procurement` | Procurement | Source warmup, source health/pre-dispatch checks, dispatch readiness, loading/transit handoff, arrival gate, load follow-up, rejected-before-load, accepted-intake handoff. |
| `inventory` | Inventory / Stock | Operational stock work: cold-chain temperature checks, lot reservation/readiness, reorder deadlines, expected deliveries, expiry/FEFO checks, GRN, physical stock audit, lot quarantine from stock/cold-chain breach. |
| `sales_commerce` | Sales / Commerce | Reserved/provisional disabled-future key for deadline-only sales/allocation/dispatch/payment work. Do not emit live events until a Sales/Commerce slice/ADR activates it. |
| `farmer_network` | Farmer Network | Sowing, harvest, fodder pickup, crop status checks, crop/fodder expenditure review. |
| `hr_people` | HR / People | Attendance, roster publish, payroll/attendance reconciliation, absence/backfill, training, escalation review, EOD reporting with `cross_cutting: true` when it spans directors. |
| `admin_data_ops` | Admin / Data Ops | Source-review due, config-approval due, import/replay review, data-quality review, audit follow-up. Not raw config rows. |

## Owner Rule

Calendar uses the executor owner: the pill is the role/vertical that must act at
that time, not necessarily the vertical that owns the protocol or policy.

Examples:

- Sanitation: Preventive Care (PC) may own the biosecurity SOP, but the dated shed-clean task is
  Parks/Infra ground work, so the Calendar pill is `Parks / Infra`.
- Cold-chain temperature check: Preventive Care (PC) cares about vaccine integrity, but the
  inventory keeper runs the check, so the Calendar pill is `Inventory / Stock`.
- Vaccine stock discrepancy: the physical count or ledger correction belongs to
  `Inventory / Stock`; a Preventive Care (PC) anti-misuse investigation or penalty follow-up
  belongs to `Preventive Care (PC)` because the Preventive Care (PC) Director handbook makes that a Preventive Care (PC) control.
- Quarantine: vet-led health checks are `Preventive Care (PC)`; pure physical isolation moves are
  `Parks / Infra`.
- Colostrum and K1 bottle-feeding are `Breeding`; Preventive Care (PC) only takes the exception
  path if refusal triggers not-eating, ICU, or diagnosis work.

Exception: when source docs assign explicit responsibility and there is no
distinct non-owner executor, use the documented owner. Feed/water lab QA is the
important case: both weekly water lab tests and weekly feed lab tests are `Preventive Care (PC)`
because source docs put feed/water testing under Preventive Care (PC) responsibility. Feed owns
ration, direction, packing, distribution, consumption, and feed execution, not
lab safety ownership.

## Procurement Boundary

Accepted intake is the ownership transfer from Procurement to herd operations.

Before accepted intake:

```text
source warmup
pre-dispatch decision
reject-before-load
dispatch readiness
loading / transit / handoff
arrival gate
intake handoff
```

These are `procurement` Calendar events.

After accepted intake:

```text
vaccination or quarantine-health -> Preventive Care (PC)
physical isolation move          -> Parks / Infra
tagging or census                -> Counts / Identity
stock/cold-chain                 -> Inventory / Stock
```

There is no overlap window where the same dated event belongs to both
Procurement and Preventive Care (PC), Parks, and Counts.

## Vaccination Calendar Scope Now

The current implementation target is the Preventive Care (PC) Vaccination slice only. Calendar
should render only real due work emitted by the existing protocol/obligation/SOP
path. Do not hardcode PPR/FMD/HS/BQ or any vaccine column/event from labels.

Current Calendar-eligible vaccination events:

| Event family | owner_key | Source of truth | Notes |
| --- | --- | --- | --- |
| Published vaccination dose due | `phc` | `obligation_instances` generated from a `protocol_definitions(category='vaccination')` protocol with a published, source-backed `protocol_versions` row expanded through `protocol_rules` | Requires a published source-backed rule and a due date/window. Draft or label-only rows generate no event. |
| Shed/cohort vaccination drive | `phc` | `obligation_batches` grouped from due vaccination obligations | This is the worker-facing work unit. The event owner is the vaccinator/health worker or assigned execution role. |
| Manual campaign / catch-up drive | `phc` | Published/manual-campaign protocol rule or explicit Preventive Care approved catch-up batch | Must be source-backed or explicitly approved; no fabricated history. |
| Booster due after accepted completion | `phc` | SM-7 generation from accepted `vaccination_completions.administered_at` | Due date is based on actual administration time, not planned date. |
| Vaccination defer / waiver review due | `phc` | Dated Preventive Care (PC) review task derived from a blocked/deferred obligation | Applies to medical defer states such as sick, ICU, quarantine, adverse reaction review, or Preventive Care approved waiver. No event if the state is only a passive flag. |
| Historical or holding-farm vaccination evidence review due | `phc` | Procurement/intake evidence plus Preventive Care (PC) backfill/review workflow | Source-side vaccination history is evidence only. It becomes Calendar work only when a Preventive Care (PC) reviewer has a due action to accept/reject it under the vaccination contract. |
| Vaccination proof verification due | `phc` | SOP task/submission verification due work, when it has `due_at` and verifier owner | Only appears if it is a dated human verifier action. Otherwise it stays in Action Center / Protocol Adherence. |
| Vaccination rework due | `phc` | Rejected proof/rework task with owner and due date | Only the dated rework action appears. The rejected proof record itself is not a Calendar event. |
| Vaccine cold-chain check | `inventory` | Inventory/cold-chain task or obligation | Supporting vaccination readiness, but owned by inventory keeper. |
| Vaccine stock readiness / reservation shortfall | `inventory` | `obligation_batches` plus inventory ledger/readiness task | Resolves lot assignment, FEFO reserve, stock-out, or shortfall before a drive. The drive remains Preventive Care (PC); the stock fix is Inventory/Stock. |
| Vaccine reorder / expiry / GRN | `inventory` | Inventory task or stock review due action | Not a Preventive Care (PC) event unless Preventive Care (PC) separately raises a dated medical or anti-misuse action. |
| Preventive Care (PC) stock anti-misuse investigation due | `phc` | Preventive Care (PC) discrepancy/spot-audit/penalty follow-up with due date | Used when expected vaccine usage, access log, or stock movement variance needs Preventive Care (PC) Director action. Physical ledger correction stays Inventory/Stock. |
| Config/source approval due | `admin_data_ops` | Config/source-review workflow with due date | A protocol draft itself is not a Calendar event; a due source-review or approval task is. |

Vaccination data that must not create Calendar events:

- label-only vaccines without timing/dose/booster/source approval evidence.
- draft protocol versions.
- static status-matrix cells.
- coverage percentages, overdue counts, and KPI cards.
- Goat Passport vaccination history.
- owner-missing gaps; these stay in Action Center / Control Tower until an owner
  exists.
- background sweepers/replays/generation jobs with no human owner.
- trusted holding-farm vaccination evidence by itself; it affects generation and
  suppression, but only creates Calendar work if it becomes a dated human action.
- notification/reminder pings that point at an existing due work item.

## Future Calendar Scope

The 12-pill taxonomy is documented here so future Calendar work does not invent
new owner keys: 10 active event-owner keys, the filter-only `all` pill, and the
reserved `sales_commerce` key. Future modules may add events under the same
admission rule:

```text
due_at/window + owner/executor + action required
```

Examples:

- Preventive Care (PC) future: deworming, biosecurity, quarantine-health checks, treatment, ICU,
  feed/water lab QA, post-mortem due work, adverse-reaction follow-up.
- Feed future: ration direction generation, cutoff Diff, feed packing, bridge
  exceptions, feeding sessions, wastage/variance actions.
- Breeding future: estrus, AI, pregnancy scan, kidding, lactation, colostrum,
  milk-feeding sessions.
- Parks/Infra future: ground movement, sanitation, physical isolation, infra
  inspection.
- Procurement future: source warmup, dispatch, arrival gate, accepted-intake
  handoff.
- Inventory future: stock audit, expiry, cold-chain, reorder, GRN.
- Sales/Commerce future: dispatch dates, allocation deadlines, payment follow-up
  after the Sales/Commerce slice is active.

## Implementation Notes

- Persist owner with a stable owner key from this ADR; do not use mock-only keys
  like `goats`, `farms`, display labels with slashes, or ambiguous `health`.
- Calendar filters show an event when `event.owner_key` matches the selected
  pill, or when `all` is selected.
- `system: true` events are excluded from pill counts and visible lists unless
  explicitly showing a system diagnostics view.
- `cross_cutting: true` events remain visible to leadership/global command
  views even if their v1 owner key is `hr_people`.
- Protocol category is a source hint, not the final owner by itself:
  `vaccination`, `deworming`, `biosecurity`, `feed_water_testing`, `sop_video`,
  and Preventive Care (PC) `stock_check` review work normally map to `phc`; `feed_direction`
  maps to `feed`; `director_reporting` should derive `cross_cutting: true`.
- The top bar owns park/date scope. Calendar may filter by date/window and park,
  but should not duplicate scope chips inside every event section.
