# Phone QA Throwaway RBAC

Use this when testing Android scan permissions, not when checking the normal local
app seed.

## Two Local Databases

`goatos-local-current` on `127.0.0.1:5433` is the normal local app database. Do
not mutate it for quick phone role tests.

The phone QA database is disposable and must run on `127.0.0.1:15544`. The
phone still calls its own `localhost:8080`, but the sanctioned runner maps that
device port to a non-default laptop API port: `GOATOS_PHONE_QA_PORT`, default
`8081`. Never bind phone QA to laptop `8080`; that port is the ordinary dev API
and may point at a local replica of STG-shaped data.

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

The seed is self-contained: it runs `cmd/seed-vaccination-per-goat-qa` itself,
so there is no separate base seed to run first. It ends by asserting that every
alive goat has exactly one active primary `animal_identifier_1` and that all six
QA identities below hold a `user_scope_grants` row with `status = 'active'` --
a pending-only grant produces runtime 403s on the phone. A non-zero exit means
the fixture is not usable; recreate the container rather than patching around it.

Then start the API through the phone-QA wrapper with the same `DATABASE_URL`,
install the app, and bake a dev token:

```bash
GOATOS_LOCAL_USER_ID=90000000-0000-4000-8000-000000000102 \
GOATOS_PHONE_QA_PORT=8081 \
DATABASE_URL="$DATABASE_URL" \
GOATOS_ENV=local \
tools/local/phone-qa-throwaway-run.sh
```

`tools/local/phone-qa-throwaway-run.sh` starts or reuses the laptop API on
`:8081`, maps device `localhost:8080` to laptop `:8081`, and calls
`tools/dev/android-dev-run.sh` with the same host port for token validation.

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

Vaccination is strict and uses 160 goat identities across eight sheds in two
parks (2 parks x 4 sheds x 20 animals/shed). The per-shed animal count is
tunable via `GOATOS_ANIMALS_PER_SHED` (default `20`), passed through
`tools/local/phone-qa-throwaway-seed.sh` to `cmd/seed-vaccination-per-goat-qa
-animals-per-shed`. This fixture exists because the maintainer only has five
physical RFID tags but needs to exercise several sheds per park, each with a
realistic animal count, on a real phone.

Important identity rule:

- `goat_identifiers` is unique by `(tenant_id, normalized_value)`.
- The same raw physical RFID must never be assigned to two goats in one tenant.
- The fixture therefore gives each shed its own PREFIXED copy of the five
  physical tags (slots 1-5), plus 15 more synthetic, non-RFID identities per
  shed (slots 6-20, tag value `SYN006`..`SYN020`, prefixed the same way). Godel
  1 keeps slots 1-5 raw; every other shed prefixes them. 8 sheds x 20 slots =
  160 distinct identities, of which 8 x 5 = 40 are reachable by a real RFID
  scan.
- Only the first 5 animals in any shed can actually be walked with the phone's
  5 physical tags. Animals 6-20 in a shed exist so the roster, obligation
  counts, and shed-completion UX are realistically sized, but they are
  deliberately unscannable synthetic identities -- there is no way to make them
  scannable without either inventing fake physical RFIDs (rejected below) or
  owning more real tags.
- The Android dev/local build applies the matching shed prefix to a vaccination
  scan (for example `901007000504418` -> `M2-901007000504418` in Mandela 2)
  before Room roster lookup, scan-attempt recording, scan-capture recording, and
  outbox sync.
- STG/prod builds must not transform RFID input.

Do not add fake duplicate RFIDs to `goat_identifiers` to make a physical test
easier. That would invalidate the exact production identity invariant this
fixture is protecting. In particular, do not re-point the raw physical tags onto
a second park's goats: every alive goat needs exactly one active primary
`animal_identifier_1`, and moving the raw tags leaves the original park's goats
with no vaccination identity at all. The seed asserts both invariants before it
reports success.

| Park | Shed | Partition | Weighing assignee | Vaccination tag prefix |
| --- | --- | --- | --- | --- |
| CBE | Godel 1 | `whole` | Pramod | none (raw) |
| CBE | Yashoda 1 | `Parts 1-3` | Pramod | `Y1-` |
| CBE | Gandhi 1 | `whole` | Dinakar | `G1-` |
| CBE | Gandhi 2 | `whole` | Dinakar | `G2-` |
| CPT | Mandela 2 | `whole` | Amit | `M2-` |
| CPT | Castro 1 | `Parts 1-3` | Amit | `C1-` |
| CPT | Castro 2 | `whole` | Dinakar | `C2-` |
| CPT | Castro 3 | `whole` | Dinakar | `C3-` (lump-sum) |

The five physical tags are `901007000504418`, `901007000504332`,
`901007000504407`, `901007000504419` and `901007000504392`.

This intentionally covers both shed shapes:

- a normal shed with no split partition, represented by `partition_label =
  'whole'`
- a partitioned shed, represented by `goat_shed_partitions.partition_label` and
  `vaccination_drive_assignments.partition_label`

Location names must stay clean: `Godel 1`, `Yashoda 1`, `Mandela 2`,
`Castro 1`, and so on. Do not concatenate park + shed + partition into
`locations.name`. Park chips/filters should come from the parent park, and
partition text should come from the assignment/partition fields.

Weighing is free-flow:

All eight sheds above are weighing sheds. None has an expected RFID list or an
animal count limit; `expected_animal_count` is 0 everywhere. Dinakar owns sheds
in BOTH parks, so every other shed on his Operators list belongs to somebody
else.

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

## Vaccination Neighbor Submit Regression

This is the Godel 1 Part 1 / Part 2 phone regression that caused a submit loop.
The expected behavior is precise:

- The Part 1 submit payload includes the three Part 1 goats and the two accepted
  Part 2 neighbor goats.
- Submit readiness counts expected animals at goat grain, not vaccine-obligation
  grain. Three goats with ET+TT and Sheep Pox still means three expected animals.
- Partition validation must accept the `goat_shed_partitions` fallback when the
  legacy `shed_partitions` catalog is empty.
- The fanout creates one `sop_submission_items` row per scanned goat, then one
  `vaccination_completions` row per matching vaccine obligation. One goat scan
  can therefore close two vaccine obligations.
- Current shed obligations match by task or batch. Neighbor obligations match by
  the accepted `neighbor_partition` scan attempt anchor: either the exact
  obligation, the anchor's batch, or the same goat/protocol/same India business
  date when the obligations are standalone and have no task/batch.
- A future Part 2 obligation closed by a Part 1 neighbor scan must no longer
  appear as future work for Part 2 after the obligation completion cascade runs.
- RFID lookup must work through both active `animal_identifier_1` and
  `animal_identifier_2`.

Run the focused guard before touching the phone:

```bash
make vaccination-neighbor-submit-regression-guard
```

The guard is also wired into the backend lane when Docker/Postgres integration
tests are deliberately enabled:

```bash
GOATOS_RUN_POSTGRES_TESTS=1 make ci-local JOB=backend
```

For the disposable phone-QA database, reset only the Godel vaccination facts
before a replay. Do not use this against `goatos-local-current`:

```bash
export DATABASE_URL='postgres://postgres:goatos@127.0.0.1:15544/goatos?sslmode=disable'
psql "$DATABASE_URL" <<'SQL'
WITH target_task AS (
  SELECT '91000000-0000-4000-8000-000000000702'::uuid AS task_id
), target_goats AS (
  SELECT unnest(ARRAY[
    '91000000-0000-4000-8000-000000001001'::uuid,
    '91000000-0000-4000-8000-000000001002'::uuid,
    '91000000-0000-4000-8000-000000001003'::uuid,
    '9a000000-0000-4000-8000-000000000001'::uuid,
    '9a000000-0000-4000-8000-000000000002'::uuid
  ]) AS goat_id
), doomed_submissions AS (
  SELECT submission_id
  FROM sop_submissions
  WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
    AND task_id = (SELECT task_id FROM target_task)
), doomed_items AS (
  SELECT item_id FROM sop_submission_items
  WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
    AND submission_id IN (SELECT submission_id FROM doomed_submissions)
), del_fanouts AS (
  DELETE FROM sop_task_submission_fanouts
  WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
    AND submission_id IN (SELECT submission_id FROM doomed_submissions)
), del_completions AS (
  DELETE FROM vaccination_completions
  WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
    AND sop_submission_item_id IN (SELECT item_id FROM doomed_items)
), del_items AS (
  DELETE FROM sop_submission_items
  WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
    AND submission_id IN (SELECT submission_id FROM doomed_submissions)
), del_submissions AS (
  DELETE FROM sop_submissions
  WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
    AND submission_id IN (SELECT submission_id FROM doomed_submissions)
), reset_obligations AS (
  UPDATE obligation_instances
  SET status = 'due',
      completed_at = NULL,
      row_version = row_version + 1,
      updated_at = now()
  WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
    AND target_id IN (SELECT goat_id FROM target_goats)
    AND target_type = 'goat'
  RETURNING obligation_id
)
UPDATE sop_tasks
SET state = 'in_progress',
    row_version = row_version + 1,
    updated_at = now()
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND task_id = (SELECT task_id FROM target_task);
SQL
```

After submit, prove the outcome at the database before asking anyone to repeat
the phone flow:

```bash
psql "$DATABASE_URL" <<'SQL'
SELECT count(*) AS completions
FROM vaccination_completions
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND goat_id IN (
    '91000000-0000-4000-8000-000000001001',
    '91000000-0000-4000-8000-000000001002',
    '91000000-0000-4000-8000-000000001003',
    '9a000000-0000-4000-8000-000000000001',
    '9a000000-0000-4000-8000-000000000002'
  );

SELECT g.goat_id, pr.dose_code, oi.status
FROM obligation_instances oi
JOIN protocol_rules pr
  ON pr.tenant_id = oi.tenant_id
 AND pr.rule_id = oi.rule_id
JOIN goats g
  ON g.tenant_id = oi.tenant_id
 AND g.goat_id = oi.target_id
WHERE oi.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND oi.target_id IN (
    '91000000-0000-4000-8000-000000001001',
    '91000000-0000-4000-8000-000000001002',
    '91000000-0000-4000-8000-000000001003',
    '9a000000-0000-4000-8000-000000000001',
    '9a000000-0000-4000-8000-000000000002'
  )
ORDER BY g.goat_id, pr.dose_code;
SQL
```

The pass condition is 10 completions and 10 completed obligations: five scanned
goats times two vaccines. Godel 1 Part 2's two neighbor goats must be included
in that count even though the operator submitted from Part 1.

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
