# Phone QA Throwaway RBAC

Use this when testing Android scan permissions, not when checking the normal local
app seed.

## Two Local Databases

`goatos-local-current` on `127.0.0.1:5433` is the normal local app database. Do
not mutate it for quick phone role tests.

The phone QA database is disposable and should run on `127.0.0.1:15544`. The
phone still calls `localhost:8080`; `adb reverse tcp:8080 tcp:8080` makes that
hit the laptop backend, and that backend must be started with the `15544`
database URL.

## Start And Seed

This fixture is reset-first by design. Do not rerun the seed against an
already-seeded database: the base vaccination seed publishes its protocol, and
published protocol config is immutable. If the test data needs to change, delete
and recreate only the `goatos-phone-qa` container on port `15544`.

```bash
docker rm -f goatos-phone-qa >/dev/null 2>&1 || true
docker run --name goatos-phone-qa \
  -e POSTGRES_PASSWORD=goatos \
  -e POSTGRES_DB=goatos \
  -p 127.0.0.1:15544:5432 \
  -d postgres:16.9-alpine

export DATABASE_URL='postgres://postgres:goatos@127.0.0.1:15544/goatos?sslmode=disable'
export GOATOS_ENV=local

(cd backend && go run ./cmd/migrate -timeout=10m)
tools/local/phone-qa-throwaway-seed.sh
```

Then start the API on `127.0.0.1:8080` with the same `DATABASE_URL`, install the
app, and bake a dev token:

```bash
GOATOS_LOCAL_USER_ID=90000000-0000-4000-8000-000000000102 \
DATABASE_URL="$DATABASE_URL" \
GOATOS_ENV=local \
tools/local/phone-qa-throwaway-run.sh
```

## Test Users

| Person | User ID | Role | Scope | Expected app behavior |
| --- | --- | --- | --- | --- |
| CEO QA | `90000000-0000-4000-8000-000000000101` | `ceo_internal` | tenant | Sees all modules and PA card; can monitor close/reopen paths. |
| Chandrakant | `90000000-0000-4000-8000-000000000102` | `pc_director` | tenant | Vaccination only; shed card opens BLE scan and submit. No Weighing module. |
| Dinakar | `90000000-0000-4000-8000-000000000103` | `growth_director` | tenant | Weighing only; shed card opens BLE scan, submit, and reopen. No Vaccination module. |
| Jyothi | `90000000-0000-4000-8000-000000000104` | `verifier` | tenant | Video review only. No scan/submit. |
| Amit | `90000000-0000-4000-8000-000000000201` | `operator` | CPT park | CPT Vaccination/Weighing execution only. |
| Pramod | `90000000-0000-4000-8000-000000000202` | `operator` | CBE park | CBE Vaccination/Weighing execution only. |
| Kumar Sharath | `90000000-0000-4000-8000-000000000203` | `operator` | CBE park | Spare CBE operator for reassignment tests. |

Operators never receive tenant scope in this fixture. They get a park-scoped
`operator` grant and module access through the `operations` department.

## Seeded Work

Vaccination is strict and uses ten goat identities across two parks. This
fixture exists because the maintainer only has five physical RFID tags but needs
to exercise two sheds per park on a real phone.

Important identity rule:

- `goat_identifiers` is unique by `(tenant_id, normalized_value)`.
- The same raw physical RFID must never be assigned to two goats in one tenant.
- The fixture therefore keeps CBE goats on raw RFID values and gives CPT goats
  transformed identifier values prefixed with `CPT-`.
- The Android dev/local build may transform a CPT vaccination scan from
  `901007000504418` to `CPT-901007000504418` before Room roster lookup,
  scan-attempt recording, scan-capture recording, and outbox sync.
- STG/prod builds must not transform RFID input.

Do not add fake duplicate RFIDs to `goat_identifiers` to make a physical test
easier. That would invalidate the exact production identity invariant this
fixture is protecting.

| Park | Shed | Partition | Operator | Vaccination identifiers |
| --- | --- | --- | --- | --- |
| CBE | Godel 1 | `whole` | Pramod | `901007000504418`, `901007000504332` |
| CBE | Yashoda 1 | `Parts 1-3` | Pramod | `901007000504407`, `901007000504419`, `901007000504392` |
| CPT | Mandela 2 | `whole` | Amit | `CPT-901007000504418`, `CPT-901007000504332` |
| CPT | Castro 1 | `Parts 1-3` | Amit | `CPT-901007000504407`, `CPT-901007000504419`, `CPT-901007000504392` |

This intentionally covers both shed shapes:

- a normal shed with no split partition, represented by `partition_label =
  'whole'`
- a partitioned shed, represented by `goat_shed_partitions.partition_label` and
  `vaccination_drive_assignments.partition_label`

Location names must stay clean: `Godel 1`, `Yashoda 1`, `Mandela 2`,
`Castro 1`. Do not concatenate park + shed + partition into
`locations.name`. Park chips/filters should come from the parent park, and
partition text should come from the assignment/partition fields.

Weighing is free-flow:

| Park | Shed | Partition display | Operator | Rule |
| --- | --- | --- | --- |
| CBE | Godel 1 | `whole` | Pramod | No expected RFID list, no animal count limit. |
| CBE | Yashoda 1 | `Parts 1-3` | Pramod | No expected RFID list, no animal count limit. |
| CPT | Mandela 2 | `whole` | Amit | No expected RFID list, no animal count limit. |
| CPT | Castro 1 | `Parts 1-3` | Amit | No expected RFID list, no animal count limit. |

The weighing fixture deliberately sets `expected_animal_count = 0` and inserts no
`weighing_expected_animals` rows. Scanned strings go to weighing observation
tables only; Vaccination herd identity is not loosened.

Weighing must keep the raw physical RFID text. Do not apply the CPT vaccination
RFID transform to Weighing. Weighing is a free-flow bucket system: the same raw
RFID may be captured under any weighing shed bucket because those rows are
weighing observations, not herd identity ownership.

## Android RFID Transform Boundary

The local phone test transform belongs at the Vaccination scan entry point, not
inside Room DAOs, sync dispatchers, backend validation, or Weighing.

Vaccination flow today:

```text
RFID reader
  -> ScanViewModel.onTagRead(raw tag)
  -> effective vaccination tag for local dev CPT sheds
  -> normalize(effective tag)
  -> Room roster lookup
  -> Room scan attempt
  -> Room scan capture
  -> outbox
  -> backend revalidation
```

The same effective tag must be used for roster lookup, scan-attempt payload,
scan-capture payload, idempotency key, and backend sync. If the transform is
applied only to the API payload or only to the Room lookup, local state and sync
state will diverge.

Production-safe naming rule:

- shared production source may use neutral names such as `RfidInputTransform`
- shared production implementation must be no-op
- dev/local implementation may contain the CPT fixture mapping
- do not use `QA`, `test`, or `alias` in shared production-facing names
- do not duplicate `ScanViewModel` or any feature ViewModel between source sets

## Gates To Verify

Navigation visibility is module-gated by `/app/bootstrap`. Execution is
separately gated:

- `vaccination_execute=true`: shed card click opens Vaccination BLE scan and
  submit.
- `weighing_execute=true`: shed card click opens Weighing BLE scan and submit.
- CEO-only PA card stays behind `canViewProtocolAdherenceCard`.
- Verifier sees video review surfaces, not feature execution surfaces.

Director presentation expectation:

- CEO views are data/monitoring first and can tolerate filter controls.
- Director views are work-first; prefer obvious park chips (`CBE`, `CPT`) and
  visible work cards over hidden filter drawers.
- Operators should see only their park-scoped assigned work.
- Directors are tenant-scoped for their module, so they may see both parks but
  should be able to switch park context with chips.

## Weighing Completion And Reopen

Operator or Growth Director can submit a weighing shed. Once completed, the
operator cannot continue scanning that shed.

CEO or Growth Director can reopen the completed weighing shed. Reopen keeps old
observations and videos; it changes the shed status back to `in_progress` so the
assigned operator or Growth Director can scan more.

FCM/event direction:

- Operator completes weighing shed: notify Growth Director and CEO.
- Growth Director or CEO reopens weighing shed: notify the assigned operator,
  Growth Director, and CEO.
- Verifier review verdicts remain verifier-owned; leaders can see verdict state.
