# Staging Weighing Scope Seed

## Purpose

This seed appends the park and shed choices used to plan **weighing only** in
`goatos-stg`. It must not alter vaccination, counts, goat identity, goat
location, or any other feature. Staging is not cleaned before this seed.
Every write must be an idempotent insert/upsert scoped to the records declared
here; unrelated existing rows must remain unchanged.

Do not run this seed as part of a local or production migration. Run it
explicitly against staging after verifying the active Google Cloud project,
database target, tenant, and operator.

## Park and shed matrix

The seed must upsert both park records. CBE may not exist in staging yet and
must be added before its weighing scopes.

| Park | Scope range | Expanded count |
| --- | --- | ---: |
| CBE | Castro 1 through Castro 3 | 3 |
| CBE | Godel 1 Part 1 through Godel 1 Part 8 | 8 |
| CBE | Godel 2 Part 1 through Godel 2 Part 8 | 8 |
| CBE | Yashoda 1 through Yashoda 10 | 10 |
| CPT | Castro 1 through Castro 2 | 2 |
| CPT | Godel 2 Part 1 through Godel 2 Part 4 | 4 |
| CPT | Mandela 1 Part 1 through Mandela 1 Part 4 | 4 |
| CPT | Yashoda 1 through Yashoda 4 | 4 |

Expected total: **43 weighing scopes**: 29 in CBE and 14 in CPT.

The machine-readable source is
`fixtures/weighing-stg-scopes/weighing-scope-seed.json`. Range expansion is
inclusive.

## Isolation contract

These records are selectable weighing scopes, not the canonical herd roster.

- A scope belongs to one park and to the weighing module only.
- Individual weighing is free flow. Any RFID received in an open scope is
  accepted as `scanned_identifier` text.
- An RFID does not need to exist in `goats`, `goat_identifiers`,
  `weighing_expected_animals`, or any shed roster.
- The same RFID may be captured in different weighing sheds. There is no
  cross-shed ownership or location validation in weighing v1.
- Individual captures write only the weighing observation/proof/outbox
  records required for offline-first sync.
- A capture must never create or update a goat, goat identifier, canonical
  location, vaccination roster, vaccination capture, or counts record.
- Duplicate handling is local to the current weighing shed/session. A repeated
  RFID in that scope updates/replaces that scope's active weighing observation
  and proof; it is not a global RFID relationship.
- An unfinished shed restores its already-synced weighing observations from
  the backend after Room loss or reinstall.
- Submit is allowed only when every RFID captured in that shed has a positive
  weight and a completed backend video proof.

Lump-sum weighing uses the same park/scope seed but stores a shed observation:
animal count, total weight, derived average weight, and one to five completed
videos. It does not create individual animal observations.

## Staging people and access

The same staging append must ensure the following people can sign in and see
both Vaccination and Weighing, restricted to their assigned park and sheds.
It must not grant Counts. Counts is temporarily hidden in Android for every
role.

| Park | Person | Role | Login |
| --- | --- | --- | --- |
| CPT | Amit Kumar | Operator | `amit797069@gmail.com` |
| CPT | Darshan Talwar | Operator | `darshantalawar033@gmail.com` |
| CPT | Sagar Mahoor | Operator | `sagarmahoor143@gmail.com` |
| CBE | Pramod | Operator | `pramodsahu616285@gmail.com` |
| CBE | Sharath | Operator | Firebase UID/email must be confirmed before seeding |
| Assigned parks | Dinakar | Director | `babureddy315@gmail.com` |

Eshwar is intentionally excluded.

The seed must:

1. Preserve existing Firebase identities and workforce rows.
2. Match by permanent Firebase UID, not mutable display name.
3. Append or reactivate the required operator/director scope grants.
4. Grant only `vaccination` and `weighing` department modules.
5. Bind operator access to the listed park and its assigned sheds.
6. Bind Director access only to the parks/sheds explicitly assigned to
   Dinakar in the seed input.
7. Leave all unrelated users, roles, departments, parks, sheds, and grants
   unchanged.
8. Fail before writing if Pramod, Sharath, or Dinakar lacks a confirmed
   Firebase UID or if a requested park/shed cannot be resolved uniquely.

## Staging verification

Before writing, verify the active account is `ravi@mesha.sg`, the Google Cloud
organization is `vgoats.com`, the project is `goatos-stg`, and the database is
the Goat OS staging database.

After seeding, verify:

1. CBE has 29 weighing scopes and CPT has 14.
2. All 43 scopes appear in the weighing planner.
3. None appear because of vaccination or expected-animal roster membership.
4. Capturing an unknown RFID succeeds without a `goats` or
   `goat_identifiers` row.
5. Capturing the same RFID in two different weighing scopes succeeds in both.
6. No non-weighing tables change during either capture.
7. Counts is absent from Android for operators, directors, and leadership.
8. Each listed operator sees Vaccination and Weighing only for their assigned
   park/sheds.
9. Dinakar sees Vaccination and Weighing for only his explicitly assigned
   director scope.
10. Eshwar receives no grant from this seed.
