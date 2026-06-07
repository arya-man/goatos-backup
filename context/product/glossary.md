# Goat OS Glossary

This glossary defines business terms, legacy labels, and operating codes that
appear in Goat OS docs, source data, dashboards, Slack flows, and migration
plans. If a term is not confirmed, mark it as pending instead of guessing.

## Legacy Farm And Source Codes

### CBE

Known meaning:

- Ops-confirmed farm/site code for the Coimbatore farm/location.
- The code follows the railway/location-code convention used internally.
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
- Holding farms are external agent/partner places used after purchase and before
  transport or farm intake.
- They are used for source-side warm-up, usually before a long journey.
- Examples such as `HF - Rajasthan Farms`, `HF - Gokul Agronomics`,
  `HF - Goat World`, and `HF - Bhopal Agro` are agent/partner company or
  source names, not Mesha-owned core farms.

Pending confirmation:

- Official geo details for each holding farm if it should be represented as a physical location.
- Whether each named HF partner should be modeled as a separate external party
  during Phase 1 import, or kept as source context first and promoted later.

Build rule:

- Treat HF as procurement/source/holding context by default.
- Do not treat HF/Holding Farm as final goat ownership truth automatically.
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

## Status And Cohort Labels

### K0

Baby goat stage immediately after birth, usually the first one to two days.

### K1

Baby goat stage where the kid is trained to drink bottle milk.

### K2

Baby goat stage of roughly two months where the kid receives milk and is trained
to eat solid feed.

### K3

Weaning stage where milk is gradually stopped and the kid is moved to solid feed.

### M0

Mother-goat post-delivery stage. The mother stays in this stage for some time,
typically around one month after giving birth.

### F2-Male / F2-Female

Fattening groups after K3/weaning, separated by sex. The goal is strong feed
performance and average daily gain.

### ADG

Average Daily Gain. Growth-performance measure used during fattening and R&D.
Ops mentioned a target around 200g+ daily gain for strong fattening performance.

### Warmup

Adaptation period before the goat enters normal farm flow.

- Source warm-up: goats are held at the source/holding farm after purchase,
  often before travel.
- Destination warm-up: goats adapt again after arriving at CBE/CPT or another
  destination farm, especially to local climate and feed.

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
