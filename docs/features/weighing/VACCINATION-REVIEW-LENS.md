# Weighing Vaccination Review Lens

Status: seed/E2E lane draft for Weighing v1.

This lens must be read before implementing or reviewing Weighing code. It turns
the last Vaccination hardening fixes into explicit Weighing review questions and
fixture obligations.

## Review Lens

| Vaccination hardening lesson | Weighing review question | Required fixture/E2E proof |
|---|---|---|
| Shared parent task state leaked across sibling sheds. | Does every progress query, submit, proof, and route preserve campaign + selected shed/partition + work group + animal grain where applicable? | A work group contains more than one selected shed/partition, and progress stays category-aware for each selected scope. |
| Over-broad submit items wrote sibling shed completions. | Can a command for shed A accidentally complete shed B or a per-shed category accidentally create animal completions? | Per-shed/partition observation completes only its selected `campaign_shed_id` and never creates individual weights. |
| Proof refs disappeared across retry/recovery. | Can proof be recovered from durable rows after upload retry, app restart, idempotency replay, or process death? | One per-animal proof retries from failed upload to accepted, and one uploaded-but-unsubmitted proof is removable before acceptance. |
| Terminal state accepted fresh side effects. | Are completed/canceled work groups immutable except explicit correction/reopen flows? | Duplicate scan/idempotency replay returns the existing observation and does not increment progress twice. |
| Android routes reopened the wrong task/shed after scan. | Does every Room row, outbox command, and route carry campaign id, work group id, selected shed/partition id, animal id, and proof id where relevant? | Off-page scan updates the scan feed without loading the whole roster and keeps the active Weighing route identity. |
| Permission/proof gates ran too late. | Do backend routes enforce `weighing.plan`, `weighing.monitor`, and `weighing.execute` before mutation or data leakage? | CEO/CXO can create/edit/publish, Dinakar can monitor/review only, Amit can execute, and anonymous/other personas are forbidden. |
| Leadership cards used frontend fallback copy. | Are progress buckets, disabled reasons, labels, tones, and routes backend-owned? | Fixture declares expected backend contract buckets and verifies they are disjoint and page-size independent. |
| Retry/fanout failures made progress appear stuck. | Are observation acceptance, projection, notification, and proof recovery durable and retryable with visible degraded states? | Scenario includes projection pending, media retry, and delayed roll-forward review rows. |
| Mobile scan/RFID fixes exposed O(n) and terminator-key hazards. | Is RFID lookup indexed/Room-first, and are Enter/Tab terminators swallowed only on the active Weighing scan route? | Scenario contains a 5k+ scale profile, an off-page RFID scan, and explicit route-scoped Enter/Tab expectations. |

## Operational Invariants

- Projection grain: individual-animal progress ranges over expected animal rows;
  per-shed/partition progress ranges over selected `campaign_shed_id` scopes.
- Proof grain: individual category requires animal-scoped video proof;
  per-shed/partition category requires selected-scope proof; neither proof mode
  substitutes for the other.
- RBAC: `ceo_internal`/CXO plans; director monitors/reviews only; Amit executes.
- Offline-first: mobile reads from principal-scoped Room rows, queues outbox
  commands, and survives process death and sign-out wipe.
- Bounded reads: summaries are whole-filter summaries, detail rows are keyset
  paged, and RFID resolution does not require loading all expected animals.

## Fixture Gate

The companion fixture lives at:

```text
fixtures/weighing-e2e-2026-07-29/weighing-seed.json
```

Run:

```bash
node --test fixtures/weighing-e2e-2026-07-29/weighing-seed.test.mjs
node tools/dev/validate-weighing-e2e-fixture.mjs
```

These checks are contract gates for the seed/E2E lane. They do not replace the
future backend, Android, and browser E2E tests; they define the minimum scenario
those tests must materialize.
