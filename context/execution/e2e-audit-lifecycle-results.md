# E2E Lifecycle Audit — L1-L8

Environment: temp stack, backend `http://127.0.0.1:8081` @ `e451ad93`, DB `postgres://127.0.0.1:15544/goatos`,
tenant `00000000-0000-4000-8000-000000000001`. Admin dev token from `/tmp/e2e-dev-token.txt`.

**Environment finding (blocks every async case until worked around):** the long-running
`domain-event-consumer` and `outbox-relay` processes listed as "running" in
`.e2e-audit-logs/STACK_STATUS.md` had actually exited at startup —
`domain-event-consumer.log` / `outbox-relay.log` show `project id is required via
--project-id, GOATOS_PUBSUB_PROJECT_ID, or GOOGLE_CLOUD_PROJECT` and `GOATOS_OUTBOX_PUBLISHER
must be set`, and `ps aux` showed no such processes. No outbox message from any mutation below
would ever have been delivered. Worked around per the sanctioned local/dev path: ran
`go run ./cmd/outbox-relay -limit 50 -timeout 60s` with `GOATOS_OUTBOX_PUBLISHER=eventbus
GOATOS_OUTBOX_ALLOW_NONDURABLE=1` after every mutation, draining the outbox through the same
production `outboxapp.Service` + real registered handlers (`obligationapp.NewGoatShiftedHandler`,
`vaccinationapp.NewGoatRecheckHandler`, etc.) in-process — this is the CLI's own documented
local-dev fallback, not a hand-rolled shortcut.

## Summary table

| Case | Result | Notes |
|---|---|---|
| L1 Park shift A->B | **FAIL** | Real bug: SM-2 re-scope SQL crashes when the goat has any already-batched open obligation (SQLSTATE 42P18) |
| L2 Return A->B->A | **PASS** (conditional) | Passes cleanly once no obligation is under a planned batch; blocked by the same L1 bug otherwise |
| L3 Deferred (sick) | **PASS** | health_status='sick' → obligations deferred, `defer_status='sick'` recorded |
| L4 Recovery | **PASS** | health_status='healthy' → obligations reopened to scheduled; scope also self-healed to current shed as a side effect |
| L5 Terminal states | **PASS** | dead/sold/culled all terminally close open obligations; a full-tenant generation run afterward creates 0 new obligations for any of them |
| L6 New goat entry | **PASS** | kid gets 21-day-offset kid course from DOB; procured adult gets post_arrival path + expected warm-up-hold defer; both shed-scoped with owner |
| L7 Co-due spacing (PPR + Goat Pox) | **PASS** | 8/8 sampled goats show exactly 28-day PPR→Goat-Pox live-to-live spacing |
| L8 Null-DOB goats | **PASS** | 5/5 sampled goats' open work is `post_arrival` / `after_previous_completion` (history/entry-anchored); none `birth_age` |

Secondary finding: after any recheck/recovery event, the outbox also emits an internal
`obligation.rescoped` notification event that consistently fails schema validation
(`invalid_event_envelope`) and is marked permanently `failed` — every rescope path leaves these in
the dead-letter-adjacent `failed` state. Not blocking for L1-L8 (nothing downstream reads it in
this stack), but it means any consumer that DOES subscribe to `obligation.rescoped` never sees it.

---

## L1 — Park shift A -> B: FAIL (real backend bug)

**Setup.** Goat `003c6330-6a21-509b-9abd-21fe82285879` (park CPT, shed
`a80948b1-63a1-51b4-b4e3-e0cd3d8e2daa`) has 5 open `scheduled` obligations; 2 of them
(`353aa045-9348-4657-b2c7-af2a9f7813cd`, `74b03fd9-d405-45eb-a2eb-1fb846b22b08`) were already
attached to `planned` obligation batches (`batch_id` set) — this is the normal state for most of
the 1311-goat seed: a DB-wide check (`obligation_instances WHERE status='scheduled' GROUP BY
target_id HAVING bool_and(batch_id IS NULL)`) returned **zero rows** — every goat with open work
has at least one batched obligation, so this is the common case, not an edge case.

**Mutation.** `POST /admin/goats/003c6330.../move` with `park_id=00000000-...-003001`,
`shed_id=c93c636f-08f5-4ee9-aaca-bf7f1efd4788`, `row_version=1` → `200 OK`, emits
`goat.location.changed` (event `d6839a3f-...`), decision `move_goat` approved.

**Worker run.** `go run ./cmd/outbox-relay` (eventbus mode) drained the event; result:
`published: 65, retry_scheduled: 1` — the 1 retry-scheduled message was our shift event.
Re-running the relay left it stuck in `retry_scheduled` / `pending` indefinitely (would eventually
dead-letter after `MaxAttempts`).

**Root-cause repro.** Wrote a throwaway debug harness (`backend/cmd/e2e-debug-shift`, deleted
after use — not committed) calling the exact production handler
`obligationapp.NewGoatShiftedHandler(repo).HandleEvent(...)` against the same DB with the same
payload/timestamp taken verbatim from the outbox row. Result:

```
HandleEvent err: obligation: update old shift batch: ERROR: could not determine data type of parameter $4 (SQLSTATE 42P18)
```

**Location:** `backend/internal/obligation/adapters/postgres/repository.go`, in
`reScopeOpenForGoatInTx`'s per-old-batch `tx.Exec` (~line 3380-3410). The `UPDATE obligation_batches
... SET context = ... jsonb_build_object('moved_target_id', $4, ...)` passes the bare goat-ID
string as `$4` with no cast anywhere in the statement; pgx v5 cannot infer a type for a parameter
used only inside a `jsonb_build_object(...)` argument list, so Postgres rejects the query with
42P18 on every invocation where `oldBatches` is non-empty (i.e., the goat has ≥1 already-batched
open obligation). The fix is a one-line cast, e.g. `$4::text`.

**Effect:** whenever a real shed shift happens on a goat with at least one batched obligation (the
overwhelming majority of this cohort), the `goat.location.changed` handler returns an error, the
outbox message is marked retryable and stays stuck, and the obligation is **never re-scoped to the
new shed** through this path — the old shed keeps counting a goat that has physically left, and the
new shed's drive never picks it up. This is a P0-class kernel-correctness bug per the operational
kernel contract (SM-2 re-scope), not a test artifact.

**DB evidence (before/after, this exact goat):**
```
obligation_id=55390cc4-...  scope_id BEFORE move: a80948b1... (Shed-Old, CPT)
obligation_id=55390cc4-...  scope_id immediately after move + relay run: a80948b1... (unchanged — FAIL)
```
(It later self-corrected to the new shed only as an incidental side effect of the goat becoming
sick then recovering — see L3/L4 below — not through the shift path itself.)

## L2 — Return A -> B -> A: PASS, conditional on L1's precondition

Once the goat's obligations had no `batch_id` set (true after the L3/L4 defer-then-recover cycle,
which clears `batch_id` on obligations it touches), the return shift worked cleanly:

- `POST /admin/goats/.../move` back to `park_id=00000000-...-003002`,
  `shed_id=a80948b1-63a1-51b4-b4e3-e0cd3d8e2daa`, `row_version=4` → `200 OK`, emits
  `goat.location.changed` (`8d7f37bc-...`).
- Outbox relay run: this event published successfully (no retry).
- DB after: all 5 obligations (same `obligation_id`s throughout — `55390cc4-...`, `de32cf12-...`,
  `25a73e0a-...`, `353aa045-...`, `74b03fd9-...`) now `scope_id = a80948b1...` (Shed-Old again),
  `status = scheduled` — deterministic lineage preserved, no duplication, no rewind to a wrong
  status.

This confirms the re-scope logic itself is correct for the unbatched case; the L1 finding is
narrowly the raw-SQL type-inference bug in the batched-obligation branch.

## L3 — Deferred (sick): PASS

`POST /admin/goats/.../health` `health_status=sick`, `row_version=2` → `200 OK`, emits
`goat.health.changed` (`75c527fc-...`). Outbox relay run delivered it. Result: all 5 open
obligations for the goat flipped `scheduled → deferred`; `obligation_status_events` has a
`deferred` row per obligation with `payload->>'defer_status' = 'sick'` for every one of them.
(Location-flag ICU path from Story Q was not separately re-driven here since Story Q already
proves it in the committed E2E suite — `backend/tests/e2e/story_q_sick_icu_location_defer_test.go`
— and this audit's goat pool didn't include an ICU-flagged shed.)

## L4 — Recovery: PASS

`POST /admin/goats/.../health` `health_status=healthy`, `row_version=3` → `200 OK`, emits
`goat.health.changed` (`556a91c3-...`). Outbox relay run delivered it (2 published, 5 unrelated
`obligation.rescoped` notifications failed schema validation — see secondary finding above, not
this goat's recovery). Result: all 5 obligations `deferred → scheduled`;
`obligation_status_events` shows `deferred → rescoped → scheduled` per obligation, confirming the
recovery-repair path both reopens the obligation AND re-syncs its scope to the goat's current
shed (`c93c636f-...`) — which is how this goat's obligations ended up correctly scoped to the new
shed despite the L1 bug blocking the direct shift path.

## L5 — Terminal states: PASS

Three distinct goats, each with several open `scheduled` obligations:

- `0069e73f-7329-5833-b067-538f52936986` → `POST .../critical-death-exit`
  `lifecycle_status=dead`, `exit_reason=died` → `200 OK`.
- `00a76d4a-2c6b-52d2-b9e1-d5a68ce1b817` → `POST .../exit` `lifecycle_status=sold`,
  `exit_reason=sold` → `200 OK`.
- `00e0cfd7-e563-5550-85ee-8b845442553a` → `POST .../exit` `lifecycle_status=culled`,
  `exit_reason=culled` → `200 OK`.

After the outbox relay run, `obligation_instances` status breakdown per goat:

```
0069e73f...  canceled=7 completed=1   (0 scheduled/due/deferred)
00a76d4a...  canceled=3 completed=3   (0 scheduled/due/deferred)
00e0cfd7...  canceled=4 completed=5   (0 scheduled/due/deferred)
```

Then ran the real generation CLI tenant-wide:
`go run ./cmd/generate-vaccination-obligations -tenant-id 00000000-0000-4000-8000-000000000001`
→ `generated=0` (both the recovery-repair pass and the effective-cohort pass) — confirms the
three exited goats produce no future work at all, i.e. they're correctly excluded from the
generation cohort by `lifecycle_status`.

## L6 — New goat entry: PASS

- **Kid** (birth origin): `POST /admin/goats` `origin_type=birth`, `dob=2026-06-28`,
  `entry_date=2026-06-28` → goat `97214430-fa6c-4b04-b675-ee9aac5703ca`, `goat.created` emitted,
  `generation_status=queued`.
- **Adult** (purchased): `origin_type` must be `birth`, `procured`, or `imported` (API-enforced
  enum — `purchase` is rejected with `invalid_goat_create`); used `procured`, `dob=2024-01-01`
  (`dob_estimated=true`), `entry_date=2026-07-15` → goat `dfd6dea4-f774-4683-bf57-47b73037045d`.

After outbox relay run (both `goat.created` events published, 0 failed):

- **Kid**: 6 `scheduled` obligations, first due `2026-07-25` (27 days post-birth — the 21-day
  kid-course-offset rule), all shed-scoped to the seeded shed with an owning park — confirms
  DOB-anchored kid course.
- **Adult**: 6 obligations, all `deferred` with `defer_status='warming_hold'` — the expected
  arrival warm-up hold for a freshly `procured` adult (per the shed/warm-up model), all
  shed-scoped. This is `post_arrival`-path generation from entry date, correctly held for warm-up
  rather than immediately open — not a bug, matches the documented arrival-hold behavior.

## L7 — Co-due spacing (PPR + Goat Pox, 28-day live-to-live): PASS

Resolved PPR and Goat Pox `protocol_rules.rule_id`s from `eligibility_json->>'code'` (`PPR`,
`GOAT_POX`), then found every goat with an open obligation on both. Sampled 8:

```
target_id       PPR due        Goat Pox due    gap
0303d239...     2026-09-21     2026-10-19      28d
0437e3a7...     2026-07-31     2026-08-28      28d
07dd7dc9...     2026-08-17     2026-09-14      28d
09aee4f4...     2026-09-12     2026-10-10      28d
0a213046...     2026-08-29     2026-09-26      28d
0dbeb888...     2026-08-25     2026-09-22      28d
105ccde9...     2026-09-12     2026-10-10      28d
13355a83...     2026-09-17     2026-10-15      28d
```

All 8/8 show exactly a 28-day gap — the live-to-live spacing rule (Goat Pox after PPR) holds
across the generated cohort. (Goat `003c6330-...`, the L1-L4 test subject, was excluded from this
sample since its due dates were disturbed by the audit's own health-status mutations.)

## L8 — 42(*)-nulled-DOB goats: PASS

(*The live count in this seed is 299 null-DOB goats, not 42 — the task's number appears to be
from a different/prior seed run; sampled 5 from the current 299.)

Sampled 5 `dob IS NULL` goats (`a24ddcea...`, `88aa6e28...`, `0e4ae63d...`, `34cd325b...`,
`ed66c299...`, all `origin_type=procured`, `entry_date=2025-05-24`). For each, joined open
obligations to `protocol_rules.trigger_type`:

```
all 5 goats -> et_tt_adult_w2 (post_arrival), fmd_revac (after_previous_completion),
               hs_revac (after_previous_completion)
```

5/5 confirm entry/history-anchored triggers (`post_arrival`, `after_previous_completion`); none
show `birth_age` — correct, since these goats have no DOB to anchor a birth-age rule to.

---

## Net defect list from this audit

1. **P0 — SM-2 shed-shift re-scope crashes on any goat with a batched open obligation**
   (`obligation: update old shift batch: ... SQLSTATE 42P18`,
   `backend/internal/obligation/adapters/postgres/repository.go` ~`reScopeOpenForGoatInTx`,
   untyped `$4` inside `jsonb_build_object(...)`). Blocks the majority real-world shed-shift case
   in this 1311-goat cohort (every goat with open work has ≥1 batched obligation). Fix: cast the
   parameter, e.g. `$4::text`, add a regression test with a goat whose open obligation has
   `batch_id` set at shift time.
2. **P2 — `obligation.rescoped` internal notification event fails envelope schema validation**
   on every recheck/recovery rescope, permanently (`invalid_event_envelope`, marked `failed`).
   Not exercised by any current consumer in this stack, but any future subscriber to
   `obligation.rescoped` will never receive it.
3. **Environment — the temp stack's `domain-event-consumer`/`outbox-relay` background processes
   were not actually running** despite `STACK_STATUS.md` claiming "RUNNING"; both exited at
   startup on missing Pub/Sub config. Worked around per-mutation with the CLI's own
   `eventbus`/`GOATOS_OUTBOX_ALLOW_NONDURABLE=1` local-dev mode; a real staging/prod deployment
   would need real Pub/Sub wiring to be validated the same way.
