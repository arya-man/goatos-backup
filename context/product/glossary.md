# Goat OS Glossary

This glossary defines business terms, legacy labels, and operating codes that
appear in Goat OS docs, source data, dashboards, Slack flows, and migration
plans. If a term is not confirmed, mark it as pending instead of guessing.

## Legacy Farm And Source Codes

### CBE

Known meaning:

- Legacy farm/site code found in current Sheets, dashboard filters, and Phase 1 discovery.
- Treated as one of the core Goat OS operating locations in current data.

Pending confirmation:

- Official full name, address, district, pincode, latitude/longitude, and timezone.
- Whether CBE is only a physical location, or also maps to a custodian party in some legacy rows.

Build rule:

- Do not hard-code an expansion for `CBE`.
- Store it as a location/code during migration and attach official geo/business details only after confirmation.

### CPT

Known meaning:

- Legacy farm/site code found in current Sheets, dashboard filters, and Phase 1 discovery.
- Treated as one of the core Goat OS operating locations in current data.

Pending confirmation:

- Official full name, address, district, pincode, latitude/longitude, and timezone.
- Whether CPT is only a physical location, or also maps to a custodian party in some legacy rows.

Build rule:

- Do not hard-code an expansion for `CPT`.
- Store it as a location/code during migration and attach official geo/business details only after confirmation.

### HF / Holding Farm

Known meaning:

- Legacy label found as `Holding Farm` and `HF - <name>` in current source data.
- Appears to represent goats held outside the core CBE/CPT buckets, often with vendor/source/procurement context.

Pending confirmation:

- Whether each HF row means owner party, custodian party, procurement staging, vendor/source context, physical holding location, or a mix by case.
- Official geo details for each holding farm if it is a real location.

Build rule:

- Do not treat HF/Holding Farm as final ownership or custody truth automatically.
- Import ambiguous HF rows into staging/review unless a confirmed mapping exists.

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
