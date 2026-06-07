# Goat OS Glossary

This glossary defines business terms, legacy labels, and operating codes that
appear in Goat OS docs, source data, dashboards, Slack flows, and migration
plans. If a term is not confirmed, mark it as pending instead of guessing.

## Legacy Farm And Source Codes

### CBE

Known meaning:

- Ops-confirmed farm/site code for the Coimbatore farm/location.
- The code follows the railway/location-code convention used internally.
- Older source data can use `CJB` for the same Coimbatore park.
- Found in current Sheets, dashboard filters, and Phase 1 discovery.
- Treated as one of the core Goat OS operating locations in current data.

Pending confirmation:

- Official legal/location name, address, district, pincode, latitude/longitude, and timezone.

Build rule:

- Store `CBE` as a physical location/site code during migration.
- Do not use CBE as an owner or custodian party by itself.
- Attach official geo details only after confirmation.

### CPT

Known meaning:

- Ops-confirmed farm/site code for the Channapatna farm/location.
- The code follows the railway/location-code convention used internally.
- Older source data can use `BLR` for the same Channapatna park.
- Found in current Sheets, dashboard filters, and Phase 1 discovery.
- Treated as one of the core Goat OS operating locations in current data.

Pending confirmation:

- Official legal/location name, address, district, pincode, latitude/longitude, and timezone.

Build rule:

- Store `CPT` as a physical location/site code during migration.
- Do not use CPT as an owner or custodian party by itself.
- Attach official geo details only after confirmation.

### HF / Holding Farm

Known meaning:

- `HF` means `Holding Farm`.
- Legacy label found as `Holding Farm` and `HF - <name>` in current source data.
- Holding farms are facilities at the source where goats are kept after
  procurement and before dispatch to main parks.
- Confirmed holding period is roughly 2 to 8 weeks post procurement.
- They are used for initial selection, tagging, health SOP, and source-side
  holding before a long journey.
- Examples such as `HF - Rajasthan Farms`, `HF - Gokul Agronomics`,
  `HF - Goat World`, and `HF - Bhopal Agro` are agent/partner company or
  source names, not Mesha-owned core farms.

Pending confirmation:

- Official geo details for each holding farm if it should be represented as a physical location.

Build rule:

- Treat HF as procurement/source/holding context by default.
- Do not treat HF/Holding Farm as final goat ownership truth automatically.
- When Mesha has paid an advance but not the full amount, ownership is
  business-wise shared/pending and should stay reviewable until source evidence
  confirms the owner ledger row.
- Create minimal external party records for known HF partners during Phase 1 so
  imports reference entities, not free-text names.
- If a row says the goat is physically at an HF location, capture it as
  holding-location evidence and route unclear owner/custodian meaning to review.
- If HF appears in an Origin Farm/source column, store it as provenance only; do
  not use it to set current custody or ownership.

### Origin Farm

Known meaning:

- Source/origin value in RFID DB.
- Ops-confirmed meaning: where the goat was purchased from, born, or otherwise
  originally sourced.

Build rule:

- Store Origin Farm as source/origin evidence.
- Do not treat Origin Farm as current physical location, owner, or custodian
  unless another source explicitly proves that relationship.

### No Tag / Tagless

Known meaning:

- Animal has no reliable tag.
- RFID tagging was done recently, so older rows can have no RFID or inconsistent
  tag terminology.

Build rule:

- Do not collapse multiple No Tag/tagless rows into one goat.
- Field-created temporary goats require photo/proof.
- Import-created temporary goats require source-row evidence and stay in review
  until stronger identity is attached.

### Old Tag

Legacy ear tag number used before RFID became the stable identifier. The old tag
number alone is not unique. The confirmed legacy uniqueness scope is:

```text
old_tag_number + park_code
```

Examples:

```text
826 CBE and 826 CPT can be two different goats.
435 CBE and 435 CPT can both be present in Coimbatore after historic movement.
435 CBE cannot occur twice for two different goats.
```

Build rule:

- Never merge by old tag number alone.
- Normalize historic park aliases (`CJB -> CBE`, `BLR -> CPT`) while preserving
  the original source code as evidence.
- Duplicate old tag inside the same normalized park scope goes to review.

## Status And Cohort Labels

Goat OS keeps three separate ideas:

- **Raw legacy label:** exact old value from Sheets/Slack, kept as evidence and
  searchable alias.
- **Canonical code:** clean stored code used by APIs and analytics, such as
  `K0`, `F2`, `pregnant`, `icu`.
- **Display name:** human UI label, such as `K0 - Newborn` or
  `F2 - Fattening`.

Compound old labels must be split. Example: `F2-Male` is not a separate
genetics category and must not set sex by itself. It maps to
`growth_cohort_tag=F2`; sex comes from the source `Gender` column when present.
`ICU-Non-Pregnant` maps to `health_status=icu` and
`reproductive_status=non_pregnant`.

Legacy code sometimes defaulted unknown gender into an F2 sex label, so
`F2-Male`/`F2-Female` is a contaminated sex signal. If a compound F2 label
disagrees with the source `Gender` column, preserve both pieces of evidence and
route the row to review. If `Gender` is blank and only an F2 label implies sex,
keep sex as needs-review instead of inferring it from the label.

### K0

Baby goat stage immediately after birth, with newborn kids kept with the mother
for a maximum of about one day.

### K1

Baby goat stage where the kid is separated from the mother and trained to drink
from the milk feeding system, for a maximum of about seven days.

### K2

Baby goat stage after milk training where the kid drinks milk freely. Confirmed
duration is about 42 days / six weeks.

### K3

Weaning stage where milk is gradually stopped and the kid is moved to solid feed.

### M0

Mother-goat post-delivery stage. The mother has delivered recently and is managed
as a mother/post-delivery animal rather than as a kid growth cohort.

Canonical mapping:

```text
reproductive_status = mother
management_stage = m0_post_delivery
short label = M0
display name = M0 - Post-delivery mother
```

M0 is not a growth cohort and not a genetics category by itself. It is a
post-delivery management stage for a mother.

### F2-Male / F2-Female

Fattening groups after K3/weaning. `F2` is the growth/fattening stage; the
`Male`/`Female` suffix is a legacy grouping label and is not authoritative sex
evidence. Goat OS uses the source `Gender` column for sex and treats missing or
conflicting sex evidence as reviewable. The goal is strong feed performance and
average daily gain.

`F0` is not currently used.

### ADG

Average Daily Gain. Growth-performance measure used during fattening and R&D.
Ops mentioned a target around 200g+ daily gain for strong fattening performance.

### Warmup

Adaptation period before the goat enters normal farm flow.

- Source warm-up: goats are held at the source/holding farm after purchase,
  usually as part of the 2 to 8 week holding period before travel.
- Destination warm-up: goats adapt again after arriving at CBE/CPT or another
  destination farm, especially to local climate and feed. Park warmup is
  typically about 14 days.

## Goat OS Identity Terms

### Source Row

One exact row from the old Excel/CSV/Slack export. Goat OS keeps this as
evidence for where an imported fact came from.

### Load

A batch/group of goats that came together, usually through purchase, transport,
or shifting.

### Tenant

The top-level Goat OS data boundary. Today this is Mesha. Later it can separate
another company/franchise without mixing data.

### Party

A person, company, farm operator, vendor, lender, investor pool, or system
account that Goat OS can refer to.

### Owner

The party that owns the goat's economic value. Today this is Mesha for imported
goats unless source evidence proves otherwise.

### Custodian

The party responsible for taking care of the goat operationally. Today this is
Mesha unless source evidence proves otherwise.

### Staging Party / Staging Location

A safe temporary bucket for messy rows when Goat OS cannot yet prove the real
owner, custodian, or location. Staging is review state, not final truth.

### Current Location

Where the goat physically is now: farm, park, shed, or cohort.

### Display ID

The human-visible goat code. It is different from the internal immutable
`goat_id`.

### Merge

Combining two goat records when they are proven to be the same real goat. This
is risky and needs approval because a wrong merge corrupts identity, health,
vaccination, genetics, and sale history.
