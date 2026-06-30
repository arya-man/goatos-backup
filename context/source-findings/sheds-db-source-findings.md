# Sheds DB Source Findings

Date reviewed: 2026-06-30

Sources reviewed:

- `wiki/Sheds DB.xlsx` (`DB` sheet, root wiki copy modified 2026-06-30 12:00 IST)
- `wiki/General/Sheds DB.xlsx` compared with the root wiki copy on 2026-06-30;
  both files had the same SHA-256 at review time
- Operator chat note from 2026-06-30: current values are updated manually to
  match ground reality; desired future workflow is scheduled actions around
  birth, breeding, and other goat events, with shiftings performed accordingly.

Status: sanitized source finding for GoatOS Locations/Parks, Counts/Shifting,
Feed Direction, PHC, Procurement, Breeding, SOP, and command-lens consumers. Do
not commit the raw workbook, Google Sheet URL, screenshots, or private media.

## Source Shape

`Sheds DB.xlsx` is a legacy/manual shed profile matrix, not a formula model.
The current root wiki workbook has one sheet named `DB`, no formulas in the
reviewed cells, and a two-row header:

| Column group | Meaning observed |
| --- | --- |
| `Shed` | Human shed/section label such as Gandhi, Castro, Ho Chi Minh, Q, Yashoda, Mandela, Godel, and Sumathi rows/parts |
| `CBE Tags`, `CPT Tags` | Park-specific operational shed tag or cohort role |
| `CBE Capacity`, `CPT Capacity` | Park-specific capacity-like numeric value |
| `CBE Total Area (sq ft)`, `CPT Total Area (sq ft)` | Park-specific area field, mostly blank in the reviewed workbook |
| `CBE Potential Tags`, `CPT Potential Tags` | Candidate/future tags, often used for kid/ICU-style possibilities |

Observed tags include `Non-Pregnant`, `F2-Male`, `F2-Female`, `Buck`, `Mother`,
`ICU-Kid`, `ICU`, `K0`, `K1`, `K2`, `K3`, `K1/K2`, and `Warmup`. The exact
spelling, casing, spacing, and blanks are legacy source values requiring alias
review before runtime use.

The reviewed workbook has 1,013 data-row slots. Most sheet rows are blank
capacity/profile slots; populated values are concentrated in current shed
assignments. Observed aggregate shape at review time:

- CBE: 70 populated tag rows, 71 populated capacity rows, 9 unique non-blank
  tags, and roughly 1,116 total capacity units across populated tagged rows.
- CPT: 45 populated tag rows, 44 populated capacity rows, 9 unique non-blank
  tags, and roughly 725 total capacity units across populated tagged rows.
- `Total Area (sq ft)` and `Potential Tags` are sparse, especially for CPT.

Treat those aggregates as source evidence only. They help size fixtures and
validation checks, but they do not make raw Sheds DB rows runtime truth.

## Base Rule

Sheds DB values are source evidence for location profiles, allowed/target shed
tags, capacity-like limits, and candidate tag possibilities. They are not GoatOS
runtime truth by themselves.

GoatOS must model this as governed, effective-dated location/profile data:

- location identity and park scope are canonical GoatOS records;
- tags and capacity are reviewed source-backed profile attributes;
- changes are auditable, effective-dated, and do not silently mutate already
  generated Feed Directions, SOP obligations, or historical counts;
- blanks, conflicting tags, and dirty source spellings create review work;
- admins may propose and publish profile changes through source-backed CRUD,
  but unpublished/manual edits cannot become runtime truth silently.

## Workflow Implications

The current manual process is: someone updates Sheds DB so values match ground
reality. GoatOS should replace that with event-driven and reviewed workflows:

- birth/kidding can create kid/mother stage work and candidate shed moves;
- breeding and pregnancy confirmation can schedule reproductive-stage review,
  buck/doe separation, pregnancy/lactation capacity checks, and feed-risk work;
- procurement intake can schedule warm-up/holding/accepted-herd transition
  checks before a goat becomes normal park truth;
- health/ICU/quarantine events can block or change allowed location fit, but a
  quarantine-looking shed name alone is not biological quarantine proof;
- ShiftingEvents update actual and projected shed occupancy through the
  Counts/Shifting ledger, not by direct sheet edits;
- Feed Direction must re-resolve destination shed profile/ration context after
  shifting high-risk cohorts such as pregnant, lactating, warm-up, mother, or
  kid groups.

## Feed And Counts Implications

Counts/Shifting still owns physical aggregate counts at park + shed + breed +
horizon. Sheds DB does not replace Base Count or ShiftingEvent truth.

Feed Direction uses Sheds DB-derived profile data only after review/publish to
resolve the physical count row into a nutrition/ration context. If a pregnant or
warm-up group is shifted into a destination shed and the destination profile,
capacity, or ration context is missing or conflicting, Feed generation must
fail closed with visible exception work. GoatOS must not underfeed high-risk
animals, and must not overfeed by adding hidden safety buffers that create moist
feed, refusal-to-eat, sickness, or wastage risk.

## Product Surface Implications

Parks/Sheds/Locations need a governed profile surface, not a raw spreadsheet
clone. The surface should support:

- shed profile CRUD with review/publish state and source evidence;
- effective-dated tags, capacity, potential tags, and fit-for-purpose notes;
- alias cleanup for legacy tags and spacing variants;
- impact preview showing Feed, PHC, Procurement, Breeding, SOP, Calendar, Action
  Center, Control Tower, and Protocol Adherence effects before publish;
- exception work when current occupancy, scheduled shifting, or profile changes
  exceed capacity or violate high-risk cohort fit.

## Relationship To Other Sources

`context/source-findings/goats-and-parks-source-findings.md` remains the base
source for goat/park/stage/feed-safety semantics. `Sheds DB.xlsx` adds the
operational matrix showing how current shed tags and capacities are maintained.
When the two conflict, record a reviewed source conflict and block runtime
behavior until the owner resolves it.

`Feed, Shiftings and Count.docx` remains the controlling source for Feed
Direction clocks, Diff, bridge, physical count adoption, one-day projection, and
ration behavior.
