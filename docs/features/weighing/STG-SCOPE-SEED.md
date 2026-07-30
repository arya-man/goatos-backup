# Staging Weighing Scope Seed

## Purpose

This seed appends the park and shed choices used to plan **weighing only** in
`goatos-stg`. It must not alter vaccination, counts, goat identity, goat
location, or any other feature. Staging is not cleaned before this seed.
Every write must be an idempotent insert/upsert scoped to the records declared
here; unrelated existing rows must remain unchanged.

Use only the live roles in `docs/runbooks/current-active-rbac-roles.md`.
Composite catalog roles such as `director_breeding` and
`director_preventive_care` are dormant scaffolding today; do not grant them for
this STG weighing seed.

Do not run this seed as part of a local or production migration. Run it
explicitly against staging after verifying the active Google Cloud project,
database target, tenant, and operator.

## Park and shed matrix

The seed must upsert both park records. CBE may not exist in staging yet and
must be added before its weighing scopes.

| Park | Weighing mode | Scope range | Expanded count |
| --- | --- | --- | ---: |
| CBE | Lump-sum | Castro 1 through Castro 3 | 3 |
| CBE | Lump-sum | Yashoda 1 through Yashoda 10 | 10 |
| CPT | Lump-sum | Castro 1 through Castro 2 | 2 |
| CPT | Lump-sum | Godel 2 Part 1 through Godel 2 Part 2 | 2 |
| CPT | Individual | Mandela 1 Part 1 through Mandela 1 Part 10 | 10 |
| CPT | Individual | Mandela 2 Part 1 through Mandela 2 Part 10 | 10 |

Expected total: **37 active weighing scopes**: 13 in CBE and 24 in CPT.

This is the corrected WhatsApp scope for the 2026-07-29 STG trial. Older
expanded lists that included CBE Godel 1, full CBE Godel 2, or broader CPT
ranges are not part of the active STG weighing picker for this trial.

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

## Assignment examples

One weighing campaign is the park/week shell. Each selected shed row is the
operator-owned bucket. Do not create two active campaigns for the same
park/week just to split operators.

Example CBE campaign for 2026-07-29:

| Shed bucket | Mode | Assigned operator | What the operator sees |
| --- | --- | --- | --- |
| Yashoda 1 | Individual | Kumar Sharath | Only Yashoda 1 under the CBE task |
| Yashoda 2 | Individual | Pramod | Only Yashoda 2 under the same CBE task |
| Yashoda 3 | Lump-sum | Dinakar | Only Yashoda 3 under the same CBE task |

Director/CEO users can see the full campaign and all shed buckets. Operators
see only the shed buckets assigned to their Firebase user id.

Do not assign the same individual shed bucket to two operators in v1. If Yashoda
1 needs help, keep Kumar as the owner and let helpers physically assist him; the
app writes the bucket under Kumar. If a second operator needs app ownership,
assign a different shed bucket such as Yashoda 2.

The `planned_cap_per_day` default is 100 animals per operator per business day.
It is planning guidance, not a hard submit limit. A shed remains atomic: if
Yashoda 1 has 130 animals, do not split Yashoda 1 into two app buckets. The
unfinished rows remain open and roll forward until the shed bucket is submitted
or leadership cancels/closes it.

## Staging people and access

The same staging append must ensure the following people can sign in with the
documented STG password convention and see only the modules listed below.
It must not grant Counts. Counts is temporarily inactive for operator module
grants.

| Park | Person | App role | Business title / department | Login | Modules |
| --- | --- | --- | --- | --- | --- |
| CPT | Amit Kumar | Operator | Preventive Care | `amit797069@gmail.com` | Vaccination, Weighing |
| CPT | Darshan Talwar | Operator | Preventive Care | `darshantalawar033@gmail.com` | Vaccination, Weighing |
| CPT | Sagar Mahoor | Operator | Preventive Care | `sagarmahoor143@gmail.com` | Vaccination, Weighing |
| CBE | Pramod | Operator | Weighing Operations | `pramodsahu616285@gmail.com` | Weighing only |
| CBE | Kumar Sharath | Operator | Weighing Operations | `kumarsharath95279@gmail.com` | Weighing only |
| Both | Dinakar | `growth_director` | Growth Director | `babureddy315@gmail.com` | Weighing only |

Eshwar remains intentionally excluded from this STG weighing grant until his
exact email/Firebase UID/access requirement is confirmed.

The seed must:

1. Preserve existing Firebase identities and workforce rows.
2. Match by permanent Firebase UID, not mutable display name.
3. Append or reactivate the required operator/director scope grants.
4. Grant `vaccination` to Preventive Care director/users, `weighing` to Growth
   Director / Weighing Operations users, and never grant both director feature
   modules by accident.
5. Bind operator access to the listed park and its assigned sheds.
6. Never grant a real operator tenant scope. Operator execution belongs to a
   park plus assigned shed/task buckets: Amit/Darshan/Sagar -> CPT;
   Pramod/Kumar Sharath -> CBE. Director visibility may span both parks, and
   `growth_director` may scan/submit weighing where the backend assigns
   executable work. Run
   `make stg-operator-scope-guard` after any seed/grant edit.
7. Dinakar uses `growth_director`, not an `operator` grant. He can see both parks
   and execute Weighing only; he does not add operator capacity.
8. Leave all unrelated users, roles, departments, parks, sheds, and grants
   unchanged.
9. Fail before writing if Pramod, Kumar Sharath, or Dinakar lacks a confirmed
   Firebase UID or if a requested park/shed cannot be resolved uniquely.

## Staging verification

Before writing, verify the active account is `ravi@mesha.sg`, the Google Cloud
organization is `vgoats.com`, the project is `goatos-stg`, and the database is
the Goat OS staging database.

After seeding, verify:

1. CBE has 13 active weighing scopes and CPT has 24.
2. All 37 scopes appear in the weighing planner.
3. None appear because of vaccination or expected-animal roster membership.
4. Capturing an unknown RFID succeeds without a `goats` or
   `goat_identifiers` row.
5. Capturing the same RFID in two different weighing scopes succeeds in both.
6. No non-weighing tables change during either capture.
7. Counts is absent from Android for operators, directors, and leadership.
8. Amit, Darshan, and Sagar see Vaccination and Weighing.
9. Pramod, Kumar Sharath, and Dinakar see Weighing only.
10. Eshwar receives no grant from this seed.
