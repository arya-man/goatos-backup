# Lane 4 - write-path journeys on the OCI clone

Status: built, unit-tested, and proved end to end against the real OCI clone on
2026-09-23. The screen half of each journey is parked with the exact command to
finish it (see [What is parked](#what-is-parked)).

These are the flows production cannot test. `dashboard.mesha.sg` and
`api.goatos.mesha.sg` are STG-backed production and are **read-only** for all of
this automation. Lane 1 proves pages render; nothing in lane 1 ever presses Save.
Lane 4 presses Save - on a writable clone, on tables it declared in advance, and
it puts every one of them back and proves it did.

---

## How to run it

The lane is **off by default**. It is nightly and on-demand only, and it never
runs in `production-smoke`.

```sh
# on the OCI box, once, to mark the clone as disposable
GOATOS_WRITE_JOURNEY_DATABASE_URL='postgres://…@127.0.0.1:5432/goatos' \
GOATOS_WRITE_JOURNEY_TARGET=oci-clone \
GOATOS_WRITE_JOURNEY_CLONE_URL='postgres://postgres@127.0.0.1:5432/goatos' \
  node mesha-ops/dashboard-automation/tooling/run-write-journeys.mjs --stamp-clone

# then, per run
GOATOS_DASHBOARD_WRITE_JOURNEYS=1 \
  node mesha-ops/dashboard-automation/tooling/run.mjs --mode write-journeys

# or the runner on its own
node mesha-ops/dashboard-automation/tooling/run-write-journeys.mjs --out <file> [--only <name>]
```

Flags: `--self-test`, `--only <name>`, `--out <file>`, `--stamp-clone`,
`--no-ui` (data half only), `--simulate-write` (apply the write each journey's
screen would make, directly - for proving the machinery with no browser stack up;
the receipt records `screenDriven: false` so this can never be read as proof that
the screen works).

Environment: `GOATOS_WRITE_JOURNEY_DATABASE_URL` (required),
`GOATOS_WRITE_JOURNEY_TARGET=oci-clone` and `GOATOS_WRITE_JOURNEY_CLONE_URL`
(required for the OCI clone; a database whose name already says it is disposable
needs neither).

---

## The table rule, and Ravi's reasoning

**Lane 4 refreshes and restores only the tables each individual journey actually
writes.** There is no wholesale nightly refresh of a big table list.

Ravi's reasoning, as given: parity is never a gate. STG keeps moving, so a
nightly bulk refresh is a moving target that fails for reasons that have nothing
to do with the product, and blocking on it is exactly what made the old alerts
useless. A journey, on the other hand, knows precisely what it touches. So each
journey declares `writesTables`, the harness snapshots exactly those before the
journey and restores them after, and then **proves** the restore by comparing
row counts and a content fingerprint against the snapshot. The blast radius of
the whole lane is the union of what the journeys declare, and it is visible in
one file.

Consequences, all enforced in code:

- A journey that declares no tables does not run.
- A journey that writes a table it did not declare **fails**. Not a warning.
- **Analytics and telemetry tables are never touched, in any list.**
  (`NEVER_TOUCH_TABLE_PATTERN`.)
- **Obligations are never refreshed from STG** - this lane has no STG refresh
  path at all, by design. They may appear in a journey's `generatedTables`,
  because publishing a plan version *generates* the work it implies and leaving
  those rows behind would poison the clone. They are snapshotted and restored
  with the rest of what the journey touched, and they may never be declared as a
  table a journey writes directly. (`NEVER_DECLARE_AS_WRITTEN_PATTERN`.)

---

## How the guard makes production and STG unreachable

Four independent checks, all of which must pass before a single row is touched.
`mesha-ops/dashboard-automation/tooling/lib/table-snapshot.mjs`.

1. **Loopback only.** A non-loopback database host is refused outright. Production
   and STG are Cloud SQL; there is no DSN shape that gets past this.
2. **Declared read-only DSNs are refused even on loopback.** The OCI box runs the
   STG read-only proxy on `127.0.0.1:5455`. Any `*_DATABASE_URL` in the
   environment whose **name** says STG or production is parsed, and a target that
   matches its host and port is refused. A read-only *role* on the clone itself is
   deliberately not treated this way, or it would lock the clone out.
3. **Naming.** The database must be a disposable automation/preview database
   (`dashboard[_-]automation|preview|tmp|throwaway`, the same rule
   `run.mjs: assertPostgresIntegrationConfigured` already enforces) **or** the
   explicitly declared OCI clone, named in two environment variables that have to
   agree with each other and with the target.
4. **Live identity.** The server is interrogated rather than trusted. Refused if
   it carries a managed `cloudsqlsuperuser` / `cloudsqladmin` / `alloydbsuperuser`
   role (production and STG always do, the container clone never does), if the
   session is read-only, if the server address is not loopback, or if the database
   does not carry the one-time `goatos_write_journey_clone` marker schema. The
   marker is created by `--stamp-clone`, which itself passes every other check
   first - and could never be created on production or STG, because they are
   read-only.

On top of that, `run.mjs`'s `write-journeys` layer classifies the target before
the runner is even spawned, and refuses `production-smoke` explicitly.

**A refusal never echoes the DSN** - only `host:port/database`.

### Proved on the box, not asserted

Run on `opc@144.24.107.47` against the real clone on 2026-09-23:

| attempt | result |
|---|---|
| `…@goatos-stg-core-db.asia-south1.gcp:5432/goatos` | refused: non-loopback database host, exit 1 |
| `…@127.0.0.1:5455/goatos` (the **live** STG read-only proxy) | refused: the database this box declares as production or STG, exit 1 |
| `…@127.0.0.1:5432/goatos` before stamping | refused: no clone marker, exit 1 |
| `…@127.0.0.1:5432/goatos` after stamping | allowed, `targetKind: "oci-clone"` |

---

## The journeys

Nine journeys: the six write paths the roadmap names, and the three business
rules that keep regressing. `mesha-ops/dashboard-automation/tooling/write-journeys.json`.
Each carries the plain-English story, the UI steps, what must be visible
afterwards, the SQL that must find the row the write created, `writesTables`,
the farm-manager sentence, and the commits behind it.

| journey | the story it proves | tables it writes |
|---|---|---|
| `publish-vaccination-plan-version` | a manager publishes a new vaccination plan version | `protocol_versions`, `protocol_rules`, `protocol_rule_dimensions` (+ generated obligations) |
| `change-a-feed-rate` | a manager changes a ration's feed rate and the sheet picks it up | `feed_ration_rates` |
| `publish-an-sop` | a manager publishes an SOP so the phone shows the new card | `sop_definitions`, `sop_versions` |
| `create-and-move-a-task` | a manager creates a task and drags it to another column | `leadership_tasks`, `leadership_task_events` |
| `record-a-sale` | a manager records a sale against a load | `sales_deals`, `sales_deal_lines` |
| `approve-a-verification` | a verifier approves a pending item | `verification_items`, `verification_review_events` |

### The three rules, and the regressions they come from

These are not best-practice checks. Each one is a thing that has actually broken.

- **A feed item cannot be deleted while a ration names it.**
  `aa429412c fix(configuration): fence keyed register deletes` (2026-09-18) and
  `58836fa4f docs(configuration): record pr303 delete fence proof`. If the fence
  comes off, the next feed sheet asks for an item that no longer exists.
  History miner: 13 commits, all subject-linked.

- **A published plan version stays locked.**
  `d3944496f fix(vaccination): carry over medical-equivalent work` (2026-09-01)
  and `f6e75b236 chore(procurement): lock the SOP-driven programme and widen its
  guard` (2026-09-20). If a published version can still be edited in place, the
  doses people were given change without anyone publishing anything. History
  miner: 24 commits, all subject-linked, and `P-published-version-not-locked` is
  the pattern on every one of them - this is the second-biggest repeat pattern in
  the lane.

- **Partition labels survive a move.**
  `475312f5d fix: prevent doubled worded partition labels` (2026-09-21),
  `1a256a225 test(weighing): follow one pen's label from the bucket row to the
  verification` (2026-09-21), `409925cad feat(configuration): pen type moves to
  the partition` (2026-09-22). The database assertion is the regression itself:
  no pen label may read with a doubled word, and no pen label may be rewritten by
  a move. History miner: 59 commits, all subject-linked - the largest repeat
  group routed to this lane.

### Coverage, honestly

The history miner derived **35** journeys for this lane from **423** commits
since 2026-08-01. **Nine are built.** The other 26 are listed in
`write-journeys.json` under `backlog.notBuiltYet`, each with its repeat count and
priority, so the gap is visible rather than quietly dropped. The two biggest
unbuilt ones are `lane4.weighing-plan-publish-and-capture` (69 commits) and
`lane4.record-a-vaccination` (35).

**The path-only caveat.** Of the 426 routed commits, 43 are tied to their check
by **file path alone**. Two of the built journeys inherit a lot of those and
their history link is **weak**:

- `approve-a-verification`: 25 of 33 routed commits are path-only. Reading them,
  most are about **proof media rendering** (video log titles, media kinds, k-of-N
  proofs) rather than the approve write path. This journey proves that approving
  records the verdict; it does **not** prove those media commits.
- `change-a-feed-rate`: 5 of 6 routed commits are path-only, and they are about
  feed notifications and a generated `partition_key` column, not the ration rate.
  (Worth noting: `d68dfe2ed fix(feed): match the generated partition_key column on
  Part-N pens -- second submits 500'd` is the same class of bug the restore engine
  hit live - see below.)

The `sourceCommits` on each journey were read by hand and are the real
justification. The `history.sourceShas` block is the miner's automatic routing,
kept for traceability, and it is not a coverage claim.

---

## The per-journey cycle

Per journey, in this order, **every time**:

1. guard the target database
2. snapshot exactly the declared tables (`writesTables` + `generatedTables`)
3. drive the screen
4. assert **what is on screen**
5. assert **the row the write created** in the database
6. check nothing undeclared was written
7. **restore**
8. **prove the restore**
9. record all of it

Steps 7 and 8 run even when 3-6 threw. A restore that cannot be proved keeps its
snapshot instead of cleaning it up, so the evidence survives.

**The proof is a content fingerprint, not a row count**: per table,
`md5(string_agg(md5(row::text), '' order by …))`. It catches a row that was not
put back, a row that came back changed, and a delete-and-reinsert pair. It also
folds in the undeclared-write check, because rows in a table that was never
snapshotted were never put back either.

**Undeclared writes are read honestly.** `pg_stat_user_tables` counters are not
transactional: a backend accumulates them locally and a reader serves a cached
snapshot. So the engine forces this backend's stats out
(`pg_stat_force_next_flush`), clears the reader's snapshot
(`pg_stat_clear_snapshot`), falls back for servers older than PostgreSQL 15, and
believes only a reading that two consecutive polls agree on. Otherwise "no
offenders" could just mean "the stats had not flushed yet".

### What the live run found

The first real run against the clone snapshotted 2656 rows of
`feed_ration_rates`, applied the write, found the row, and then **failed its
restore**:

```
ERROR:  cannot insert a non-DEFAULT value into column "ration_group_key"
DETAIL:  Column "ration_group_key" is a generated column.
```

`insert into t select * from snap` is wrong on any real schema. The restore now
reads each table's columns at snapshot time, leaves generated columns out of the
insert, and adds `OVERRIDING SYSTEM VALUE` for an identity column declared
`ALWAYS`; a table with no column the engine could put back is refused at
**snapshot** time rather than discovered at restore time.

The point worth keeping: **the proof did its job.** Same 2656 rows, different
content fingerprint - so it reported `blocked` (louder than a journey failure),
kept the snapshot, and put the server's own error in the receipt. A row-count
check would have called that green. Receipts:
`docs/engineering/lane4-receipts/`.

After the fix, the same journey ran clean and an independent check confirmed the
clone's content fingerprint was **byte-for-byte what it was before the run**,
with zero snapshot tables left behind.

---

## What a finding looks like in Slack

Lane 4 adds a **new kind** of finding through the path lane 1 already uses. It
does not restructure `formatVisualIssuesMessage`, `formatSlackMessage`,
`humanIssue`, `groupSlowPages` or the threaded-reply mechanics.
`notify-slack.mjs` gained one marked registry block, one import and one registry
entry, and no line of the existing rendering was removed. This branch is rebased
onto `origin/auto/lane2-20260923`, which is the canonical copy of that file: this
lane's registry verbatim, plus PR #367's guarded `postSlack` fix, plus lane 2's
two lines. A lane-1-only receipt renders
**byte-identically**, and there is a checked-in golden
(`mesha-ops/dashboard-automation/tooling/testdata/lane1-only-slack.golden.txt`, captured from
the pre-registry `notify-slack.mjs` at `4aeb8264b`) and a test that spawns the
current one and compares byte for byte.

A registered lane that found **nothing** on a receipt contributes nothing - not
even an `issueRules` regex - so it cannot relabel or reorder a lane-1 message.
Section order and headline ownership come from a numeric `severity` each kind
declares, never from import order.

**A lane whose module throws loses its own section, never the message.** Silence
is the worst outcome this automation has: the alert going quiet exactly when
something is wrong. Each kind is isolated in collect, render and reply, and a
failure becomes a finding of its own - *"Something could not be checked / One of
the checks could not run at all, so whatever it looks after was not checked this
time."* The module's name and its error go to the log, never to Slack.

Everything a kind contributes - its section, its summary line **and its
headline** - is resolved before a single block is spliced. The headline used to
be called on its own at the tail, outside the guard: a kind whose `headline()`
threw killed the whole notifier, after blocks had already been inserted, on the
realistic path where lane 1 is clean and another lane found something. The
judge's reproduction is now a permanent case in `notify-slack.mjs --self-test`:
a lane whose headline throws loses its own section, cannot rewrite the header,
and lane 1's content plus every healthy lane's findings still post.

`run.mjs` also gained a `write-journeys` mode. Its mode logic moved into
`lib/run-modes.mjs` (pure) so a unit test can prove `production-smoke` and
`post-main-certification` behave exactly as before. Parity is never a gate for
this lane's mode - without that, a fully green journey run would still report
fail because an STG-to-OCI parity receipt was 26 hours old - and this lane never
runs the certification extras (Lighthouse, API latency, the Go lifecycle suites).

**A failed journey:**

> 🚨 **2 things a person does on the site did not work**
>
> **Doing this on the site did not work**
> • [Vaccination Plan Edit](#) - Publishing a new vaccination plan version did not
>   save. A manager who publishes a plan would be told it worked and find nothing
>   there.
> • [Feed Config](#) - The screen let someone delete a feed item that a ration
>   still uses. The next feed sheet would ask for an item that no longer exists.

**A failed or unproved restore** is its own, louder finding, and it leads the
message:

> ‼️ **Practice data was left changed**
>
> **‼️ Practice data was left changed - fix this before anything else**
> • [Feed Config](#) - The practice copy of the farm's data was left with this
>   check's changes in it. Nothing should be read from that copy, and no further
>   check should run against it, until someone puts it back.
>
> **Doing this on the site did not work**
> • [Feed Config](#) - Changing a feed rate did not save. The sheet the team feeds
>   from would keep the old amount.

Each finding gets its own threaded reply with its link and its own red-boxed
screenshot, exactly as lane 1's do.

**Slack text from this lane contains no SQL, no table or column names, no
selectors, no check codes, no env var names, no endpoints and no file paths.**
That is enforced, not advised: `assertPlainEnglish` runs at the render site and
**throws** rather than let one through. The table names that matter for debugging
live in the receipt and the HTML report.

Baseline debt deliberately not touched: `formatSlackMessage`'s own fields
(`Mode`, `Browser`, `Data parity`, `Receipt: <path>`) still show through on this
path. Fixing them would break byte-identical rendering for lane 1, so they stay.

---

## What is parked

**The screen half of each journey.** The runner drives the screen by spawning
`npm --prefix apps/admin-web run smoke:write-journey:live` per journey, in the
same shape `run-module-journeys.mjs` already uses. That script and the
admin-web + API stack pointed at the clone are not up: the OCI box has the API
processes on `127.0.0.1:18873/18874` and Playwright's chromium installed, but
admin-web has no `node_modules` and no build there. So the live run used
`--simulate-write`, which applies the write the screen would make directly, and
the receipt records `screenDriven: false` with a reason. **Nothing in any receipt
claims a screen was proved when it was not.**

To finish it, on the box:

```sh
ssh goatos-oci
cd /home/opc/goatos-automation/pr-350-dashboard-parity
npm ci --prefix apps/admin-web && npm --prefix apps/admin-web run build
GOATOS_ADMIN_WEB_BASE_URL=http://127.0.0.1:13308 \
GOATOS_API_BASE_URL=http://127.0.0.1:18873 \
GOATOS_WRITE_JOURNEY_DATABASE_URL='postgres://…@127.0.0.1:5432/goatos' \
GOATOS_WRITE_JOURNEY_TARGET=oci-clone \
GOATOS_WRITE_JOURNEY_CLONE_URL='postgres://postgres@127.0.0.1:5432/goatos' \
GOATOS_STG_READONLY_DATABASE_URL='postgres://…@127.0.0.1:5455/goatos' \
  node mesha-ops/dashboard-automation/tooling/run-write-journeys.mjs \
    --out /home/opc/goatos-automation/reports/write-journeys/receipt.json
```

Also parked, and listed so it is not mistaken for coverage: the 26 derived
journeys in `backlog.notBuiltYet`, worst-first by repeat count.
