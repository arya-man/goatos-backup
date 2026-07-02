# Goat OS Glossary

This glossary defines business terms, legacy labels, and operating codes that
appear in Goat OS docs, source data, dashboards, Slack flows, and migration
plans. If a term is not confirmed, mark it as pending instead of guessing.

Base goat and park semantics come from
`context/source-findings/goats-and-parks-source-findings.md`. Use that source for
identity/tag/RFID, shed tags, lifecycle/stage, breed labels, pregnancy,
lactation, warm-up, fattening, feed safety, weighing, handling, medicine
administration, park roles, and feed session terms before adding glossary
aliases or changing a definition.

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
- Source documents said the intended/optimistic holding period is roughly 2 to 8
  weeks post procurement, but the 2026-06-25 operator clarification supersedes
  using that as a hard cap: breeding source warmup is realistically 45-70 days
  today, while fattening/non-breeding cases may be 0 days or around 2 weeks.
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
- Store actual source warmup start/end/days per load/goat/purpose. Do not hard
  code one universal warmup duration into validation, vaccination eligibility, or
  UI copy.
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

### Animal External Identifiers

Every canonical herd animal has one internal immutable `animal_id` plus two
required external field/business identifiers:

```text
animal_identifier_1
animal_identifier_2
```

These apply to goats, sheep, and future species. They are parallel identifiers,
not old/new IDs. Product copy, APIs, canonical DB columns, imports after review,
and vaccination matching must call them Animal ID 1 and Animal ID 2, or the exact
snake-case field names above.

Identifier values are globally single-use for life:

```text
identifier_value -> exactly one animal_id ever
```

No park, shed, source sheet, species, death, sale, transfer, tag breakage, or tag
loss releases the value. If a value ever belonged to an animal, it remains tied
to that animal in history forever and cannot be assigned to another animal.

Build rule:

- Never merge by one external identifier value alone.
- Every accepted/canonical herd animal must have both `animal_identifier_1` and
  `animal_identifier_2`, and those two current values must be different.
- Duplicate checks run against all current and historical identifier rows. Any
  match means the value is already owned by that animal; a new animal using it
  is invalid source data and must be rejected/fixed, not parked for later.
- If a physical tag falls off or breaks, mark the old value as broken/retired in
  identifier history. The animal keeps operating through the surviving
  identifier while a replacement is pending.
- Replacement uses a brand-new globally unused identifier value, fills the
  vacant slot, and writes an audit event with actor, time, reason, old value,
  and new value. The broken/fallen value is never refitted or reissued.
- Raw legacy source column names are stored only as import provenance. They must
  not become canonical field names, UI labels, or rule-selector names.

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

Legacy code sometimes defaulted unclear gender into an F2 sex label, so
`F2-Male`/`F2-Female` is a contaminated sex signal. If a compound F2 label
disagrees with the source `Gender` column, preserve both pieces of evidence and
reject/block the row before canonical creation. If `Gender` is blank and only an
F2 label implies sex, do not infer sex from the label; reject/block the row until
the real sex is corrected.

## Animal And Breed Terms

### Kid

Baby goat. Male and female newborn goats are both called kids.

### Adult Female

Fully grown female goat.

### Buck

Adult male goat kept separately and used for breeding. Buck assignment is a
breeding/genetics decision, not just a sex label.

### Fattening Kid

Weaned young goat raised for meat on a high-nutrition diet. Fattening is a
management/growth stage, not a breed or genetics category.

### Known Breed / Species Labels

Reference labels found in current farm material and legacy dashboard constants:

```text
Malai
Beetal
Sojat
Osmanabadi
Boer
Anantapur Sheep
Anantapur
Kenguri
```

Goat OS must store breeds/species as reference data with aliases, not as
hardcoded dropdown strings. Dirty spellings or unknown labels should be
reviewable instead of silently mapped.

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
evidence. Goat OS requires canonical animal sex to be real `female` or `male`;
missing, unclear, or conflicting sex evidence blocks import/creation until
corrected. The goal is strong feed performance and average daily gain.

`F0` is not currently used.

### ADG

Average Daily Gain. Growth-performance measure used during fattening and R&D.
Ops mentioned a target around 200g+ daily gain for strong fattening performance.

### Warmup

Adaptation period before the goat enters normal farm flow.

- Source warm-up: goats are held at the source/holding farm after purchase. The
  actual duration is purpose-dependent: breeding is currently expected around
  45-70 days; fattening/non-breeding may be immediate/0 days or around 2 weeks;
  2 to 8 weeks remains an optimistic/target planning range, not a validation cap.
- Destination warm-up: goats adapt again after arriving at CBE/CPT or another
  destination farm, especially to local climate and feed. Park warmup is
  typically about 14 days.

## Reproduction Terms

### Estrus / Heat

The period when a female goat is sexually receptive and can be bred.

Current operating assumptions from farm material:

```text
duration: typically 12-48 hours, average about 24 hours
goat cycle frequency: about every 21 days
sheep cycle frequency: about every 17 days
visible signs: restlessness, loud bleating, tail wagging, reduced appetite,
  sometimes white discharge
```

### Estrus Synchronization

Process of aligning heat cycles so breeding and delivery windows become more
predictable. Current farm material mentions progesterone sponges kept in the
vagina for about 14 days as one synchronization method.

### Natural Breeding

Buck naturally mates with the female. Current planning ratio is about one buck
for every five females, with at least two days rest before using the buck again.

### Artificial Insemination

Semen is deposited manually. Current farm material says diluted fresh semen can
be used for up to about 100 females within about three days after collection.
Treat this as a configurable breeding protocol, not a hardcoded universal rule.

### Gestation

Pregnancy period. Current goat planning value is about 150 days / five months
from breeding to delivery.

Pregnancy confirmation:

```text
ultrasound can start around 45 days after breeding
scans around 3-4 months become harder
```

### Anestrus

Period after delivery or during off-season when a goat does not return to normal
heat cycles. Current farm material says that if it is not seasonal, estrus is
expected to resume around 60 days after delivery.

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
