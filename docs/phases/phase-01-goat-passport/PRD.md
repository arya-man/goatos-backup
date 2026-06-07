# Phase 1 PRD: Goat Passport And Herd Registry

Status: draft for review.

## Summary

Phase 1 gives every goat a trusted Goat OS passport.

Today, goat identity is spread across Sheets, old tags, possible RFID tags,
manual notes, dashboard counts, and operator memory. That is dangerous because
the same tag can be reused, typed wrong, missing, or attached to the wrong goat.

Goat OS must create one permanent internal goat identity:

```text
goat_id = the official Goat OS identity
```

Tags are evidence, not truth:

```text
old tag
RFID tag
visual tag
sheet row ID
purchase/load ID
temporary field ID
```

If tags are clean, Goat OS links them. If tags conflict, Goat OS stops and asks
for review instead of silently creating wrong truth.

Important nuance:

```text
RFID may be globally unique.
Old tags may be unique only inside a farm, load, or legacy source.
Goat OS must store that scope instead of assuming every tag is globally unique.
```

## Why This Phase Exists

Every later Goat OS module depends on identity:

```text
vaccination
health
breeding
genetics
growth
feed
movement
sales
meat yield
devices
AI suggestions
analytics
```

If Phase 1 gets goat identity wrong, later modules will confidently attach
proof, vaccines, treatments, offspring, meat yield, and sale history to the
wrong goat. Phase 1 is the foundation.

## Business Goals

- Create one trusted goat record per real goat.
- Import the existing herd from current Sheets/XLSX sources.
- Preserve all useful legacy identifiers without treating any single tag as
  permanent truth.
- Detect duplicate, missing, dirty, reused, or conflicting identifiers.
- Give admins a review workflow for identity conflicts.
- Give dashboards and mobile app a stable way to find goats.
- Prepare the identity layer for RFID, camera/FaceID suggestions, and device
  observations later.

## Architecture Guarantees

Phase 1 must be built in the same Goat OS architecture as every later phase.
This is not a throwaway import tool.

In short:

```text
The goat identity brain stays inside Goat OS.
Replaceable tools stay outside that brain.
```

That means:

- The backend is one modular Go system, but identity, locations, import,
  permissions, media, reporting, and outbox stay behind clear module walls.
- Product logic must not be locked to one vendor. In SOLID terms, Goat OS
  depends on interfaces first, and concrete vendors sit behind adapters.
- Auth, storage, analytics, media, device integrations, AI identity
  suggestions, and notifications must be plug-play components wired through
  bootstrap/factory code, not scattered through business rules.
- Admin web, mobile, and dashboard code must call Goat OS APIs. They must not
  read databases, Sheets, BigQuery, Firestore, or vendor SDKs directly as the
  source of product truth.
- Web/mobile clients use normal REST/JSON APIs with generated clients from
  OpenAPI.
- JSON Schema is used for event, decision, import, and repair payloads where
  compatibility matters.
- gRPC/protobuf is not the default app-client protocol. It is reserved for
  internal high-volume seams or future split services when the workload proves
  it is needed.

## Analytics And Monitoring In This Phase

Phase 1 is not where we build every BI screen and every AI analyst feature.
But Phase 1 must create identity data in the correct analytics shape from day
one.

In short:

```text
Goat identity must be clean enough that dashboards, Cube, Metabase, AI, and
future analytics can trust it.
```

This phase builds:

- identity events and outbox messages for every canonical identity change
- small herd-count projections for fast dashboard counts
- analytics API contracts for identity counts and freshness
- dashboard rewiring path so copied dashboard UI calls Goat OS APIs/facades
- monitoring for import health, API latency, search latency, outbox lag,
  conflict volume, and dashboard projection freshness

This phase does not make Metabase or Cube the source of truth. They are
read-side tools. Goat identity remains in Goat OS/Postgres, emitted through
events and governed projections.

Tool meaning:

```text
Cube
  official metric definitions later/alongside analytics stack; never product truth

Metabase
  internal exploration over governed marts/metrics; never canonical writes

BigQuery
  historical warehouse; not queried directly from product pages

Tinybird
  hot telemetry/live serving when the data is high-volume/live

Grafana
  optional visualization later; OpenTelemetry + Google Cloud Monitoring are the
  default monitoring path first
```

## Users

```text
Admin
  imports data, resolves duplicates, approves identity decisions

Park head / supervisor
  checks goat identity, location, status, and dirty cases for their park

Operator
  scans/searches goat from Android when doing work

Verifier
  checks identity evidence while reviewing proof

CEO/internal user
  sees herd counts and passport-level truth in dashboards
```

## Existing Legacy Pieces

Already present:

```text
private herd workbook / Sheets
  current goat rows, old tags, breed/status/location-ish data

dashboard/
  counts, status, breed, farm, shifting, parent-stock, mortality views

vgoats-dashboard/
  sanitized/reduced investor views

Slack/App Script
  operational forms and updates that mention goats by tag/name/context
```

Legacy is input/reference, not truth. Phase 1 builds canonical identity in Goat
OS and then the existing dashboard copies are rewired to use Goat OS data.

If `goatos/apps/admin-web` or `goatos/apps/investor-web-shadow` already contain
copied dashboard snapshots, Phase 1 may reuse their shell, routes, cards, tables,
and chart UX. Those copies are still only presentation surfaces. They must call
Goat OS APIs or the analytics facade and must not preserve direct Sheets,
BigQuery, Firestore, or local CSV access as product truth.

## In Scope

### Goat Passport

Each goat profile should show:

```text
Goat OS ID
active identifiers: old tag, RFID, visual tag, sheet/source IDs
breed
sex
approximate DOB or age band
current lifecycle status
current farm/park/shed/cohort
source/load/vendor if known
basic family references if already known
identity confidence/review state
timeline of identity/location/status changes
```

### Identifier Registry

Goat OS must track identifiers as separate records:

```text
identifier type
identifier value
identifier scope: global | farm | park | load | source
linked goat_id
status: active | retired | disputed | duplicate | invalid
valid_from / valid_to
source record
confidence
who approved it
```

Identifier examples:

```text
RFID RFID_EXAMPLE_000001
  likely global

old tag 1900
  may be unique only in CBE, a purchase load, or one legacy sheet

sheet row 8421
  unique only inside that sheet/import
```

### Import And Reconciliation

Import current data into staging first.

For each incoming row:

```text
exact unique RFID match          -> link
exact unique old-tag match       -> candidate or auto-link based on rule
same tag on multiple goats       -> conflict
missing tag                      -> needs review or temp identity
same goat with changed tag       -> retire old identifier, add new identifier
fuzzy/similar record             -> proposal, never automatic truth
```

Spreadsheet row number is not goat identity. If a sheet is sorted or a row is
inserted, row numbers change. Goat OS must use a stable source ID when available
or create a reviewed stable import key. A content hash only detects that a known
source row changed; it must not be used by itself to decide two goats are the
same.

Every import run must use a named, versioned import policy:

```text
source system and dataset
stable source-key recipe
hash recipe and fields
old-tag/RFID/visual-tag scope rules
which fields can auto-apply
which fields require review
```

If the source has no stable row ID, the synthesized key recipe must be approved
before import. Tagless goats or goats with the same breed/sex/location cannot be
collapsed into one goat just because their rows look similar.

### Dirty Data Review

Admins need queues for:

```text
duplicate tag
missing tag
RFID conflict
old tag reused
possible same goat
possible different goats with same tag
missing breed/sex/location
source rows not imported
```

### Search And Lookup

Users should search by:

```text
Goat OS ID
old tag
RFID
visual tag
source row ID
farm/park/shed/cohort
breed/status/sex
```

### Basic Passport Timeline

Show identity-level events:

```text
goat created
identifier added
identifier retired
identifier disputed
location changed
status changed
merge approved
merge rejected
conflict opened/resolved
legacy row imported
```

## Out Of Scope For Phase 1

Not built in this phase:

```text
vaccination workflow
health/treatment/death forms
full workforce roster
feed/growth calculations
breeding/genetics scoring
sales/allocation
device gateway ingestion
AI FaceID production model
public website changes
payments/investor ownership
```

Phase 1 can store basic legacy values if available, but it does not implement
those domain workflows.

## Key Product Rules

### Truth Rule

```text
goat_id is truth.
tags/RFID/source IDs are identifiers attached to goat_id.
```

### No Silent Merge Rule

Goat OS must not merge two goats automatically if the evidence is ambiguous.

Ambiguous cases become:

```text
needs_review
```

### No Silent Duplicate Rule

If the same active identifier is attached to more than one goat, Goat OS must
create a conflict instead of pretending both are valid.

### Scoped Identifier Rule

Goat OS must not assume every tag is globally unique.

```text
RFID can be globally unique.
Old tags can be scoped by farm, park, purchase load, or source system.
Sheet row IDs are scoped to the import/source.
```

If the scope is unknown, Goat OS treats the case as lower confidence and may
send it to review.

Unknown scope must never be silently treated as global. If Goat OS does not know
whether an old tag is farm-scoped, load-scoped, or source-scoped, it must show a
review case before linking that tag as truth.

### No Delete Rule

Goats and identity decisions are not deleted when records are merged or
corrected.

```text
wrong/duplicate goat record -> marked merged/inactive
surviving goat_id           -> stays canonical
old record                  -> redirects to survivor with audit trail
```

That preserves vaccination, health, breeding, and proof history.

### Temporary Identity Rule

If a goat has no reliable identifier, Goat OS may create a temporary identity
only when a human/source context is attached:

```text
location
source row/load
breed/sex/status if known
created_by
reason
review_required
```

Temporary identities cannot be used for high-risk workflows without review.
They still get a temporary Goat OS identifier so the goat is findable in the
system, but the passport clearly shows `needs_review` until stronger evidence is
attached.

### Evidence Rule

Every identity decision must store evidence:

```text
source row IDs
identifier IDs
matching reasons
before/after goat IDs when merged
actor
timestamp
decision result
```

### Ledger Rule

Every canonical identity change must be replay-safe and auditable:

```text
idempotency key
identity decision
typed identity event
audit record
outbox message for analytics/notifications
```

These are written together. Replaying the same import row or mobile/admin action
must return the same result instead of creating a second goat, identifier,
conflict, event, or dashboard count.

### AI Rule

AI or image matching may suggest:

```text
"these records may be the same goat"
```

AI may not directly merge goats or make canonical identity truth.

## Main Workflows

### Workflow 1: Import Existing Herd

```text
Admin uploads/imports current goat data extract
-> Goat OS stages rows
-> system normalizes identifiers and locations
-> clean rows become goat passports
-> dirty rows become review cases
-> admin sees import summary
```

Import summary should show:

```text
rows processed
goats created
identifiers added
clean matches
duplicates found
missing required fields
conflicts opened
rows needing review
```

### Workflow 2: Resolve Duplicate Tag

```text
Admin opens duplicate-tag queue
-> sees both goat records side by side
-> sees source rows, location, breed, sex, age/status clues
-> chooses:
     same goat, merge
     different goats, mark identifier disputed
     create new goat
     reject candidate
     needs field verification
-> Goat OS records decision and updates timeline
```

If the admin chooses `same goat, merge`, Goat OS must:

```text
choose one surviving goat_id
mark the other goat_id as merged
move/retire identifiers according to policy
keep a redirect from merged goat to survivor
store evidence and decision record
```

### Workflow 3: Add RFID To Existing Goat

```text
Operator/admin scans RFID
-> searches old tag or goat profile
-> Goat OS checks RFID uniqueness
-> if unique, RFID is added as active identifier
-> if already linked elsewhere, conflict opens
```

### Workflow 4: Android Goat Lookup

```text
Operator opens goat lookup
-> scans RFID or types old tag
-> sees matching goat passport summary
-> if multiple matches, operator cannot choose blindly
-> dirty case is flagged for admin/park head review
```

### Workflow 5: Dashboard Counts Rewire

```text
dashboard copy calls Goat OS API/analytics facade
-> counts come from canonical goat identity/status/location
-> old live dashboard remains untouched until validated
```

Dashboard counts must not scan every goat row every time someone opens the
dashboard. Goat OS keeps small count projections, updated from identity/status
changes, so counts remain fast at 50k goats and still work at 1M+ goats.

During a large import, those counts are rebuilt after the import batch finishes
instead of incrementing the same hot counter row for every goat. Normal daily
changes can update counts incrementally.

Count responses should show their freshness, especially during migration:

```text
as-of time
source import run if relevant
rebuilding/stale warning if a large import is still settling
```

Dashboard pages must never fall back to raw dashboard BigQuery, Sheets, local
CSV, or full-herd API fetches when a projection is stale.

### Workflow 6: Resolve Tagless Or Field-Dirty Goat

```text
Operator or park head finds goat without reliable tag/RFID
-> opens goat lookup or correction request
-> records location, visible clues, optional photo/proof reference, and reason
-> Goat OS creates or links a temporary identity only with source context
-> admin reviews possible duplicates before attaching a permanent identifier
```

Tagless goats remain visible for follow-up, but they are clearly marked
`needs_review` and cannot be silently merged with another tagless goat.

## Screens Needed

Admin web:

```text
Herd search
Goat passport detail
Import run summary
Dirty-data review queue
Duplicate/conflict review detail
Identifier history panel
Location/status edit with audit
```

Operator mobile:

```text
Goat lookup by scan/search
Goat passport compact card
Dirty/multiple-match warning
```

Dashboard:

```text
Counts by status
Counts by location
Counts by breed/sex
Import/reconciliation health summary for admins
```

## Permissions

```text
Admin
  import data, approve merges, resolve conflicts, edit canonical identity fields

Park head
  view goats in assigned park, request corrections, resolve low-risk local identity issues if permitted

Operator
  lookup goats in granted work/location scope, scan identifiers, submit correction request, cannot merge

Verifier
  view identity evidence needed for proof verification

CEO/internal
  read dashboards and passport data by granted scope
```

Search and lookup must enforce scope before showing details. A user without
permission for a goat must not see that goat's passport, source rows, conflict
details, proof references, or operator names through identifier search.

## Acceptance Criteria

Phase 1 is review-complete when:

```text
existing herd sample imports into staging
clean goats receive Goat OS IDs
old tags and RFID values are modeled as identifiers
duplicate/missing/dirty tags become review cases
admin can resolve a duplicate case with audit trail
admin can merge duplicate goat records without deleting history
operator can lookup a goat by tag/RFID in mobile shell
dashboard copy has contract compatibility and at least one canonical count path
backed by imported sample data
no ambiguous identity merge becomes truth without approval
all identity decisions have evidence IDs
all identity mutations write audit records, typed events, and outbox messages
replaying the same import row/action is idempotent
scoped lookup does not leak out-of-scope goat details
```

## Success Metrics

```text
% imported rows linked to goat_id
% rows requiring review
duplicate active identifier count
missing critical identifier count
time to resolve dirty identity case
operator lookup success rate
wrong-merge count, target 0
```

## Non-Technical Examples

### Clean Import Row

```text
Input:
  old tag 1900, Boer female, CBE shed 3

Goat OS result:
  creates or links one goat passport
  adds old tag 1900 as an identifier
  marks identity clean if no conflict exists

Admin sees:
  imported cleanly

Operator sees:
  one goat when searching tag 1900
```

### Reused Old Tag

```text
Input:
  two different rows both say old tag 1900 in the same scope

Goat OS result:
  does not guess
  opens duplicate-tag conflict

Admin sees:
  side-by-side evidence and must decide same goat, different goats, or field check

Operator sees:
  multiple/dirty match warning, not a silent selection
```

### RFID Conflict

```text
Input:
  RFID RFID_EXAMPLE_000001 is already active on goat A
  another scan tries to attach it to goat B

Goat OS result:
  blocks direct attachment
  opens RFID conflict

Admin sees:
  current owner, attempted owner, source, and actor evidence
```

### Missing Tag

```text
Input:
  goat row has no reliable old tag or RFID

Goat OS result:
  creates temporary identity only if source/location/context is enough
  otherwise sends row to review

Admin sees:
  missing-identifier queue
```

## Phase 1 Decision Gate Before Migrations

These decisions shape schema, import policy, and review rules. Contracts and
read-only discovery can start immediately. Migrations, import seeds, identifier
policies, and canonical import logic must wait until these decisions are
confirmed.

These are human/business answers. An agent may identify the missing decision
and propose options, but it must not choose defaults on its own. If any required
decision is unknown, stop and ask before writing migrations or canonical import
logic.

Before asking a human owner to answer from memory, the implementation agent must run a
read-only legacy discovery pass over the current artifacts:

```text
<mesha-workspace>/source-material/private-data/private herd workbook
dashboard/ and vgoats-dashboard/ CSVs, API routes, data loaders, and display logic
slack-automation-scripts/ App Script and Slack SOP automation files
procurement_app/ mobile/operator patterns if useful
```

The output should be an evidence-backed proposal, not a silent default:

```text
old-tag uniqueness evidence + proposed scope
lifecycle/status vocabulary + proposed canonical mapping
tenant/party/custody/location mapping proposal
first migration source candidates with row counts/freshness
SOP/form inventory found in Slack/App Script
open policy decisions that cannot be derived from artifacts
```

A human owner confirms or corrects the proposal. Raw private goat data and PII stay out
of git; committed proposal docs may include aggregate counts, source paths,
column names, and anonymized examples only.

Current discovery output:

```text
docs/phases/phase-01-goat-passport/legacy-discovery-proposals.md
```

```text
old_tag uniqueness
  global | farm-scoped | park-scoped | load/source-scoped
  unknown scope goes to review, never silently to global

RFID uniqueness
  active RFID is globally unique unless explicitly approved otherwise

human display ID
  Goat OS exposes a human-readable display_id separate from immutable goat_id
  display_id is generated by Goat OS as a global G-000001 style code, not manually typed during import
  display_id must not include current farm/location because goats can move

minimum location for import
  farm | park | shed | cohort
  unknown values map to explicit unknown locations, not empty strings

tenant / party / custody / location
  every goat needs a tenant for isolation, an owner party for economic ownership,
  a custodian party for operational responsibility, and a current location
  this is separate from daily task assignment because operators can change without custody changing
  first migration must map each canonical goat to approved parties or leave the row in staging/review

status structure
  lifecycle, reproductive status, growth/cohort tag, and health status are separate axes
  label semantics for sale-blocking and SOP-trigger rules still need ops confirmation

merge approval authority
  central admin approves and assigns approval roles; farm admin can approve only when granted
  mass approval requires validation preview, evidence sampling, and dry-run

first migration source
  file/source name, owner, date, and column dictionary

first migration scope
  locked RFID DB only for first import; event-log/tagless temporary identities come in a later pass
```

Pre-migration lock list:

```text
goat_id format
display_id generator: global G-000001 style, no farm/location in code
tenant/party/custody mapping
old-tag scope policy
identifier policy per type
temporary identity minimum evidence
status structure and label semantics
source stable-key recipe
source hash/diff recipe
first import policy version
first import scope
```

## Migration Decisions

These are not open architecture debates. They are the few decisions needed to
move old Sheets/Slack data into the new Goat OS model without guessing.

### Terms Used Below

Canonical term definitions live in:

```text
context/product/glossary.md
```

Important for Phase 1:

- `CBE` is the Coimbatore farm/location code. `CPT` is the Channapatna farm/location code. Official addresses and geo details are still pending.
- `HF` / `Holding Farm` means an external agent/partner holding place used after purchase and before transport or farm intake. Do not treat it as final goat ownership truth unless a confirmed mapping exists.
- `Origin Farm` means where the goat was purchased from, born, or originally sourced.
- `source row`, `load`, `tenant`, `party`, `owner`, `custodian`, `staging`, `current location`, `display ID`, and `merge` are defined in the glossary.

### Answers Already Locked

**Display ID**

Meaning: `goat_id` is internal. `display_id` is the goat code people will search, read, and say out loud.

Locked answer: use an easy global human code, defaulting to `G-000001` style. It must not include farm/location because goats can move. RFID, old tag, QR, visual tag, shed, and breed remain searchable aliases.

**Old tag scope**

Meaning: the same old tag number appears in different farms in the discovered data.

Locked answer: old tags are farm-scoped, not globally unique.

**First migration source**

Meaning: RFID DB looks like the clean goat registry; DB/dashboard files look like history or derived views.

Locked answer: use RFID DB first for the identity seed, then reconcile DB/dashboard data.

**First import scope**

Meaning: RFID DB is the cleanest low-risk identity slice. DB/tagless/event rows
are dirtier and need temporary identity/review handling.

Locked answer: first import seeds RFID DB only. Tagless/event rows come in a
later import pass as temporary/review identities.

**Owner, custodian, and location separation**

Meaning: this is about the goat, not where a person lives. One party can own the goat's value, another party can be responsible for caring for it, and the goat can physically sit in a farm/park/shed.

Locked answer: keep owner, custodian, and physical location as separate concepts.

**Merge approval**

Meaning: merging two goat records is risky because a wrong merge corrupts identity, health, vaccination, genetics, and sale history.

Locked answer: central admin can approve identity merges and can configure which roles may approve. Farm admins can approve only if central admin grants that role/scope. Park heads/operators can request or recommend. Mass merge approval is allowed only after validation preview, evidence sampling, and dry-run results.

**Temporary goat**

Meaning: this is for goats before proper tag/RFID, or when the tag is missing/lost/dirty/unreadable.

Locked answer: temporary goats are allowed, but they stay in review until linked to stronger evidence. If an operator creates one in the field, photo/proof is required because it is the only dedupe anchor. If import creates one from old data, source row evidence is required. Temporary goats that do not get a durable tag/RFID/approved visual link inside the configured staleness window escalate to review.

**Status structure**

Meaning: legacy status mixes pregnancy, kid stage, fattening, sex, and health in one string. Genetics and R&D need this split correctly.

Locked answer: separate the structure into lifecycle, reproductive status, growth/cohort tag, and health status. Do not leave the rest mashed after pulling lifecycle out.

**Core site-code meanings**

Meaning: CBE/CPT/HF were unclear labels in Sheets and dashboards.

Locked answer: CBE is Coimbatore farm/location. CPT is Channapatna farm/location. HF means Holding Farm, an external agent/partner holding place used during procurement/warm-up. Origin Farm is source/origin evidence, not current location by itself.

**HF partner modeling**

Meaning: HF partners are real external partner/source names, but the audio does
not prove final goat ownership for every HF row.

Locked answer: create minimal external party records for known HF partners in
Phase 1. Create location records only where source evidence indicates a physical
holding place. Do not assume ownership from the HF label.

**Current location conflict**

Meaning: RFID DB and latest DB event should ideally agree on current shed/location.

Locked answer: if RFID DB shed and latest DB event shed disagree, treat it as a data discrepancy and route to reconciliation/review. Do not blindly pick one source.

**Growth/cohort label meanings**

Meaning: labels like K0/K1/K2/K3/M0/F2 are operational stages, not goat identity tags.

Locked answer: K0 is newborn first 1-2 days; K1 is bottle-milk training; K2 is milk plus solid-feed training for roughly two months; K3 is weaning to solid feed; M0 is mother post-delivery for around a month; F2-Male/F2-Female are post-weaning fattening groups separated by sex. Warmup can happen at source before travel and at destination after arrival.

### Still Need Legacy/Ops Meaning

These are not technical architecture questions. They are places where current
legacy data uses business words/codes that only operations can correctly
interpret.

**Status label semantics**

Current legacy state: labels include `K0/K1/K2/K3`, `Pregnant`, `Non-Pregnant`, `Mother`, `Milking`, `Buck`, `F2-Male`, `F2-Female`, `ICU`, `ICU-Non-Pregnant`, `Quarantine kids`, and others.

Known from ops: K0/K1/K2/K3/M0/F2/Warmup meanings are captured in the glossary.

Need ops meaning: which labels are official for reporting, which labels block sale/allocation, and which labels should trigger SOP follow-up?

**Geo details**

Current legacy state: CBE and CPT are physical farm/location codes; HF values are external holding/source places.

Need ops meaning: official address, district, pincode, coordinates, and timezone for CBE/CPT and any HF location that should be represented as a physical location.
