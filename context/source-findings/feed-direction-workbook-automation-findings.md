# Feed Direction Workbook And Automation Findings

Status: sanitized source finding for Feed Direction implementation reference.

Sources reviewed:

- `Feed, Shiftings and Count.docx` v1.1, June 2026.
- `Feed transfer KT - 2026_06_24 15_00 IST - Notes by Gemini.docx`.
- `Counting DB - values only.xlsx` from the local Counting DB reconstruction.
- `Feed Directions Automation DB.xlsx`.
- Legacy Apps Script files in `slack-automation-scripts/`: `feed_automation.js`,
  `unified_automation.js`, `counting_db_automation.js`, and
  `video_verification_system.js`.
- Read-only live review on 2026-07-19 of the Experiment Feed Directions
  workbook and the CBE/CPT experiment packing, distribution, and wastage Slack
  channels.

Do not commit raw workbook rows, Slack media links, user names, file ids, tokens,
webhook URLs, or local source paths. This note records structure and design
implications only.

## Controlling Rule

`Feed, Shiftings and Count.docx` is the business source of truth for the Feed
Direction slice. It controls physical Base Count behavior, append-only
Shifting ledger, one-day projection, Feed Direction clocks, Diff behavior,
manual bridge behavior, as-fed field quantities, and ration solver constraints.

The workbooks and Apps Script are legacy implementation evidence. They are useful
for migration, parity, fixture design, and cutover risk. They are not GoatOS
runtime truth, product timing authority, or target schema.

## Workbook Findings

The Counting DB workbook exposes the historical count/source shape:

```text
Date, Farm, Shed, Shed Tag, Breed, Age, Count, Staff/Staff (Counted)
```

The inspected local copy had a large `DB` tab, small `FutureDB` and
`Projected-DB` tabs, and many comparison/pivot/validation views. It also showed
source-quality issues that GoatOS must absorb through import validation and
reference-data review, not through feed code:

- `Shed Tag`, `Breed`, and `Age` contain whitespace/case variants such as
  `Non-Pregnant` vs `Non-Pregnant `, `Warmup` vs `WarmUp`, `Kid` vs `kid`, and
  breed spacing/case variants.
- Some comparison/pivot tabs contain broken `#REF!` references.
- The useful count grain for the initial Feed Direction slice is physical
  aggregate shed + breed, with reviewed ration-context resolution attached or
  blocked. RFID-to-shed per-animal derivation remains future scope.

Sanitized alias extraction from the Counting DB, Feed Directions Automation
workbook, and Sheds DB evidence produced
`backend/testdata/counts/source-workbook-required-aliases.json`: 121 required
aliases across `breed`, `stage_tag`, `age_class`, and `sex`. This fixture
intentionally includes dirty source variants such as `Pregant`, `Bukcs`,
`Anathapur Sheep`, F1/F2 weight-band labels, warm-up variants, ICU variants, and
male/female labels so `counts-alias-coverage-check` can force owner-reviewed
retain/merge/retire decisions before Feed consumes projection rows.

Sanitized column mapping extraction produced
`backend/testdata/counts/source-workbook-column-map.json`: 117 mapped columns
across 16 source-role groups for count source tabs, future/projected count
candidate tabs, Feed Direction output rows, validation/feed-vector tables,
supply planning tables, session template rows, packing proof, transport proof,
consumption/wastage proof, and Sheds DB profile evidence. The companion
`backend/cmd/counts-workbook-mapping-check` command validates that each role
group has the required canonical fields before `CSG10` can move even to
pending. This is mapping coverage evidence only; it does not import raw
workbook rows or approve formulas as runtime truth.

The companion `backend/cmd/counts-workbook-source-scan` command checks the
actual local XLSX files for sheet/header structure only. Current evidence covers
the Counting DB, Feed Directions Automation DB, and Sheds DB workbooks with 117
required headers across 14 sheets / 3 workbooks. The Feed Automation `Template`
sheet represents farm/park context as `CBE` and `CPT` section headers, not as a
literal `Farm` column. The scan intentionally imports no private rows, formulas,
media links, Slack refs, or workbook values; it is structure proof for parity
work, not runtime truth.

The Feed Directions Automation workbook is a sheet implementation of feed
planning and execution, not a clean domain model:

- `Feed Direction` materializes rows by date, farm, session, shed, shed tag,
  breed, age, count, feed item/quantity pairs, session total, and processed
  flags.
- `Count-DB`, `CPT Supply Planning`, and `CBE Supply Planning` stage imported
  counts and per-feed quantities before materialized directions are written.
- `CPT Validation`, `CBE Validation`, hidden validation copies,
  `Validation-BW`, and `Feed-Energy-Protein` contain formula-driven ration and
  energy/protein logic. Some hidden copies contain broken references.
- Those validation and feed-energy tabs combine feed vectors (`Energy`,
  `Dry Matter Factor`, `Wastage Factor`, `Net Energy`) with breed/variation
  requirements and feed factors. The inspected tabs include dimensions such as
  Pregnant, M0/Mother, Non Pregnant, Warmup, K0/K1, F2/Fattening, Flushing,
  Milking, breed aliases, and weight/energy-style thresholds. These are
  candidate config families, not stable formulas to execute in production.
- Ratios and quantities are row data, not product constants. Examples observed
  in KT or workbook context (`80/20`, `400-500g`, `600g`, F1/F2 ranges,
  pregnancy/warm-up allowances, and similar thresholds) must be imported into a
  reviewed parameter template only if source owners approve them for a specific
  dimension set such as breed, shed tag/stage, kid weight band/ADG,
  pregnancy/lactation/warm-up state, feed vector family, farm, or effective
  version. The runtime must never hardcode `80/20` or any single quantity as a
  global Feed Direction rule.
- `Template` defines per-farm sessions and feed sets.
- `Feed Packing Form`, `Feed Transport Form`, and `Feed Consumption & Wastage`
  are execution/proof surfaces with media links and processed/reconciliation
  signals. These prove stage capability requirements; they must not be copied as
  Sheet truth.

### Legacy experiment-feed finding

The separate experiment workbook is a workaround for a limitation in the normal
Sheet/App Script allocation model, not evidence for a separate GoatOS product
module. The normal workbook tends to apply one shared composition by
breed/shed-tag/category. The experiment config instead records quantities at
farm + shed + category + feed-item grain, allowing same-tag sheds to receive
different composition permutations. A sanitized structural check found three
CBE Castro sheds with the same `Sheep M NEW` category and three distinct
concentrate/bhusa quantity pairs; raw rows and workbook identifiers are not
committed.

The live Slack workflow has six private operational channels: packing,
distribution, and wastage for each of CBE and CPT. Threads prompt for packing,
distribution/water, and wastage media and then record completion. Combined with
the Apps Script afternoon packing trigger, this supports the operational loop:
observe distribution/wastage during the day, review or revise the composition,
and publish the next-day packing direction. Slack is proof of collection and
delivery; it is not approval authority. GoatOS audit/version history must record
who approved the next composition and why.

Target implication: versioned per-shed/per-cohort composition assignment belongs
inside normal Feed Direction. An optional comparison label/group may organise
alternatives, but ordinary same-tag variation must not require a formal
experiment, control/treatment record, or duplicate packing/wastage backend.

## Legacy Automation Findings

Legacy feed automation roughly does this:

1. Copy or compare count rows between `FutureDB`, `Count-DB`, projected/count
   tabs, and shifting reports.
2. Mutate/normalize tags in script code, including examples such as
   `F2-Male`/`Fattening`.
3. Read `Template` and `CBE/CPT Supply Planning` tabs.
4. Generate or regenerate `Feed Direction` rows, including affected-shed change
   paths and processed flags.
5. Send packing, afternoon-change, consumption, transport, proof, stock, wastage,
   archive, retry, and alert workflows through Apps Script and Slack.

This proves useful workflow intent: feed directions, Diffs, packing proof,
transport proof, consumption/wastage proof, shortfall/rework, stock/wastage
alerts, and retries matter operationally. It also proves the weakness GoatOS must
replace: mutable sheets, formula-only validation, string transforms, hidden
copies, processed flags, script properties, retry triggers, and Slack links are
not robust source-of-truth design.

## GoatOS Design Implications

GoatOS must replace workbook tabs and script glue with governed kernel
capabilities:

- typed imports with source checksum, row-level validation, alias matching,
  calculation preview, dry-run parity preview, dead-letter/repair state, and
  business audit;
- CRUD/review/publish surfaces for feed item nutrient vectors, feed costs,
  feed-type constraints, breed aliases, stage/tag aliases, kid weight-band/ADG
  rules, quantity/weight-band thresholds, warm-up/pregnancy policy,
  eligibility/exclusions, transport maps, proof thresholds, validation
  tolerances, and session-slot policy;
- a reviewed resolver from physical count projection rows
  (`park + shed + breed + horizon`) to approved nutrition/ration cohort keys;
- versioned `feed_direction` protocol config with immutable effective-dated
  published versions; draft changes may be edited, but generated directions are
  superseded explicitly rather than silently rewritten;
- immutable generation runs and count/ration snapshots instead of live formula
  dependencies;
- stage obligations or typed stage records for packing, transport,
  consumption/wastage, bridge exceptions, proof, verification, rejection, and
  rework;
- bounded Postgres queries, idempotency keys, audit/outbox, scheduler/sweeper
  jobs, notification ports, and command-lens read models.

Implementation note as of 2026-06-30: Counts/Shifting now has
`backend/cmd/counts-source-import` for reviewed typed JSONL
`base_count_anchor` and `shifting_event` rows. That command is the safe path for
Counting DB / Feed Direction workbook evidence once a reviewer has mapped the
row into GoatOS fields, and `count_source_import_runs` records execute-mode
import status, row counts, replays, failures, source reference, and readiness
evidence. `count_projection_recompute_runs` records bounded projection worker
status, row counts, exception counts, snapshot reference, trace, and failure
evidence for the same `G2` closure lane. Neither path is a raw workbook parser,
and neither promotes workbook ratios, feed vector examples, breed/tag shortcuts,
or formula outputs into Feed Direction runtime truth.
`counts-workbook-source-scan` is the limited exception that opens raw XLSX files,
but only to validate workbook/sheet/header presence against the sanitized map;
it still does not import rows, calculate formulas, or approve source values.
`backend/testdata/counts/source-parity-high-risk-sample.json` is the executable
sanitized parity sample for pregnancy/lactation/warm-up breadth: pregnant late
gestation, lactating/mother, pregnant warm-up, fattening male warm-up, and
normal non-pregnant rows are compared against canonical projection reads by
`backend/cmd/counts-source-parity-check`. This proves the comparison shape; it
does not replace full workbook row parity, owner-approved nutrition policy, or
seeded local E2E.

Required runtime shape:

```text
source workbook/import batch
-> typed feed parameter template rows
-> alias/dimension/source-hash validation
-> calculation preview and workbook/solver parity review
-> row-level errors / repair / DLQ for wrong data
-> reviewed + approved feed_direction protocol version
-> immutable generation snapshot for each Feed Direction run
```

Wrong or incomplete data must fail closed before generation. Block on unknown
breed, unreviewed shed tag/stage, unresolved F2/sex-gender tag, missing
pregnancy/warm-up policy, invalid ratio or slot-weight math, non-numeric as-fed
quantity, missing feed vector, broken workbook/formula reference, missing source
hash, missing resolver coverage from `shed + breed` to nutrition cohort, or
calculation-preview mismatch. These failures become visible repair/process work;
they must not be silently "fixed" by legacy string transforms.

Admins can change or add Feed Direction serving slots, but only through
approved, effective-dated protocol config. The default published policy should
start from the docx two slots, `09:00` and `15:00`, with a `50/50` split. More
slots, disabled slots, reordered slots, changed weights, or scoped feed-item
inclusion require Feed Director draft plus COO/CEO publish authority, validation
that active weights cover the full daily as-fed quantity, and explicit
supersession behavior for already-generated dates.

The old workbook names (`Count-DB`, `CPT Validation`, `CBE Validation`,
`Feed-Energy-Protein`, `Supply Planning`, `Template`, `Feed Packing Form`,
`Feed Transport Form`, and `Feed Consumption & Wastage`) should appear in
GoatOS only as import/parity labels, source evidence, or migration mappings.
They must not become product module names, runtime tables, or hardcoded
business rules.
