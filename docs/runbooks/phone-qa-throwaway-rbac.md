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

Vaccination is strict and uses five real goat identities:

| Park | Shed | Operator | RFIDs |
| --- | --- | --- | --- |
| CBE | Godel 1 Parts 1-3 | Pramod | `901007000504418`, `901007000504332`, `901007000504407` |
| CPT | Mandela 2 Parts 3-5 | Amit | `901007000504419`, `901007000504392` |

Weighing is free-flow:

| Park | Shed | Operator | Rule |
| --- | --- | --- | --- |
| CBE | Godel 1 Parts 1-3 | Pramod | No expected RFID list, no animal count limit. |
| CPT | Mandela 2 Parts 3-5 | Amit | No expected RFID list, no animal count limit. |

The weighing fixture deliberately sets `expected_animal_count = 0` and inserts no
`weighing_expected_animals` rows. Scanned strings go to weighing observation
tables only; Vaccination herd identity is not loosened.

## Gates To Verify

Navigation visibility is module-gated by `/app/bootstrap`. Execution is
separately gated:

- `vaccination_execute=true`: shed card click opens Vaccination BLE scan and
  submit.
- `weighing_execute=true`: shed card click opens Weighing BLE scan and submit.
- CEO-only PA card stays behind `canViewProtocolAdherenceCard`.
- Verifier sees video review surfaces, not feature execution surfaces.

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
