# Lane 2 — data sanity on production (read-only)

Lane 2 asks one question of the live Goat OS data, twice a day, in seconds, without
writing a single byte: **do the numbers on the screens add up?**

It is 42 SQL checks. Each one returns only the rows that should not exist. Zero rows is a
pass. When a check comes back with rows, Slack gets a new kind of finding — **"Data does
not add up"** — naming the screen a person opens, the question that was asked, the
plain-English consequence, and how many rows are wrong. The rows themselves never go into
Slack; they go into an HTML report attached in the thread.

- Catalogue: `mesha-ops/dashboard-automation/tooling/data-sanity-checks.json`
- Runner: `mesha-ops/dashboard-automation/tooling/check-data-sanity.mjs`
- Tests: `mesha-ops/dashboard-automation/tooling/check-data-sanity.test.mjs`
- Slack rendering (all of it): `mesha-ops/dashboard-automation/tooling/lib/finding-kinds/data-sanity.mjs`
- Wired into `mesha-ops/dashboard-automation/tooling/run.mjs` as the layer `data-sanity`

## How to run it

```sh
# The connection is built from Secret Manager at run time and is never written to a file.
export GOATOS_STG_READONLY_DATABASE_URL='…'    # via the local cloud-sql-proxy
node mesha-ops/dashboard-automation/tooling/check-data-sanity.mjs --out /tmp/data-sanity.json
node mesha-ops/dashboard-automation/tooling/check-data-sanity.mjs --only feed_issued_exceeds_purchased --out /tmp/one.json
node mesha-ops/dashboard-automation/tooling/check-data-sanity.mjs --self-test
node --test mesha-ops/dashboard-automation/tooling/check-data-sanity.test.mjs
```

Inside the automation it runs as a layer, on by default because it is seconds of pure reads:

```sh
GOATOS_DASHBOARD_DATA_SANITY=0 node mesha-ops/dashboard-automation/tooling/run.mjs --mode production-smoke   # to turn it off
```

Exit code is 0 only when every check came back with no rows and nothing was parked.

## Read-only, and how that is actually enforced

Four independent things have to fail before this lane could write anything. Three of them
are in place today; the fourth is owed and is parked, not claimed.

0. **The row cap is applied by the runner, and the wrapper cannot be escaped.** A `LIMIT` in the text is
   not a cap: `with c as (select 1 limit 1) select g.* from goats g, c` satisfies any text-level
   LIMIT check and still streams the whole table — proved against the replica, it returns all
   1,741 animals. So the runner wraps every check in its own subquery,
   `select * from (<check>) _capped limit 500`, which holds even for a check with no `LIMIT` at
   all. The text-level rules below stay as a second line of defence; the cap does not depend on
   them. The wrapper is string interpolation, so it is also defended against a check that tries
   to escape it: `assertSelectOnly` refuses SQL with unbalanced parentheses or any comment, and
   `cappedSql` puts the closing paren and the cap on their own line. Without both, a check
   reading `select id from goats) _capped limit 999999 --` renders valid PostgreSQL in which the
   attacker's cap wins and the runner's is inside a comment. Every one of these cases — the CTE
   bypass, no LIMIT at all, nested CTEs, unions, the early-close payload, the block-comment
   variant, an unclosed paren and a trailing comment — is a regression test.
1. **The catalogue is refused before a connection is opened.** `assertSelectOnly()` rejects
   any check that is not a single `SELECT` or `WITH … SELECT`: no `;` anywhere (so no
   statement chaining), no psql meta-command, no row lock, no `LIMIT`-less read, and none of
   `insert update delete drop alter truncate grant revoke create copy call do merge vacuum
   refresh reindex cluster lock notify listen prepare execute set reset begin commit rollback
   savepoint into returning nextval setval pg_sleep pg_read_file lo_import lo_export dblink
   pg_terminate_backend pg_cancel_backend`. The scan runs over SQL with comments and string
   literals stripped, so `'do not delete'` is data and `created_at` / `updated_at` /
   `merged_into_goat_id` are columns, not verbs. Tests feed each verb in and assert refusal.
2. **The session is read-only, not just the transaction.** Every `psql` call is spawned with
   `PGOPTIONS=-c default_transaction_read_only=on`. This matters: `set local` is
   transaction-scoped, so a rogue `COMMIT` would drop the connection back into a writable
   autocommit session. `PGOPTIONS` survives that. The `;` ban above means a rogue `COMMIT`
   cannot get in anyway — the two guards are deliberately redundant.
3. **The transaction is read-only and is always rolled back.** Each statement runs as
   `begin read only; set local default_transaction_read_only = on; set local statement_timeout = 20000;
   <check>; rollback`, and the runner reads `transaction_read_only` and
   `default_transaction_read_only` back out of the session and records both in the report. If
   they are not both `on`, **no check runs at all** and the lane parks itself.
4. **The login itself — owed, and parked.** The runner proves the connecting role's grants
   with `has_table_privilege` / `has_schema_privilege` over the 22 tables the catalogue reads.
   On the STG replica today `goatos_app` holds `INSERT/UPDATE/DELETE/TRUNCATE` on all 22 and
   can `CREATE` in `public`. That is recorded as `readOnly.roleIsReadOnly: false` and **parked
   with a reason in every report** — deliberately not downgraded to the warning that
   `check-business-data-parity.mjs:485` settles for. The checks still run, because (1)–(3)
   make writing impossible regardless of grants, but this lane will not claim the *connection*
   is safe until a dedicated read-only login exists. **That login is an open action item.**

The target is bound to `GOATOS_STG_READONLY_DATABASE_URL` only. There is no `--database-url`
flag, no `DATABASE_URL` fallback and no host argument. Missing env parks the lane.

Rows are capped three times — the runner's structural wrapper (500), a `LIMIT` in every check,
then `maxSampleRows` (10) before anything is written — and every cell goes through `lib/redact.mjs` and is truncated to 80 characters before
it can reach a report or Slack. A test builds a report whose rows contain a fake credential and
asserts it is absent from the rendered Slack blocks and redacted in the HTML report.

One bad check never hides the rest: a check that errors is collected into `parked` with its
reason and the sweep continues.

## What a finding looks like in Slack

Lane 2 adds a section, never a rewrite. The finding-kind registry in `notify-slack.mjs` is
**lane 4's**, taken verbatim from `origin/auto/lane4-20260923`; lane 2's whole footprint in that
file is two lines — one import of `lib/finding-kinds/data-sanity.mjs` and one entry in
`FINDING_KINDS` — plus the one-word crash fix noted below. `formatVisualIssuesMessage`,
`formatSlackMessage`, `humanIssue` and `groupSlowPages` are untouched. When lane 2 has no report
next to the receipt it contributes nothing.

That is proved end to end rather than asserted in the abstract: `check-data-sanity.test.mjs`
runs `notify-slack.mjs --receipt` in dry-run twice over the same lane-1 receipt, once with this
lane's report present and once without, and compares the rendered blocks. Lane 4's golden-file
test (`testdata/lane1-only-slack.golden.txt`) was also run against this branch and is green.

Lane 2 registers at **severity 20**, behind lane 4's 0 and 1, so a broken action always leads a
mixed message. Its section is inserted straight after the header, which is where lane 4's
registry puts every finding kind's blocks; lane 1's own sections keep their content and their
relative order below it. When lane 1 found nothing of its own, lane 2 owns the headline:
`🚨 17 figures on production do not add up`.

The section reads like this (real output from the 2026-09-23 run, six shown, the rest in the
report):

```
*Data does not add up*
• <…/vaccination/live-tracker|Vaccination live tracker> — *139 upcoming vaccinations planned for animals that have left*
   _Is any upcoming vaccination planned for an animal that has already left?_
   Upcoming vaccination rounds still list animals that have already been sold or have died.
   The operator will be sent looking for animals that are not on the farm, and the round can
   never be completed.
• <…/feed/analytics|Feed analytics> — *4 feeds sent out in greater quantity than was bought*
   _Has more feed been issued to the pens than was ever bought?_
   More of some feeds has been sent out to the pens than was ever bought. Either purchases are
   missing or the daily sheets are issuing feed the store never had, so the stock figure
   cannot be trusted.
• +11 more — every one of them, with the rows behind it, is in the report in this thread
```

Slack carries no SQL, no table or column name, no check code, no file path and no rows. A test
extracts every schema identifier out of all 42 checks' SQL and asserts that none of them — and
no snake_case token, no SQL syntax — appears in any `question`, `humanFailure` or `countUnit`.

There is no screenshot to show for a number that does not add up, so lane 2 makes **one**
threaded reply rather than one per finding, and what it carries is its own HTML report,
`data-sanity-report.html`: the question, the sentence, the count and the capped sample rows for
every finding. That is the only place the offending rows appear.

The plain-English rule is enforced in code, not documented and hoped for. `assertPlainEnglish()`
throws rather than render a string containing SQL, a snake_case table or column name, a cast or
function call, a selector, an environment variable or a file path — so a badly written catalogue
entry fails the run instead of reaching the channel. The self-test feeds it six hostile strings
and asserts each is refused.

## The 42 checks, and the commits each one comes from

`—` means the check did not run in that run. A number is the count of offending rows found on
2026-09-23; `green` means it came back clean, which for this lane is the result you want.

| Check | Screen | Sev | 2026-09-23 run | Why it exists — the commits behind it |
|---|---|---|---|---|
| `herd_total_vs_status_breakdown`<br>Does the herd total equal alive plus sold plus dead plus inactive? | Herd register | high | green | `21ac4f56a` fix(configuration): resolve an item's category kind through the cheap walk, not the counted projection (2026-09-19). `5c4c0eba6` fix(configuration): read an item by its key inside the projection, not through a cast the index cannot serve (2026-09-19). Derived spec `lane2.herd-total-reconciles`. |
| `park_total_vs_pen_sum`<br>Do the pen counts add up to the park total? | Counts breakdown | high | green | `a8e58fe2f` fix(vaccination): scope anchor publish idempotency (2026-08-31). `ddf6e8788` fix(herd-signals): package and deploy mqtt bridge (2026-08-23). Derived spec `lane2.check-filed-under-the-right-pen`. |
| `alive_animal_without_pen`<br>Is every living animal in a pen? | Herd register | medium | green | `2b8a83dff` fix(location): renumber migrations off main collision, broaden SQL display-drift rule (2026-08-07). `3535a1f3a` chore(migrations): renumber the pen-label repair to 000382 (2026-09-21). Derived spec `lane2.no-orphan-pen-rows`. |
| `animal_in_two_pens`<br>Is any animal standing in more than one pen at once? | Herd register | high | green | `3535a1f3a` chore(migrations): renumber the pen-label repair to 000382 (2026-09-21). `601f6b72b` fix(migrations): make the merged pen-label repair actually runnable (2026-09-21). Derived spec `lane2.animal-in-exactly-one-pen`. |
| `pen_record_disagrees_with_animal_record`<br>Does the pen sheet agree with the animal's own record about where it is? | Herd register | high | green | `d085b2a4d` fix(weighing): an animal's gain is total movement, not a median of leg rates (2026-09-22). `1e77e6a69` Move feed transport rollout repair forward (2026-08-10). Derived spec `lane2.current-location-agrees-with-history`. |
| `register_status_disagrees_with_animal`<br>Does the herd register show the same status as the animal's own record? | Herd register | high | green | `5090bf8cb` fix(configuration): restore the feed columns lost in the merge, and reconcile the bulk fixtures (2026-09-22). `5e9672c09` fix(counts): reconcile questionnaire completes through its own workflow and keeps every proof's title, kind and answer (2026-09-16). Derived spec `lane2.herd-goat-projection-agrees`. |
| `duplicate_animal_tag`<br>Is any tag number shared by two animals? | Herd register | high | green | `442d67df0` fix(vaccination): align Castro ETTT repair with STG data (2026-09-18). `fc7b08bca` fix: repair CBE Castro ETTT history (2026-09-18). Derived spec `lane2.one-active-tag-per-animal`. |
| `non_positive_animal_weight`<br>Is every recorded animal weight above zero? | Weights | high | green | `e25ebf86f` test: repair combined PR validation fixtures (2026-09-16). `b72e1d2da` feat(verification): backfill subject labels that predate the shed/vaccine fix (2026-08-07). Derived spec `lane2.weighing-work-not-stuck`. |
| `non_positive_pen_weight`<br>Is every whole-pen weight above zero? | Weights | high | green | `e25ebf86f` test: repair combined PR validation fixtures (2026-09-16). `b72e1d2da` feat(verification): backfill subject labels that predate the shed/vaccine fix (2026-08-07). Derived spec `lane2.weighing-work-not-stuck`. |
| `implausible_weight_jump`<br>Did any animal double or halve its weight between two weighings? | Weights | medium | green | `e25ebf86f` test: repair combined PR validation fixtures (2026-09-16). `b72e1d2da` feat(verification): backfill subject labels that predate the shed/vaccine fix (2026-08-07). Derived spec `lane2.weighing-work-not-stuck`. |
| `pen_average_disagrees_with_pen_total`<br>Does the stored pen average match the pen total divided by the head count? | Weights | medium | **2** | `e25ebf86f` test: repair combined PR validation fixtures (2026-09-16). `b72e1d2da` feat(verification): backfill subject labels that predate the shed/vaccine fix (2026-08-07). Derived spec `lane2.weighing-work-not-stuck`. |
| `feed_issued_exceeds_purchased`<br>Has more feed been issued to the pens than was ever bought? | Feed analytics | high | **4** | `5ae02c0c8` fix(feed): repair stock load attribution (2026-09-21). `ccb3434a9` fix(pccare): certify inventory vaccine projection (2026-08-27). Derived spec `lane2.feed-issued-is-not-more-than-bought`. |
| `negative_feed_quantity`<br>Is every feed quantity on a daily sheet a positive number? | Feed direction | high | green | `ccb3434a9` fix(pccare): certify inventory vaccine projection (2026-08-27). `8c2bc1218` fix: add feed aggregate projection receipt (2026-09-21). Derived spec `lane2.stock-is-never-negative`. |
| `drive_doses_exceed_pen_animals`<br>Does any vaccination round list more animals than the pen it is planned for? | Vaccination plan | medium | **1** | `2ccd63d77` fix(vaccination): clean live tracker assignment totals (2026-09-22). `9525786e1` fix(vaccination): reconcile completed proof uploads (2026-09-22). Derived spec `lane2.no-double-dosing`. |
| `drive_scheduled_for_exited_animal`<br>Is any upcoming vaccination planned for an animal that has already left? | Vaccination live tracker | high | **139** | `2ccd63d77` fix(vaccination): clean live tracker assignment totals (2026-09-22). `9525786e1` fix(vaccination): reconcile completed proof uploads (2026-09-22). Derived spec `lane2.no-double-dosing`. |
| `drive_animal_not_in_planned_pen`<br>Is every animal on an upcoming vaccination round actually in the pen the round names? | Vaccination plan | medium | **90** | `a8e58fe2f` fix(vaccination): scope anchor publish idempotency (2026-08-31). `ddf6e8788` fix(herd-signals): package and deploy mqtt bridge (2026-08-23). Derived spec `lane2.check-filed-under-the-right-pen`. |
| `future_dated_animal_record`<br>Is any animal recorded as born or arriving in the future? | Herd register | high | green | `f7e03ef75` fix(vaccination): allow future manual anchors (2026-08-30). `7f7ec470c` feat(sales): Farm born is what the register marks born here (2026-09-22). Derived spec `lane2.no-future-dated-records`. |
| `future_dated_work_record`<br>Is any purchase, sale or feed day recorded in the future? | Feed direction | high | green | `f7e03ef75` fix(vaccination): allow future manual anchors (2026-08-30). `e682f7dfa` fix(weighing): repair migration for draft-campaign work-item leak, lock in publish ordering (2026-08-02). Derived spec `lane2.no-future-dated-records`. |
| `death_before_birth`<br>Did any animal die before it was born? | Mortality | high | green | `f7e03ef75` fix(vaccination): allow future manual anchors (2026-08-30). `71f87ad77` feat(counts): Mortality page — deaths from every angle beside the at-risk herd (2026-09-18). Derived spec `lane2.no-future-dated-records`. |
| `capture_after_submission`<br>Was any proof photo taken after the work it proves was signed off? | Verification | medium | **1** | `b8e726ce2` fix(tasks): gate shared workflow routes by the workflow's own module (2026-09-19). `49a8dcc28` revert(tasks): a bare legacy proof_ref stays the client's video, as on main (2026-09-17). Derived spec `lane2.completion-verified-has-its-proof`. |
| `orphan_weighing_record`<br>Does every weighing still belong to a weighing round that exists? | Weights | high | green | `2b8a83dff` fix(location): renumber migrations off main collision, broaden SQL display-drift rule (2026-08-07). `e25ebf86f` test: repair combined PR validation fixtures (2026-09-16). Derived spec `lane2.no-orphan-pen-rows`. |
| `orphan_sale_record`<br>Does every sale line and every sold animal still belong to a deal that exists? | Sales | high | green | `210b96a51` fix(weighing): enforce sale-line invariant in transaction (2026-09-19). `42171d6d0` fix(sqlc): carry sale allocation weight check (2026-09-08). Derived spec `lane2.sale-line-totals-match-the-deal`. |
| `orphan_load_record`<br>Does every purchased animal still belong to a purchase load that exists? | Source entry | high | green | `b4b12e92b` Loadwise: the membership dedupe needs a TOTAL order (2026-08-31). `3b74d99ad` feat(counts): Mortality reads vendor-wise, not only load-wise (2026-09-21). Derived spec `lane2.procurement-load-counts-add-up`. |
| `purchase_load_count_disagrees`<br>Does each purchase load hold the number of animals it says it expected? | Source entry | medium | **7** | `b4b12e92b` Loadwise: the membership dedupe needs a TOTAL order (2026-08-31). `3b74d99ad` feat(counts): Mortality reads vendor-wise, not only load-wise (2026-09-21). Derived spec `lane2.procurement-load-counts-add-up`. |
| `weighing_run_stuck_over_2_days`<br>Has any weighing round been left running for more than two days? | Weights | medium | **1** | `e25ebf86f` test: repair combined PR validation fixtures (2026-09-16). `b72e1d2da` feat(verification): backfill subject labels that predate the shed/vaccine fix (2026-08-07). Derived spec `lane2.weighing-work-not-stuck`. |
| `vaccination_round_unfinished_over_2_days`<br>Are there vaccination rounds whose day has passed with animals still untreated? | Vaccination live tracker | medium | **500** | `12f773673` fix(process-integrity): bucket adherence due-after cache key (2026-09-08). `e7d920b3d` fix(sqlc): refresh generated obligation models (2026-09-08). Derived spec `lane2.due-doses-do-not-sit-past-their-window`. |
| `feed_sheet_unlocked_over_2_days`<br>Has any day's feed sheet been left open for more than two days? | Feed direction | low | **4** | `8c2bc1218` fix: add feed aggregate projection receipt (2026-09-21). `1c1ac878a` fix(feed): older apps, heal-on-retry, fresh captures and stale verdicts on the feed cards (2026-09-17). Derived spec `lane2.feed-row-quantity-matches-its-rate`. |
| `verification_pending_over_7_days`<br>Is anything waiting for approval for more than a week? | Verification | medium | **5** | `690fd67f9` fix: add projection-review markers and integration tests for oversight analytics aggregates (2026-08-12). `455bf3269` fix(verification): repair CreateItem INSERT broken by the partition_label column (2026-08-07). Derived spec `lane2.verification-not-pending-too-long`. |
| `sales_sold_count_vs_herd_sold`<br>Does the number of animals on closed deals match the number the herd register marks sold? | Sold animals | high | **1** | `210b96a51` fix(weighing): enforce sale-line invariant in transaction (2026-09-19). `42171d6d0` fix(sqlc): carry sale allocation weight check (2026-09-08). Derived spec `lane2.sale-line-totals-match-the-deal`. |
| `dead_animal_without_recorded_cause`<br>Does every death have a cause recorded against it? | Mortality | medium | **5** | `0842c4c3b` chore(health): renumber again past main's 000383 (2026-09-22). `65ca12386` chore(health): renumber the three migrations past main's 000384 and 000385 (2026-09-22). Derived spec `lane2.health-cases-close-on-time`. |
| `vaccination_recorded_twice_for_same_dose`<br>Was any animal recorded as vaccinated twice for the same due dose? | Vaccination live tracker | high | green | `9525786e1` fix(vaccination): reconcile completed proof uploads (2026-09-22). `2ccd63d77` fix(vaccination): clean live tracker assignment totals (2026-09-22). Repeat count 38 since 2026-08-01. Derived spec `lane2.no-double-dosing`. |
| `pen_label_not_in_catalogue`<br>Is every pen label on an animal a real pen label from the catalogue? | Counts breakdown | high | green | `39ec65231` fix(counts): reject blank legacy partition labels (2026-09-04). `bdc50109b` Fix lump-sum weighing census for partition aliases (2026-08-31). Repeat count 12 since 2026-08-01. Derived spec `lane2.partition-label-is-catalogued`. |
| `treatment_course_open_past_its_length`<br>Are treatment courses being closed within the number of days they were given? | Health analytics | medium | green | `65ca12386` chore(health): renumber the three migrations past main's 000384 and 000385 (2026-09-22). `0842c4c3b` chore(health): renumber again past main's 000383 (2026-09-22). Repeat count 6 since 2026-08-01. Derived spec `lane2.health-cases-close-on-time`. |
| `signed_off_feed_work_without_proof`<br>Does every signed-off feed job still have the photo it was signed off on? | Verification | high | green | `b8e726ce2` fix(tasks): gate shared workflow routes by the workflow's own module (2026-09-19). `49a8dcc28` revert(tasks): a bare legacy proof_ref stays the client's video, as on main (2026-09-17). Repeat count 6 since 2026-08-01. Derived spec `lane2.completion-verified-has-its-proof`. |
| `repeat_dose_with_more_than_one_open_successor`<br>Does each repeating dose have exactly one next dose open? | Vaccination plan | medium | green | `a9d54f5c9` fix(vaccination): honor anchor events during generation (2026-09-01). `d3944496f` fix(vaccination): carry over medical-equivalent work (2026-09-01). Repeat count 6 since 2026-08-01. Derived spec `lane2.one-open-successor-per-repeat-dose`. |
| `check_filed_under_the_wrong_pen`<br>Is every check filed under the pen the work was actually done in? | Verification | medium | green | `a8e58fe2f` fix(vaccination): scope anchor publish idempotency (2026-08-31). `dec8a6232` fix(herd-signals): wire mqtt bridge into stg deploy (2026-08-23). Repeat count 5 since 2026-08-01. Derived spec `lane2.check-filed-under-the-right-pen`. |
| `sop_without_exactly_one_published_version`<br>Does every work instruction have exactly one published version? | Work instructions | high | green | `4c67500e2` fix(sop): a number answer out of range names only the bounds the author set (2026-09-17). `d893cabc9` fix(sop): a number answer out of range names only the bounds the author set (2026-09-17). Repeat count 2 since 2026-08-01. Derived spec `lane2.one-published-sop-version`. |
| `open_task_on_an_unpublished_sop`<br>Do open tasks still point at a published set of steps? | Tasks | high | green | `8df9414cd` fix(vaccination): retire stale disqualified rows (2026-09-01). `228d97e7c` fix(vaccination): retire replaced plan anchors (2026-08-31). Repeat count 2 since 2026-08-01. Derived spec `lane2.open-tasks-use-the-published-sop`. |
| `approval_without_who_or_when`<br>Does every approval or rejection record who did it and when? | Verification | medium | **11** | `dd5a90e06` feat(verifier): video-review telemetry ingest + CEO integrity aggregate (2026-08-06). `1ec4c465a` chore(api-client): regenerate app-api client from the verdict-gate spec (2026-08-04). Repeat count 2 since 2026-08-01. Derived spec `lane2.verification-verdicts-are-complete`. |
| `current_feed_rate_names_a_retired_feed`<br>Does every current feed rate name a feed that is still in use? | Feed configuration | high | **9** | `75e788602` People/HRMS: per-person module capability catalog + role backfill (2026-08-24). Repeat count 1 since 2026-08-01. Derived spec `lane2.feed-rates-name-a-real-feed-item`. |
| `count_exception_unresolved_over_7_days`<br>Are counting exceptions being resolved, or left sitting? | Counts breakdown | medium | **67** | `bf8ff5878` chore: enforce telemetry and exception standards, docs and E2E tooling (2026-08-05). Repeat count 1 since 2026-08-01. Derived spec `lane2.count-exceptions-not-ignored`. |
| `workflow_step_counts_disagree`<br>Does the step count on a record match the steps it actually has? | Workflows | medium | **1** | `fb50c9f74` fix(tasks): the service opens a sale workflow without an animal (2026-09-19). `0376e86ef` fix(tasks): keep work instructions out of counts workflows (2026-09-18). Repeat count 4 since 2026-08-01. Derived spec `lane2.workflow-step-counts-agree`. |
### How strong each commit link is

30 of the 42 checks come from the roadmap's explicit lane 2 list; the other 12 come from the
history miner's derived specs on `origin/auto/hist-20260923`, which routed 170 commits since
2026-08-01 into 50 lane 2 check IDs. Every SHA in the table above was resolved against this
repo's history before it was written down.

**The link is strongest** where the repeat count is high and the commits are subject-matched:
`vaccination_recorded_twice_for_same_dose` (38 commits), `pen_label_not_in_catalogue` (12),
`vaccination_round_unfinished_over_2_days` (12), `herd_total_vs_status_breakdown` (11).

**The link is weaker** where the check rests on one or two commits, or where the miner routed a
commit to a check by **file path alone** rather than by what the commit says — the coordinator
flagged 495 such path-only links across all lanes. Treat the commit list on these as "this area
has been churning", not as "this exact bug shipped": `count_exception_unresolved_over_7_days`,
`current_feed_rate_names_a_retired_feed`, `orphan_load_record` and `future_dated_work_record`
each rest on a single miner row. They are kept because each one found, or could plainly find,
something a person reads on a screen — but the coverage claim should not be inflated.

**Thresholds are chosen, not assumed**, and the reason is in the catalogue next to the check:

- `capture_after_submission` uses **one hour**, not one minute. A phone whose clock has drifted
  by seconds must not raise an alert on every run; an hour is well past drift and still catches
  a proof attached to the wrong piece of work.
- The "stuck" checks use **two days** for work that is meant to close on the day, and **seven
  days** for approvals, matching the roadmap's own wording.
- `implausible_weight_jump` fires on a doubling or halving **within 60 days**, not on any large
  delta, so ordinary growth over a long gap between weighings is not a finding.
- `pen_average_disagrees_with_pen_total` allows **0.5 kg** for rounding.

## Parked, with reasons — nothing here is faked green

| Parked | Why |
|---|---|
| **A dedicated read-only database login** | `goatos_app` can write all 22 tables this lane reads and can create in `public`. Every report says so. The checks are still safe (session + transaction read-only, single capped SELECT, no statement chaining), but the connection identity is not, and this lane will not claim otherwise. Needs a read-only role on the STG replica. |
| `lane2.feed-row-quantity-matches-its-rate` | Not run. The derived rule expects a feed row's quantity to be heads × rate, but on the live replica it fires on 26,066 of 27,394 rows: the issued quantity is the day's ration split across sessions, which the rule does not model. A check that fires on 95% of rows is noise. Park until the session split is modelled. |
| `lane2.notifications-are-not-stuck` | Not run. The 2,535 rows it returns are all in terminal states the system sets on purpose (`suppressed`, retries exhausted). Not a figure anybody reads on a Goat OS screen. |
| `lane2.messages-not-stuck-in-the-outbox` | Not run. Outbox depth is infrastructure health, not a number on a screen. Lane 3 is the right home. |
| `lane2.exited-animal-not-in-a-pen` | Not run as written. All 160 sold or dead animals still carry their last pen, and that is deliberate — the register filters by status, so nothing wrong appears on any screen. The visible half is covered instead by `register_status_disagrees_with_animal`. |
| **The local proxy on `127.0.0.1:5455`** | Was listening but closed every connection during this work (`server closed the connection unexpectedly`). The run used the second proxy already running on this machine, `127.0.0.1:15433`, against the **same** instance `goatos-stg:asia-south1:goatos-stg-core-db`. The lane reads whatever `GOATOS_STG_READONLY_DATABASE_URL` points at, so this is an environment note, not a code change. |
| **The `data-sanity` layer's label in Slack** | On the generic failure card the shared `layerText()` prints the raw layer name, so the "Failed checks" line reads `*data-sanity*`. It is one entry in `labelByLayer` to fix, but that is a shared function three lanes are editing, so it is left for one pass after all three land rather than risked as a three-way conflict. The sentence beside it is already plain English. |
| **Visual issues no longer come first** | The original lane 2 brief asked for the data section to sit below lane 1's visual issues. Lane 4's registry — now the frozen interface — inserts every finding kind's blocks straight after the header instead. Lane 2 takes the lower severity (20) so it never leads over a broken action, but on a mixed message its section does sit above lane 1's "What is wrong". Flagged rather than worked around, because reshaping `applyFindingKinds` is exactly what the coordinator froze. |

## The Slack upload path, which no test reaches

Lane 2 found, independently, that `postSlack()` referenced an undefined `inline` when setting
the `initial_comment` on the attachment upload — a `ReferenceError` on every live alert, thrown
*after* the message had already posted, so alerts arrived with no report and no screenshots. The
one-word fix was dropped in favour of **PR #367**, cherry-picked onto this branch, which extracts
`evidenceComment()` so the line is reachable from `--self-test`, asserts both branches, and reads
the `postSlack` source to refuse any identifier not in scope there. A one-word fix would have left
the next one equally invisible.

That bug is worth more than itself, because of *why* it survived: **`GOATOS_DASHBOARD_SLACK_DRY_RUN=1`
returns before the first upload.** So `--self-test`, every dry run, and lane 2's own
byte-identical proof all step straight over threaded replies, file uploads, captions and
`thread_ts`. The byte-identical proof is a proof about **block JSON**, not about what the channel
receives.

What lane 2 puts on that untested path, and what was done about each:

| On the path | Risk | What is done |
|---|---|---|
| `renderReplies()` returning one reply | It calls `writeHtmlReport()` and `assertPlainEnglish()`, either of which could throw | `renderReplies()` cannot throw under any input: the whole body is guarded, the caption falls back to a constant if the scrub refuses it, and the self-test drives three hostile findings through it |
| the reply's `file` | `notify-slack.mjs`'s own `catch` calls `path.basename(shot.file)`, so a reply naming a missing file crashes the alert from **inside** the handler meant to protect it | the reply is only emitted after `existsSync()` on the written file; asserted in the self-test |
| uploading an `.html` file | Slack may reject the type | lane 1 already uploads `report.html` on this exact path, so the precedent holds — but it has never been observed succeeding either |
| the caption length | Slack caps `initial_comment` | truncated to 2,900 characters |

**Still not provable from this lane.** `slackApi()` hardcodes `https://slack.com/api/...`, so
there is no way to drive the real upload path against a stub without editing a file three lanes
are sharing. The honest position is that **lane 2's Slack reply has never been executed against
anything** — only its inputs have. The smallest fix is a `GOATOS_DASHBOARD_SLACK_API_BASE`
override on `slackApi()` and `uploadSlackFile()`, which would let all three lanes run the upload
path against a local stub. That is proposed, not taken, because it is a shared file.

**One behaviour change lane 2 causes on that path.** Replies are pushed into `inlineShots`, and
the attachment list is chosen by `inlineShots.length ? [reportFile] : [reportFile, ...screenshotFiles]`.
So on a run where lane 1 found issues but none of them carried a per-issue screenshot, lane 2's
reply now makes that list take the first branch and the fallback screenshots are no longer
attached. It is a narrow case and the line belongs to lane 4's registry, so it is reported rather
than edited here.

## What was actually run, and what was not

Run, on 2026-09-23 against the STG-backed production replica, read-only:

- All 42 checks. 25 came back clean, 17 found rows that should not exist (848 rows in total),
  1 item parked (the read-only login). Nothing errored — every table and column in the
  catalogue was verified against the live `information_schema` before the check was written.
- `transaction_read_only = on` and `default_transaction_read_only = on` were read back out of
  the session and are in the report.
- `node mesha-ops/dashboard-automation/tooling/check-data-sanity.mjs --self-test` — pass.
- `node --test mesha-ops/dashboard-automation/tooling/check-data-sanity.test.mjs` — 17/17 pass, including
  the CTE-LIMIT bypass regression and the two end-to-end Slack rendering tests.
- `node mesha-ops/dashboard-automation/tooling/notify-slack.mjs --self-test` — pass, with lane 4's registry
  and both lanes registered.
- Lane 4's `a lane-1-only receipt renders byte-identically to the pre-registry rendering` golden
  test, run against this branch — pass.
- `node mesha-ops/dashboard-automation/tooling/run.mjs --self-test` — pass.
- The Slack message was rendered with `GOATOS_DASHBOARD_SLACK_DRY_RUN=1` to a file. **Nothing
  was posted to Slack.**

Not run:

- `node mesha-ops/dashboard-automation/tooling/run.mjs --mode production-smoke` end to end. The laptop was
  at load average ~124 with another session running `make land-main`, and a full run starts the
  browser sweep. The `data-sanity` layer's wiring is covered by `run.mjs --self-test` and by
  running the check exactly as that layer invokes it, but **the layer has not been exercised
  inside a real end-to-end run**. It needs one cycle on the OCI box before this lane is trusted
  unattended.
- Anything on the OCI box. All reads in this work came from the local proxy to the same STG
  instance the box reads.
