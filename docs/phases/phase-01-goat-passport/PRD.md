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

Current Phase 1 implementation status: the rehearsal path is local XLSX export
first. Source discovery classifies the RFID workbook shape before staging; live
Google Sheets import is deferred until a concrete Sheet source and credentials
are supplied and reviewed. Admin-web must consume Goat OS backend APIs, not
Sheets, Apps Script, direct XLSX/CSV, or BigQuery.

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
  is the internal product owner/admin for Goat OS Phase 1, with full product
  authority to view and act across internal goat-ops workflows
```

### Phase 1 Access Boundary

Phase 1 RBAC is for internal Goat OS goat-ops users only.
The Phase 1 backend API gate is signed bearer authentication plus active
tenant-scope `user_scope_grants`; token role claims and GoatOS dev headers are
not production authorization authority.
Bootstrap HS256 tokens are capped by a default 24h max TTL to limit blast
radius until production IdP/JWKS, rotation, and revocation are implemented.

The Phase 1 backend role set is:

```text
admin
verifier
park_head
operator
ceo_internal
```

These roles cover the current Goat Passport workflows:

```text
admin
  central identity/data authority; can import, review, resolve, and mutate
  Phase 1 goat identity data

verifier
  reviews evidence, dirty data, duplicate candidates, conflicts, and correction
  requests

park_head
  can see and request identity corrections for the park-level operating view

operator
  can search/scan goats and submit correction requests from field work

ceo_internal
  full Goat OS product admin for Phase 1 API actions; this role is equivalent
  to product-admin authority inside Goat OS and is not Google Cloud, IAM,
  billing, or repository administration
```

Investor, buyer, donor, partner, lending, franchise, procurement, health,
workforce, and device-specific roles are not Phase 1 goat-ops API roles.

Existing investor/reduced dashboards are migration references for sanitized
views. They must not receive internal goat-ops grants through Phase 1
`user_scope_grants`. Investor/customer access belongs to a later commerce or
sanitized analytics realm with its own contracts and data filters.

Legacy procurement roles such as procurement head, procurement manager, and
assistant procurement manager are also not Phase 1 identity roles. They map to
later procurement/workforce phases, not to this Goat Passport RBAC slice.

### Phase 1 Local Closeout Status

The local Phase 1 identity/import/read/admin-demo spine is proven against the
real Shape-2 RFID source: normal apply produces 436 created goats and 787 review
rows; guarded RFID-only blank-suffix apply produces 711 created goats, 512
review rows, and 0 errors; `tenant_lifecycle` shows 711 alive goats. The Mesha
admin-web renders the overview, counts, herd search, a real goat passport, live
Import Review rows for that final run, and an honest Data Quality empty state
when the real local DB has no conflicts or candidates.

This is a local proof, not production/staging completion. Production auth/IdP,
cloud deployment, Pub/Sub/event egress, terminal non-goat disposition, richer
messy-data search, correction/write workflows, and non-Phase-1 legacy modules
remain deferred. Candidate approve and conflict `create_goat` routes exist, but
their canonical mutation semantics remain deferred rather than silently writing
incomplete goat state.

The remaining typed not_implemented endpoints are:

```text
GET /goats/{goat_id}/timeline
GET /identity/correction-requests
GET /admin/identity/correction-requests
POST /admin/import-runs
POST /admin/goats
PATCH /admin/goats/{goat_id}
```

## Existing Legacy Pieces

Already present:

```text
private herd workbook and sheet exports
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
Old tags can be scoped by park, purchase load, or source system.
Sheet row IDs are scoped to the import/source.
```

If the scope is unknown, Goat OS treats the case as lower confidence and may
send it to review.

Unknown scope must never be silently treated as global. If Goat OS does not know
which park/load/source scope applies to an old tag, it must show a review case
before linking that tag as truth.

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

AI proposal authority is intentionally narrower than deterministic automation:

```text
AI can propose review work with reasons, confidence, and evidence/source links.
AI-authored identity records must use ai_proposal and remain proposed or needs_review.
AI must not write as system_rule or import_policy to bypass review gates.
Governed system_rule/import_policy automation is allowed only for deterministic,
approved policy paths; it is not an AI escape hatch.
```

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

Current frontend readiness status:

```text
apps/admin-web has a buildable Phase 1 shell with disabled placeholders.
The executable legacy BigQuery/Sheets routes have been removed from that copy.
Generated Goat OS API client plumbing exists for the next real screen slice.
Full data-bound admin screens are not complete yet.
```

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
  full Goat OS product-admin access to internal Phase 1 workflows by granted
  tenant scope
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

## Phase 1 Import Guardrails

The decisions below shape schema, import policy, and review rules. They are now
locked enough for contracts, migrations, import seeds, identifier policies, and
canonical import logic to start.

If a source value is still unclear, Phase 1 must preserve the raw legacy value,
map what is known, and route the unclear part to staging/review. It must not
invent business behavior such as sale-blocking or routine task triggers.

Before asking a human owner to answer from memory, the implementation agent must run a
read-only legacy discovery pass over the current artifacts:

```text
private herd workbook kept outside git
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
context/source-findings/drive-docs-findings.md
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
  lifecycle, reproductive status, growth/cohort tag, management stage, and health status are separate axes
  health diagnosis follow-up rules are legacy-derived from Slack/App Script
  sale-blocking and non-health status/stage task triggers become configurable
  later policy rules seeded from Drive source docs; they do not block Phase 1

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
status structure and raw-label preservation
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

- `CBE` is the Coimbatore farm/location code; older data can use `CJB` for the same park. `CPT` is the Channapatna farm/location code; older data can use `BLR` for the same park. Official addresses and geo details can be backfilled later; Phase 1 may seed city/state with null pincode/coordinates.
- `HF` / `Holding Farm` means an external agent/partner holding place used for 2 to 8 weeks after purchase and before dispatch to main parks. Do not treat it as final goat ownership truth unless a confirmed mapping exists.
- `Origin Farm` means where the goat was purchased from, born, or originally sourced.
- `source row`, `load`, `tenant`, `party`, `owner`, `custodian`, `staging`, `current location`, `display ID`, and `merge` are defined in the glossary.

### Answers Already Locked

**Display ID**

Meaning: `goat_id` is internal. `display_id` is the goat code people will search, read, and say out loud.

Locked answer: use an easy global human code, defaulting to `G-000001` style. It must not include farm/location because goats can move. RFID, old tag, QR, visual tag, shed, and breed remain searchable aliases.

**Old tag scope**

Meaning: the same old tag number can appear in different parks because legacy
tags were effectively `Number + Park`.

Locked answer: old tags are park-scoped, not globally unique. `826 CBE` and
`826 CPT` can be two different goats. Historic aliases must be normalized
(`CJB -> CBE`, `BLR -> CPT`) while preserving the source code as evidence.
Duplicate old tag inside the same normalized park scope goes to review.

**First migration source**

Meaning: RFID DB looks like the clean goat registry; DB/dashboard files look like history or derived views.

Locked answer: use RFID DB first for the identity seed, then reconcile DB/dashboard data.

**First import scope**

Meaning: RFID DB is the cleanest low-risk identity slice and contains old tag,
new RFID, breed, and gender. Tagless animals are expected to be RFID-tagged soon,
so they should not be imported into Goat Passport first.

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

Locked answer: separate the structure into lifecycle, reproductive status,
growth/cohort tag, management stage, and health status. Do not leave the rest
mashed after pulling lifecycle out.

Display answer: Goat OS keeps operator-friendly labels, but not as messy free
text. The UI should show canonical display names such as `K0 - Newborn`,
`K1 - Bottle milk training`, `F2 - Fattening`, `Pregnant`, `Mother`, or `ICU`.
The familiar legacy code remains visible/searchable as the short label. Compound
legacy labels are split before storage. For example, `F2-Male` / `F2-Female`
become `growth_cohort_tag=F2`; they do not set sex by themselves because legacy
code could create those labels from blank or defaulted gender. Sex comes from the
source `Gender` column when present. `ICU-Non-Pregnant` becomes
`health_status=ICU` plus `reproductive_status=non_pregnant`. `Warmup` becomes
`management_stage=warmup`, not reproductive status. `M0` becomes
`reproductive_status=mother` plus `management_stage=m0_post_delivery`, not a
growth or genetics category.

Import conflict rule: if a compound F2 label says one sex but the source
`Gender` column says another, Goat OS must preserve both values and send the row
to review. Example: `Gender=Female` plus `F2-Male` is a review case; the F2
label must not override the Gender column. If `Gender` is blank and only the F2
label implies sex, sex stays needs-review.

Genetics/R&D answer: genetics should use structured fields such as breed, sex,
age/date of birth, growth cohort, management stage, health history,
reproductive history, parentage, growth events, and later meat-yield feedback.
It must not infer genetics from one raw legacy label like `F2-Male`.

**Core site-code meanings**

Meaning: CBE/CPT/HF were unclear labels in Sheets and dashboards.

Locked answer: CBE is Coimbatore farm/location. CPT is Channapatna farm/location. HF means Holding Farm, an external agent/partner holding place used during procurement/warm-up. Origin Farm is source/origin evidence, not current location by itself.

**HF partner modeling**

Meaning: HF partners are real external partner/source names. Current business
answer says partner-held goats can be shared/pending while Mesha has paid an
advance but not the full amount.

Locked answer: create minimal external party records for known HF partners in
Phase 1. Create location records only where source evidence indicates a physical
holding place. Do not assume full Mesha ownership or full partner ownership from
the HF label. Use shared/pending/review ownership state unless stronger source
evidence exists.

**Current location conflict**

Meaning: RFID DB and latest DB event should ideally agree on current shed/location.
Ops confirmed RFID DB shed association can be stale because goats are shifted
constantly.

Locked answer: if RFID DB shed and latest DB event shed disagree, use the latest
DB event as the current placement signal, preserve the RFID shed as source
evidence, and create a reconciliation note/review item for the disagreement.

**Growth/cohort label meanings**

Meaning: labels like K0/K1/K2/K3/M0/F2 are operational stages, not goat identity tags.

Locked answer: K0 is newborn with mother for maximum about one day; K1 is milk
training for maximum about seven days; K2 is milk drinking after training for
about 42 days / six weeks; K3 is weaning to solid feed; M0 is mother
post-delivery; F2-Male/F2-Female are post-weaning fattening group labels. F2 is
the fattening stage; Male/Female is a legacy grouping suffix, not authoritative
sex evidence. `F0` is not used. Warmup can happen during source holding and as
about a 14-day park transition diet after arrival.

### Future Ops Inputs Not Blocking Phase 1

These are not technical architecture questions. They are places where current
legacy data uses business words/codes that only operations can correctly
interpret.

**Status label semantics**

Current legacy state: labels include `K0/K1/K2/K3`, `Pregnant`, `Non-Pregnant`, `Mother`, `Milking`, `Buck`, `F2-Male`, `F2-Female`, `ICU`, `ICU-Non-Pregnant`, `Quarantine kids`, and others.

Known from Drive source docs: K0/K1/K2/K3/M0/F2/Warmup meanings are captured in
the glossary. ICU/serious illness and Quarantine/viral disease restrict
sale/allocation. Kids at K3 or below are milk-drinking kids and must not be sold.
Future medication withdrawal periods should block sale once medicine tracking is
implemented.
Future feed-contamination clearance, promised-weight risk, and booking-date
price audit are also sale/allocation safety inputs. A substitute/replacement
goat must pass the same sale/allocation checks as a fresh booking, not bypass
them.

Future sale/allocation phases must also keep checking open promises after the
initial booking. If a promised goat later gets sick, moves into a risky feed
window, receives medicine, loses weight, is merged into another identity, or
otherwise becomes unsafe before delivery/dispatch, Goat OS must create a
remediation/replacement task and notify the responsible team. Phase 1 only
stores the evidence and identifiers needed for that later monitoring loop.

Known from legacy code: health diagnosis and follow-up tasks are driven by Diagnosis Form, Problem, Follow Up, Adults SOP, and Kids SOP. That flow is disease, adult/kid age group, day, and session based. It is not mainly driven by status labels like K0/K1/K2/Pregnant.

Known routine work seeds: K and F kids are weighed every Monday. Adult goats are
weighed once monthly, currently on the 15th. Vaccinations cover all goats by
schedule. Feed changes depend on configured experiments.

Phase 1 handling: store the raw legacy label, map known values into
lifecycle/reproductive/growth/management/health axes, and keep unknown or dirty
labels reviewable. Do not hardcode sale-blocking or task-trigger behavior in
Phase 1.

Needed for later policy phases: turn the known sale blockers, weighing rhythms,
vaccination schedules, medicine withdrawal periods, feed-contamination
clearance, promised-weight risk, booking-date price audit, and experiment-driven
feed changes into configurable `status_rule_policies` / SOP task rules.

**Geo details**

Current legacy state: CBE and CPT are physical farm/location codes; HF values are external holding/source places. Current answer says each known site has one physical site for now, and there are no privacy concerns with storing address/GPS once available.

Phase 1 handling: create location rows with known code/city/state and nullable pincode/coordinates. Unknown exact address/GPS must not block import.

Needed later: official address, district, pincode, coordinates, and timezone for CBE/CPT and any HF location that should be represented as a physical location. Future same-city parks must get distinct site codes/location rows rather than overloading one code.
