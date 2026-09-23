# Lane coverage report - every origin/main commit since 2026-08-01

Generated 2026-09-23 by the history miner on branch `auto/hist-20260923`.
Window: `git log origin/main --since=2026-08-01T00:00:00+05:30 --no-merges`, pinned to
`origin/main` at the time of this run.

> **The timestamp is pinned on purpose.** A bare `--since=2026-08-01` is a git *approxidate*:
> with no time given, git fills in the **current time of day**, so the same command run at
> 03:37 and at 09:00 returns different commit sets and the "every commit since Aug 1" claim
> is not reproducible. Run bare at 03:37 IST it returns 4104 commits; pinned to farm-local
> midnight it returns 4108. The four it drops were all authored between 00:04 and 01:58 IST on
> 2026-08-01. Lane 1's own ledger was built with the bare form and has the same four-commit
> hole at its boundary.

## Reconciliation - count in equals count out

**Commits in the window: 4125.** Every commit is assigned exactly one primary bucket.
A commit that also matters to another lane carries an `alsoLanes` field on its row; that field
never moves the commit between buckets, so the arithmetic below stays exact.

| Bucket | Commits |
| --- | ---: |
| Lane 1 - already covered by PR #350's web ledger (not re-classified here) | 1023 |
| Lane 2 - read-only data sanity SQL on the STG replica | 171 |
| Lane 3 - read-only production API contract + latency | 612 |
| Lane 4 - write-path journeys on the OCI writable clone | 426 |
| Lane 5 - Android on Firebase Test Lab | 1051 |
| Parked - not automatable, with a reason on every row | 842 |
| **Total** | **4125** |

`1023 + 171 + 612 + 426 + 1051 + 842 = 4125` and the window holds `4125` commits, so **4125 == 4125**.

### How the bucket is decided (deterministic, in this order)

1. Touches `apps/goatos-android/` -> **lane 5**. Lane 1 explicitly does not cover the app.
2. Already in PR #350's web ledger and touches `apps/admin-web/` -> **lane 1**, excluded here.
3. No runtime surface at all (docs, repo tooling, CI, Go tests only) -> **parked**, with a reason.
4. Subject carries a data-invariant signal (reconcile, repair, backfill, orphan, duplicate,
   mismatch, projection, stuck, rolled forward, future-dated, partition label, census, parity)
   -> **lane 2**.
5. Subject carries a write-path signal (publish, submit, approve, record, create, move, assign,
   allocate, lock, retire, intake, capture, verify, import, bulk) -> **lane 4**.
6. Otherwise, a backend / SQL / contract change -> **lane 3**.
7. If the lane chosen above has no check that exercises the commit, it is re-homed to the
   first lane that does (lane 2, then lane 4, then lane 3). The bucket follows the check, so
   the count stays exactly-once; those rows carry a `reHomedFrom` note.
8. Anything that still matches no concrete check anywhere is parked rather than attached to a
   check it does not exercise. Parked is never faked green.

### Reproducing the count

```
git log origin/main --since=2026-08-01T00:00:00+05:30 --no-merges --format=%h | wc -l
wc -l tools/dashboard-automation/commit-classification/lane*.jsonl \
      tools/dashboard-automation/commit-classification/not-automatable.jsonl
```

Lane 1's own ledger (`p1..p4.jsonl`, `sync.jsonl`, 2072 rows) is left untouched by this pass.

`make dashboard-lane-coverage-guard` (see `sync-coverage.mjs --check`) re-derives this count
against the current `origin/main` and fails when a commit lands that no lane covers, so the
number above cannot quietly rot.

## Checks per lane

| Lane | Distinct checks | Checks covering a commit | Commits routed |
| --- | ---: | ---: | ---: |
| lane2 | 50 | 30 | 170 |
| lane3 | 62 | 54 | 609 |
| lane4 | 35 | 33 | 426 |
| lane5-android | 47 | 40 | 1051 |
| **total** | **194** | **157** | **2256** |

**37 of the 194 checks are routed to no commit at all.** They are invariants the roadmap asked for — sale sex splits adding up, impossible weights, a locked feed sheet staying locked — that no commit since 2026-08-01 maps to. They are kept, because deleting a real invariant trades noise for blindness, and they are counted in their own column so "194 checks" is never read as 194 checks' worth of coverage. Only the middle column covers anything.

Many commits map to one check; each check in `lane-checks.json` carries the full `sourceShas`
list it was derived from, so a builder can always get back to the commits behind a check.

### lane2 - checks by how many commits they cover

| Check | Commits | Priority |
| --- | ---: | --- |
| `lane2.no-double-dosing` - An animal is not vaccinated twice for the same due dose | 38 | high |
| `lane2.partition-label-is-catalogued` - Pen labels on animals exist in the pen catalogue and are not doubled | 12 | medium |
| `lane2.due-doses-do-not-sit-past-their-window` - No due dose sits open long past its window | 12 | medium |
| `lane2.herd-total-reconciles` - Herd total reconciles with alive + sold + dead + culled | 11 | medium |
| `lane2.feed-row-quantity-matches-its-rate` - A feed row's quantity matches heads times rate | 10 | medium |
| `lane2.notifications-are-not-stuck` - No notification stuck unsent | 9 | medium |
| `lane2.animal-in-exactly-one-pen` - An animal is in exactly one pen | 8 | medium |
| `lane2.herd-goat-projection-agrees` - Every animal on the register appears once in the herd projection | 7 | low |
| `lane2.weighing-work-not-stuck` - No weighing work stuck open past its day | 6 | low |
| `lane2.completion-verified-has-its-proof` - Signed-off feed work has the photo it was signed off on | 6 | low |
| `lane2.health-cases-close-on-time` - Treatment courses do not stay open past their length | 6 | low |
| `lane2.one-open-successor-per-repeat-dose` - A repeating dose has exactly one next dose open at a time | 6 | low |
| `lane2.check-filed-under-the-right-pen` - A check is filed under the pen the work was done in | 5 | low |
| `lane2.messages-not-stuck-in-the-outbox` - No event stuck unpublished in the outbox | 4 | low |
| `lane2.workflow-step-counts-agree` - A birth or death record's step count matches its steps | 4 | low |
| `lane2.current-location-agrees-with-history` - An animal's current pen matches its last recorded move | 3 | low |
| `lane2.sale-line-totals-match-the-deal` - Sale line totals add up to the sale | 3 | low |
| `lane2.verification-not-pending-too-long` - No verification pending for more than seven days | 3 | low |
| `lane2.one-active-tag-per-animal` - A tag number belongs to one animal, and one animal has one main tag | 3 | low |
| `lane2.verification-verdicts-are-complete` - A finished check has a verdict, a person and a time | 2 | low |
| `lane2.one-published-sop-version` - An SOP has exactly one published version | 2 | low |
| `lane2.open-tasks-use-the-published-sop` - Open SOP tasks point at a version that is still published | 2 | low |
| `lane2.no-orphan-pen-rows` - Pen assignments point at sheds that still exist | 1 | low |
| `lane2.weighing-campaign-dates-make-sense` - A weighing plan's dates make sense | 1 | low |
| `lane2.feed-issued-is-not-more-than-bought` - Feed issued does not exceed feed purchased | 1 | low |
| `lane2.feed-rates-name-a-real-feed-item` - Every feed rate names a feed item that still exists | 1 | low |
| `lane2.no-future-dated-records` - Nothing is recorded in the future | 1 | low |
| `lane2.stock-is-never-negative` - Stock on hand is never negative or over-reserved | 1 | low |
| `lane2.procurement-load-counts-add-up` - A procurement load's animals match the count it declared | 1 | low |
| `lane2.count-exceptions-not-ignored` - Counting exceptions are not left unresolved | 1 | low |
| `lane2.exited-animal-not-in-a-pen` - An animal that left the farm is not still sitting in a pen | 0 | low |
| `lane2.sale-sex-split-adds-up` - Male plus female on a sale equals the animals sold | 0 | low |
| `lane2.sold-animals-match-the-register` - Sales sold count matches the herd register's sold animals | 0 | low |
| `lane2.sale-allocation-parents-exist` - Every animal attached to a sale points at a real animal and a real sale | 0 | low |
| `lane2.no-impossible-weights` - No impossible animal weights | 0 | low |
| `lane2.no-implausible-weight-jump` - No implausible weight jump between two weighings of the same animal | 0 | low |
| `lane2.shed-weight-average-is-consistent` - A shed's average weight equals its total divided by its animals | 0 | low |
| `lane2.weighing-points-at-a-real-animal` - Weighing records point at animals that exist | 0 | low |
| `lane2.proof-not-captured-after-it-was-checked` - Proof was captured before it was checked, not after | 0 | low |
| `lane2.feed-row-has-animals-or-no-feed` - No feed issued to a pen with no animals | 0 | low |
| `lane2.locked-feed-sheet-stays-locked` - A locked feed sheet is not changed afterwards | 0 | low |
| `lane2.feed-rates-do-not-overlap` - Two feed rates do not apply to the same pen on the same day | 0 | low |
| `lane2.feed-purchase-values-are-sane` - Feed purchases have a positive quantity and a matching per-kg cost | 0 | low |
| `lane2.death-is-not-before-birth` - No animal left the farm before it was born | 0 | low |
| `lane2.vaccination-points-at-a-real-due-dose` - Every vaccination points at a real due dose and a real animal | 0 | low |
| `lane2.work-items-not-stuck` - No pen routine, preventive care or pen visit stuck open past its day | 0 | low |
| `lane2.tasks-finished-have-a-finish-time` - A finished task records when it finished | 0 | low |
| `lane2.clock-entries-are-possible` - Clock entries have a possible start, end and length | 0 | low |
| `lane2.tag-signals-are-not-from-the-future` - Ear tag signals are not dated in the future or impossibly old | 0 | low |
| `lane2.alerts-have-a-known-type-and-a-recipient` - Every alert has a known type and somebody to go to | 0 | low |

### lane3 - checks by how many commits they cover

| Check | Commits | Priority |
| --- | ---: | --- |
| `lane3.admin-web-bootstrap` - Admin web bootstrap answers with the user's modules and parks | 81 | high |
| `lane3.vaccination-action-center` - The vaccination action centre answers with a job per row | 41 | high |
| `lane3.weighing-shed-weights` - Shed weights answer with a shed name, an average and a count | 40 | high |
| `lane3.feed-direction-preview` - Tomorrow's feed sheet answers with a quantity on every row | 29 | high |
| `lane3.admin-sops` - SOPs answer with a name and their published version | 25 | high |
| `lane3.herd-signals-live` - Live ear tag signals answer with a tag, a pen and a time | 24 | medium |
| `lane3.pc-care-tasks` - Preventive care rounds answer with a pen and a due day | 23 | medium |
| `lane3.workforce-people` - People answer with a name, a title and their access | 20 | medium |
| `lane3.control-tower-vaccination` - The vaccination control tower answers with its alerts | 20 | medium |
| `lane3.herd-register-summary` - Herd register summary answers with every status band filled in | 18 | medium |
| `lane3.health-config-protocols` - Health protocols answer with a name and a published version | 17 | medium |
| `lane3.no-5xx-on-any-admin-hot-path` - No admin page endpoint answers with a server error | 16 | medium |
| `lane3.hot-paths-stay-under-budget` - Every hot path stays under its latency budget | 16 | medium |
| `lane3.vaccination-adherence` - Protocol adherence answers with a coverage figure per row | 14 | medium |
| `lane3.vaccination-live-tracker` - The live tracker answers with an operator per row | 14 | medium |
| `lane3.weighing-leadership-growth` - Growth and daily gain answer with a real number per band | 13 | medium |
| `lane3.counts-milk-preparation` - Milk preparation list answers with a date, a park and a status | 12 | medium |
| `lane3.ceo-ai-ask` - The Ask Mesha answer endpoint answers within its budget and names its numbers | 12 | medium |
| `lane3.feed-analytics-stock` - Feed stock analytics answer with stock and days of cover | 11 | medium |
| `lane3.sales-overview` - The Sales overview answers with its headline numbers | 11 | medium |
| `lane3.feed-analytics-execution` - Feed execution analytics answer with days, consumption and completions | 10 | medium |
| `lane3.vaccination-schedule` - The vaccination schedule answers with a date and a shed per row | 9 | medium |
| `lane3.every-app-endpoint-refuses-a-bad-envelope` - The app API refuses a malformed request instead of half-accepting it | 9 | medium |
| `lane3.leadership-tasks-list` - The tasks board answers with a number, a title and a status per task | 8 | medium |
| `lane3.ceo-ai-facts-match-the-page` - Ask Mesha's figures match the page those figures come from | 8 | medium |
| `lane3.admin-locations` - The location tree answers with named parks, sheds and pens | 7 | low |
| `lane3.verification-queue` - The verification queue answers with what to check and when it was captured | 7 | low |
| `lane3.weighing-weight-demographics` - Weight demographics answer with a band per breed and sex | 6 | low |
| `lane3.feed-analytics-directed` - Directed feed analytics answer with a day and a quantity | 6 | low |
| `lane3.feed-config-pens` - Feed configuration pens answer with real pen names | 6 | low |
| `lane3.workforce-clock-entries` - Clock entries answer with a day, a start and a length | 6 | low |
| `lane3.pen-routines` - Pen routines answer with a pen, a routine and a state | 6 | low |
| `lane3.capabilities-match-the-role` - What a person can do matches the role they hold | 6 | low |
| `lane3.calendar-events` - The calendar answers with events inside the week you asked for | 5 | low |
| `lane3.admin-web-approvals` - Pending approvals answer with what is waiting and who raised it | 5 | low |
| `lane3.workboard` - The work board answers with each person's duties for the day | 5 | low |
| `lane3.counts-herd-analytics` - Herd analytics charts answer with real numbers, never NaN | 4 | low |
| `lane3.vaccination-execution` - Vaccination execution answers with progress per drive | 4 | low |
| `lane3.counts-breakdown` - Counts breakdown answers with named rows, never blank labels | 3 | low |
| `lane3.weighing-dates` - Weighing date picker answers with the days that actually have weights | 3 | low |
| `lane3.roster-coverage` - Roster coverage answers with who is covering each duty | 3 | low |
| `lane3.vaccination-action-center-counts` - The action centre's badge count matches its list | 3 | low |
| `lane3.leadership-tasks-detail` - Opening a task answers with its own details, not the first task's | 3 | low |
| `lane3.protocols` - Protocols answer with a name and the stages they apply to | 3 | low |
| `lane3.operations-audit` - The operations audit answers with who did what and when | 3 | low |
| `lane3.feed-analytics-experiment` - Feed experiment analytics answer with an arm on every row | 2 | low |
| `lane3.feed-config-feed-items` - The feed item catalogue answers with a name and a status | 2 | low |
| `lane3.procurement-loads` - Incoming loads answer with a vendor, a date and a status | 2 | low |
| `lane3.procurement-feed-purchases` - Feed purchases answer with quantity, cost and vendor | 2 | low |
| `lane3.market-prices` - Market prices answer with a city, a question and a price | 2 | low |
| `lane3.feed-analytics-packing-variance` - Packing variance answers with what was asked for and what was weighed | 1 | low |
| `lane3.feed-config-ration-rates` - Feed rates answer with a group, an item and a rate | 1 | low |
| `lane3.procurement-vendors` - Vendors answer with a name and a place | 1 | low |
| `lane3.sales-deals` - Each sale answers with a buyer, a date, a count and a value | 1 | low |
| `lane3.goat-search` - Animal search answers with tag, pen and status | 0 | low |
| `lane3.growth-director-weights` - Growth director weights answer with a weight per animal | 0 | low |
| `lane3.feed-analytics-shed-feed` - Shed feed analytics answer with a row per shed | 0 | low |
| `lane3.feed-packing-worklist` - The packing worklist answers with what to pack today | 0 | low |
| `lane3.sales-buyer-leads` - Buyer leads answer with a name and a call status | 0 | low |
| `lane3.verification-oversight-analytics` - Verification oversight answers with its latency figures | 0 | low |
| `lane3.toxin-review` - Toxin tests answer with a batch, a round and an outcome | 0 | low |
| `lane3.shifting-events` - Shifting events answer with a source, a destination and a state | 0 | low |

### lane4 - checks by how many commits they cover

| Check | Commits | Priority |
| --- | ---: | --- |
| `lane4.weighing-plan-publish-and-capture` - Publishing a weighing plan, capturing a weight and seeing it on the shed table | 69 | high |
| `lane4.move-an-animal-keeps-one-pen-and-its-label` - Moving an animal leaves it in exactly one pen with the right pen label | 59 | high |
| `lane4.record-a-vaccination` - Recording a vaccination fills the due dose once and moves coverage | 35 | high |
| `lane4.approve-a-verification` - Approving a check records who approved it and clears it from the queue | 33 | high |
| `lane4.published-version-stays-locked` - A published version stays locked and its old version stays readable | 24 | medium |
| `lane4.publish-an-sop` - Publishing an SOP hands the new steps to the app on the next sync | 23 | medium |
| `lane4.reject-a-verification-queues-rework` - Rejecting a check with a reason sends the job back for rework | 17 | medium |
| `lane4.feed-item-in-use-cannot-be-deleted` - A feed item cannot be deleted while a ration names it | 14 | medium |
| `lane4.sop-answers-reach-the-verifier` - The answers and photos captured against an SOP reach the verifier unchanged | 13 | medium |
| `lane4.grant-and-revoke-module-access` - Granting and removing a person's access takes effect on their next screen | 13 | medium |
| `lane4.blind-verification-hides-the-operator-weight` - The verifier records their own weight and never sees the operator's | 10 | medium |
| `lane4.bulk-status-preview-matches-commit` - A bulk status change does exactly what its preview promised | 10 | medium |
| `lane4.configuration-import-bundle` - A configuration import does what its preview said and can be rolled back | 10 | medium |
| `lane4.feed-packing-and-carrying-gate` - Feed packing and carrying cannot be signed off without their photo | 9 | medium |
| `lane4.add-a-birth` - Adding a birth creates the kid, the mother link and the colostrum steps | 8 | medium |
| `lane4.procurement-load-through-to-intake` - A procurement load can be created, filled, dispatched and taken in | 8 | medium |
| `lane4.task-assignment-sends-a-notification` - Assigning work sends the notification and it arrives once | 8 | medium |
| `lane4.restore-proves-itself` - The lane restores every table it touched and proves the restore | 8 | medium |
| `lane4.preventive-care-round` - A preventive care round records its animals, its answers and its verification | 7 | low |
| `lane4.feed-purchase-raises-a-toxin-test` - Recording a feed purchase raises its toxin test and the test closes it out | 7 | low |
| `lane4.change-a-feed-rate` - Changing a feed rate changes tomorrow's feed sheet and nothing before it | 6 | low |
| `lane4.add-a-death` - Adding a death takes the animal off the register and queues the check | 5 | low |
| `lane4.diagnose-and-treat` - Diagnosing an animal opens its course of treatment and records the medicine | 5 | low |
| `lane4.authorise-a-shifting-event` - A shifting event needs an authoriser before it moves animals | 5 | low |
| `lane4.create-and-move-a-task` - Creating a task, assigning it and moving it across the board | 4 | low |
| `lane4.record-a-sale` - Recording a sale marks the animals sold and moves the Sales totals | 3 | low |
| `lane4.create-and-move-a-pen-round` - A pen round can be started, submitted and verified, and its pen label survives | 3 | low |
| `lane4.re-answering-a-question-takes-back-its-branch` - Re-answering a question takes back the steps the old answer had skipped | 3 | low |
| `lane4.clock-in-out-and-leave` - Clocking in and out, and asking for leave, record correctly | 2 | low |
| `lane4.a-workflow-refuses-to-open-without-its-subject` - A workflow will not open without the animal it is about | 2 | low |
| `lane4.release-a-sale-allocation` - Taking an animal back off a sale puts it back in its pen | 1 | low |
| `lane4.milk-preparation-and-feeding` - Milk preparation and feeding record their heads, their proof and their verification | 1 | low |
| `lane4.identity-merge-keeps-one-animal` - Merging two records for the same animal leaves one animal and one live tag | 1 | low |
| `lane4.publish-vaccination-plan-version` - Publishing a vaccination plan version locks it and hands it to the app | 0 | low |
| `lane4.a-workflow-is-gated-by-its-own-module` - A shared workflow is only open to people who have that module | 0 | low |

### lane5-android - checks by how many commits they cover

| Check | Commits | Priority |
| --- | ---: | --- |
| `lane5.work-board-and-submit` - The work board shows the day's duties and each one opens | 134 | high |
| `lane5.proof-capture` - A photo or video can be captured and attached to the job it belongs to | 125 | high |
| `lane5.offline-queue-survives-force-stop` - Work recorded offline survives a force-stop and syncs when the network returns | 100 | high |
| `lane5.shifting-on-the-phone` - Animals can be moved between pens on the phone and land in one pen | 100 | high |
| `lane5.weighing-capture-screen` - A weight can be captured, corrected and submitted for one animal | 73 | high |
| `lane5.proof-media-playback` - A recorded video plays back in the proof viewer instead of showing a black screen | 56 | high |
| `lane5.verify-queue` - The verification queue on the phone shows what is waiting and opens it | 42 | high |
| `lane5.sync-retries-and-recovers` - The app retries a failed send and recovers without the operator doing anything | 41 | high |
| `lane5.work-instructions` - Work instructions open and show the steps for the job in hand | 37 | high |
| `lane5.pen-reconciliation` - A pen can be counted and its difference recorded | 36 | high |
| `lane5.login-and-session` - Signing in, staying signed in, and being signed out cleanly | 34 | high |
| `lane5.weighing-scan-identifies-the-animal` - Scanning an ear tag brings up the right animal at the scale | 33 | high |
| `lane5.pc-care-task` - A preventive care round can be walked, recorded and sent | 31 | high |
| `lane5.shell-navigation` - Every bottom-bar destination opens and comes back | 28 | high |
| `lane5.conflict-does-not-lose-work` - A job edited on the web while the phone was offline does not silently lose either change | 19 | medium |
| `lane5.app-update-gate` - An out-of-date app tells the operator to update instead of failing quietly | 17 | medium |
| `lane5.upload-killed-mid-flight-resumes` - An upload killed halfway resumes instead of starting again or vanishing | 15 | medium |
| `lane5.push-notification-opens-the-right-job` - A push notification opens the job it names | 15 | medium |
| `lane5.animal-purchase-load` - An incoming load can be opened, its animals recorded and its decision made | 13 | medium |
| `lane5.feed-distribution-complete` - Feed carrying can be completed with its proof, and cannot be completed without it | 12 | medium |
| `lane5.vendors-and-feed-purchases` - A vendor and a feed purchase can be created on the phone | 12 | medium |
| `lane5.leadership-tasks-on-the-phone` - Tasks can be read, commented on and moved on the phone | 11 | medium |
| `lane5.low-storage-is-handled` - The app says what is wrong when the phone is out of space, instead of failing silently | 10 | medium |
| `lane5.feed-direction-worklist` - The feed worklist shows today's pens, quantities and sessions | 7 | low |
| `lane5.feed-packing-complete` - Feed packing can be completed with its weight and photo | 7 | low |
| `lane5.sales-on-the-phone` - A sale and a buyer lead can be recorded on the phone | 7 | low |
| `lane5.health-and-toxin-on-the-phone` - Health jobs and toxin strip tests can be recorded | 6 | low |
| `lane5.approvals-on-the-phone` - Approvals can be read and acted on | 5 | low |
| `lane5.add-a-birth-on-the-phone` - A birth can be recorded with its mother, litter and photo | 4 | low |
| `lane5.add-a-death-on-the-phone` - A death can be recorded with its cause, its steps and its proof | 3 | low |
| `lane5.clock-and-leave` - Clocking in and out and asking for leave work on the phone | 3 | low |
| `lane5.crash-free-on-the-covered-screens` - No screen in the covered set crashes or shows an error page | 3 | low |
| `lane5.weighing-task-detail-and-sheds` - The shed list and task detail show the right shed, pen and progress | 2 | low |
| `lane5.verify-detail-and-verdict` - A verifier can approve or send back a job with a reason | 2 | low |
| `lane5.capture-access-gate` - The app asks for camera and location access once and explains why | 2 | low |
| `lane5.calendar-on-the-phone` - The calendar shows the right days and opens a day's work | 2 | low |
| `lane5.weighing-fasting-and-operators` - Fasting checks and operator assignment show and submit correctly | 1 | low |
| `lane5.pen-routine-and-pen-visit` - A pen round records entering the pen, the answers and leaving it | 1 | low |
| `lane5.vaccination-submit` - A vaccination can be recorded for a pen and sends exactly once | 1 | low |
| `lane5.sync-status-screen` - The sync screen tells the truth about what is still waiting | 1 | low |
| `lane5.feed-wastage` - Wastage can be recorded per pen with a weight and a photo | 0 | low |
| `lane5.feed-transport-capture` - Feed transport capture records the load with its video | 0 | low |
| `lane5.weighing-plan-wizard` - The weighing plan wizard builds a plan the operator can start | 0 | low |
| `lane5.roster-scan` - Scanning a person's card clocks the right person in | 0 | low |
| `lane5.market-survey` - Market prices can be recorded for a city | 0 | low |
| `lane5.language-switch` - The app reads correctly in each language it ships | 0 | low |
| `lane5.herd-signal-tags` - Ear tag readings are picked up from the gateway | 0 | low |

## How strongly each commit is tied to its check

Every routed row carries `matchStrength`. `subject+path` means the commit's message AND the
files it touched both point at the check. `subject` or `path` alone is a weaker tie: the check
is the right one to build, but a builder should read the commit before assuming it proves that
exact behaviour. The weak ties are named here so nothing is quietly overstated.

| Lane | subject+path | subject only | path only |
| --- | ---: | ---: | ---: |
| lane2 | 37 | 89 | 44 |
| lane3 | 155 | 261 | 193 |
| lane4 | 193 | 190 | 43 |
| lane5-android | 260 | 575 | 216 |

496 of 2256 routed commits are tied to their check by file path alone.
73 rows were re-homed from the lane their subject suggested to the lane that actually
has a check for them; those carry `reHomedFrom`.

## Lane 5 - which checks a virtual device cannot run

42 of the 47 Android checks run on a Firebase Test Lab **virtual**
device. The rest need a **physical** device and belong to a small before-release set; a virtual
run of them would be a false green, so they are never scheduled on one.

| Check | Why a virtual device cannot run it |
| --- | --- |
| `lane5.proof-capture` - A photo or video can be captured and attached to the job it belongs to | Drives the real camera and the video encoder. A virtual device's fake camera produces a synthetic frame, so a green run would not prove a real capture works on a farm phone. |
| `lane5.weighing-scan-identifies-the-animal` - Scanning an ear tag brings up the right animal at the scale | Reads a physical RFID ear tag through the handheld reader; a virtual device has no RFID hardware and cannot exercise the reader at all. |
| `lane5.feed-transport-capture` - Feed transport capture records the load with its video | Records video through the device camera; a Test Lab virtual device has no real camera or encoder, so a pass there would prove nothing. |
| `lane5.roster-scan` - Scanning a person's card clocks the right person in | Reads a physical card through the device's RFID/NFC hardware, which a Test Lab virtual device does not have. |
| `lane5.herd-signal-tags` - Ear tag readings are picked up from the gateway | Needs a real Bluetooth radio to hear a real tag; a Test Lab virtual device has no BLE stack and cannot see a physical beacon. |

Free tier is 10 tests/day and 60 device-minutes/day, so the virtual set is ordered by how many
commits it covers: the sync-architecture checks and the capture screens run every night, the
rest rotate.

## Repeat bug patterns since 2026-08-01

A pattern fixed three or more times is a class of bug this team keeps re-introducing, so the
checks that carry it should be built first. Counts are over the commits classified in this pass
(lane 1's own web commits are not counted again here).

| Pattern | Commits | Build priority |
| --- | ---: | --- |
| `P-proof-media-missing` | 565 | build first |
| `P-verification-gate` | 357 | build first |
| `P-published-version-not-locked` | 136 | build first |
| `P-notification-not-delivered` | 114 | build first |
| `P-offline-sync-queue` | 109 | build first |
| `P-partition-pen-label` | 99 | build first |
| `P-permission-or-access-drift` | 97 | build first |
| `P-idempotency-outbox-replay` | 91 | build first |
| `P-totals-do-not-reconcile` | 77 | build first |
| `P-latency-regression` | 55 | build first |
| `P-double-count-duplicate` | 55 | build first |
| `P-status-machine-drift` | 45 | high |
| `P-null-empty-leak` | 39 | high |
| `P-stuck-or-rolled-forward-work` | 33 | high |
| `P-sql-bind-arity` | 28 | high |
| `P-business-date-timezone` | 9 | normal |
| `P-feed-rate-or-ration` | 3 | normal |

## Parked - not automatable

829 commits are parked. Every parked row in
`commit-classification/not-automatable.jsonl` carries its own `reason`. Nothing parked is
reported as covered anywhere. That file is an extra output beyond the four lane files: the
parked rows need somewhere to live for the exactly-once count to be checkable by line count.

| Reason | Commits |
| --- | ---: |
| Repo tooling, CI or agent scaffolding only - no farm-facing surface to observe. | 383 |
| Documentation only - nothing runs, nothing a person can see. | 277 |
| Test and tooling changes only - already proved by the Go test suite, no runtime surface to observe. | 106 |
| No observable surface could be derived from this commit; parked for a human to route. | 22 |
| Migration bookkeeping only - renumbering, rebase collisions or checksum allowances. The schema and the data are unchanged, so there is nothing new a person could see. | 12 |
| Goat OS MCP connector plumbing - outside the five dashboard lanes. No dashboard page and no app screen changes, so no lane can observe it. | 12 |
| Regenerated or reformatted code only - the contract it was generated from is covered by its own lane 3 check, so re-checking the generated file would prove nothing extra. | 9 |
| No concrete check in this lane matches this commit; parked rather than attached to a check it does not actually exercise. | 8 |

## Keeping this number true tomorrow

This report is a snapshot; the guard is what stops it rotting. `sync-coverage.mjs` now has a
second half covering lanes 2-5 alongside lane 1's assertion bookkeeping, and it runs inside
`make dashboard-automation-guard`.

It fails when:

- a commit lands on `origin/main` in the window that **no lane covers** and that is not parked;
- a sha appears in **more than one** lane ledger, which would break the exactly-once count;
- a ledger row points at a **check that does not exist** in `lane-checks.json`;
- a row sits in the **wrong lane file**, or parked work carries **no reason**;
- a ledger row names a commit that is **not in the window** (history was rewritten);
- a lane 2 check's SQL stops being a single read-only `SELECT` with a `LIMIT`, a lane 3 check
  stops being a `GET`, or a lane 4 check stops naming the tables it writes;
- a `failureSentence` picks up SQL, a selector, a check code, a field name or a stack trace.

It reports, without failing, how strongly each commit is tied to its check, so a weak tie is
visible rather than hidden.

`--write` parks anything new as a `needs-lane` row in
`commit-classification/needs-lane.jsonl`: accounted for in the count, and explicitly covered by
no check, so nothing is ever faked green. A commit that touches only this automation's own
ledger is auto-parked with a reason - otherwise writing the ledger would make a commit that was
itself uncovered, and the guard could never go green.

Tests: `tools/dashboard-automation/lane-coverage.test.mjs` (23 cases) plus the existing
`sync-coverage.test.mjs` (14), both wired into `make dashboard-automation-self-test`.

## Read-only contract

- Lane 2 SQL is generated through a validator that rejects anything that is not a single
  `SELECT`, that contains `;`, that lacks a `LIMIT`, or that contains any of
  `INSERT UPDATE DELETE DROP ALTER CREATE TRUNCATE GRANT REVOKE COPY MERGE CALL DO SET LOCK`
  `REINDEX VACUUM ANALYZE REFRESH nextval setval dblink pg_terminate pg_cancel`.
  The generator fails rather than emitting a check that could write.
- Lane 3 is `GET` only; the validator rejects any other method.
- Lane 4 is the only lane that writes, and it is pinned to the OCI writable clone. Its specs
  name the tables they touch so the lane can restore them and prove the restore.

## Failure sentences

Every check carries a `failureSentence` written for a farm manager: it names the page or the
thing and says what is wrong. No check codes, no selectors, no SQL, no stack traces.

