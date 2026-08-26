# Toxin Module: Aflatoxin Strip Tests on Purchased Feed Loads

Maintainer decisions, 2026-08-25 (chat session with the maintainer's product owner).
Status: ACCEPTED, implemented on branch `feat/toxin-module`.

## What the module is

Every feed load recorded on `/procurement/feed-purchases` must be screened for
aflatoxins with the SafetiX SHF 001-A qualitative rapid strip kit before the farm trusts
the load. One test task is born **per feed-purchase row** (per load × feed type),
created automatically by the toxin consumer of `procurement.feed_purchase.recorded` —
never typed in by hand, so a load can neither be forgotten nor tested twice.

There is **no calendar and no due clock**: tasks sit in a flat pending list until done.

## The 7-step guided flow, with proof at every step

The farm's procedure deviates from the kit manual in one place: **no centrifuge** — the
shaken extract **sits for about one hour** to settle. The flow, as locked by the
maintainer:

| # | Step | Proof |
|---|------|-------|
| 1 | Take the feed sample out of the load | in-app-camera VIDEO |
| 2 | Grind and weigh 5 g | in-app-camera VIDEO |
| 3 | Mix in extraction solution, shake ~3 min | in-app-camera VIDEO |
| 4 | Let it sit — 1 hour (no centrifuge) | server-enforced WAIT |
| 5 | Dilute per kit table, fill the microwell | in-app-camera VIDEO |
| 6 | Place the strip to develop | in-app-camera VIDEO |
| 7 | Read the strip within 1 minute | in-app-camera PHOTO + reading |

**All three waits are HARD-BLOCKED on the server clock** (maintainer: "hard lock" /
"hardblock"): step 5 opens 60 minutes after step 3's video, step 6 opens 3 minutes
after step 5, step 7 opens 8 minutes after step 6. The phone renders the server's step
states (`done / available / waiting / locked` + `available_at`) and never computes gate
logic from its own clock. Canonical spec: `backend/internal/toxin/domain.Steps()` /
`CheckStepCompletable`.

**Steps are person-independent** (maintainer: "it's not that one only one person will do
all steps or he will do it in a through"): any `toxin.execute` holder may complete the
next open step; each completion records who did it. Each step's capture uploads through
the standard proof pipeline (register → signed PUT → complete, Android outbox) the
moment the step completes — nothing waits for a final submit.

**ONE CAPTURE PROVES ONE STEP.** A capture already recorded against any step in the tenant
is refused (409 `proof_already_used`), enforced by a unique constraint on
`toxin_test_step_completions (tenant_id, proof_ref)`. This exists because the proof
validator can only assert that a ref is a completed in-app-camera capture of the right kind
in this tenant — it cannot tell WHICH step was filmed — so without the rule a tester could
submit one clip six times and the evidence would read as complete. Tenant-wide rather than
per task: a clip from another load's test is no more usable than a repeat of this one.
Found while walking the live API during this build; pinned in the Postgres lifecycle test.

The step-7 reading is the kit's three outcomes: **Negative / Positive / Invalid strip**
(no control line = void strip).

## Retest = a NEW task, never an edit

An **Invalid strip** or a **rejected review** CANCELS the whole round and mints a fresh
retest task for the same load in the SAME transaction (`round_no + 1`, `origin`
`invalid_retest` / `rejected_retest`, lineage linked both ways). Evidence of the
cancelled round is permanent history. Exactly one LIVE round per load is enforced by a
partial unique index (`toxin_test_tasks_open_round_uq`). A reject does not name a step —
the whole process re-runs (maintainer decision #5).

## Review is CEO/CXO-only — and why the verifier lock stands

The maintainer's instruction: toxin review must NOT go to the tenant verifier; only
CEO/CXO accept or reject, on admin-web (a Toxin tab on the verify surface, visible only
to CEO/CXO).

This **deliberately does not touch** the 2026-08-03 verifier verdict-exclusivity lock.
Toxin is an **approval-gate module in the `counts_approver` shape**, not a generic
Verification category:

- Its verdict rides a dedicated permission, `toxin.verdict`, granted ONLY to
  `ceo_internal`, on its own routes (`GET /toxin/review`,
  `POST /toxin/tasks/{task_id}/verdict`).
- `verification.verdict` remains verifier-only; `ceo_internal` still does not hold it.
- The toxin category does not exist in the Verification type registry, the verifier
  lens, or `pendingModuleProfiles`, so the verifier cannot see toxin work by
  construction.
- The tester must not review their own test: `toxin_tester` carries no `toxin.verdict`.

Pinned by `TestToxinVerdictIsCEOOnly`, `TestToxinTesterCarriesOnlyTestingAuthority`
(backend/internal/permissions/toxin_permissions_test.go).

## Access is per person, never per job

Mobile module access (module key `toxin`, offered on the `toxin.read` permission) goes
to CEO/CXO and to the NAMED people who know how to run the test — today the two park
heads in `perPersonGrants` (`backend/cmd/seed-stg-login-grants/approvers.go`), via the
new per-person role `toxin_tester` (`toxin.read` + `toxin.execute` only; catalog row in
migration `000211`). A bare `park_head` / `pc_director` / `growth_director` job inherits
nothing. Pinned by `TestToxinModuleIsOfferedPerPersonNotPerJob` (mutation-tested:
deleting the offer branch turns it red).

`toxin.execute` is ORed into the `/app/proofs/*` upload routes (the weighing
phone-QA-2026-08-03 lever), or every step video would be permanently unsubmittable for a
tester who is not a general operator. Pinned by `TestToxinExecuteReachesProofUploads`.

## CEO/CXO WATCHES; the named testers DO (maintainer decision 2026-08-26)

Leadership holds `toxin.read` + `toxin.verdict` and **NOT `toxin.execute`**. They see
every load's test and cast the accept/reject; they never film a step. This corrects the
2026-08-25 shape, which also granted CEO execute "so leadership can run one too" — caught
on the phone, where a CEO principal opened a task card and could film step 1. That would
have let one person produce the evidence and then approve it, dissolving the separation
this module exists to keep.

Two halves, both required, per the capability-gated role-scoped UI lock:

1. **The endpoint.** `ceo_internal` no longer carries `ToxinExecute`, so
   `POST .../steps/{n}/complete` and `.../submit` refuse them at the route table. Pinned
   by `TestToxinExecuteIsTesterOnlyAndNeverCEO`, which also asserts the READ routes still
   resolve — withholding execute must not make the module vanish for leadership.
2. **The contract.** `can_execute` on `ToxinTask` is the CALLER's permission, resolved
   per request from the same grants the route table authorizes against
   (`callerCanExecute`, toxin's HTTP adapter). For a watcher the composed payload also
   renders every unfinished step `locked` rather than `available`, and the phone's list
   card stops opening (`openable = in_progress && canExecute`). Pinned by
   `TestWatcherSeesNoActionableStep` (toxin/adapters/http) and
   `a watcher sees the same open round as a card that does not open`
   (`ToxinTaskListViewModelTest`) — both mutation-tested when written.

The contract half is not decoration: without it the phone would show leadership a live
camera button whose write the server then rejects with a 403 the operator cannot act on.
Neither half alone is the fix.

## The list is sliced by three backend-owned chips (maintainer decision 2026-08-26)

The task list carries `All` / `Pending` / `Completed` across the top, **All selected by
default**. The client sends a `filter` KEY and never a status list, so what "Pending" includes
is one backend definition rather than a vocabulary each surface re-derives
(`domain.TaskFilters`).

`Pending` = `in_progress` + `pending_review`; `Completed` = `accepted` + `cancelled`. The two
are DISJOINT and together EXHAUSTIVE over every status, which is the property worth keeping:
a status added later without a home in one of them would be reachable only under All, so a
round would vanish from both working chips. `TestTaskFiltersPartitionEveryStatus` refuses
that. Cancelled sits under Completed because nobody touches such a round again — its
replacement already exists as its own round.

An absent OR UNKNOWN key resolves to All, never to an empty screen: a stale APK sending a
retired key must still see its work.

Counts on the chips are WHOLE-TENANT aggregates over the filter's statuses, never page-local
sums, so a badge cannot disagree with what the slice holds once the list pages.

The "nothing here" copy travels PER SLICE (`empty_message` on each chip), because one message
is wrong in two of the three: an empty Completed list showing "No feed loads waiting for a
test" tells the operator something false about finished work. That defect was caught on the
phone and is pinned in `ToxinTaskListViewModelTest`.

## Event spine

`procurement.feed_purchase.recorded` is emitted inside `CreateFeedPurchase`'s
transaction (`feed_purchase_outbox.go`); the toxin consumer
(`toxin/app.FeedPurchaseRecordedHandler`, wired in `kernelstages/bus.go`) creates the
round-1 task idempotently on `(tenant_id, feed_purchase_id, round_no)`. Registered in
`context/architecture/domain-event-registry.json` and the envelope schema enum.

**Trap, hit and fixed while building this (2026-08-25):** a new `aggregate_type` must be
registered in `public.validate_outbox_event_tenant()` or the outbox INSERT is refused with
SQLSTATE 23503. That trigger validates each *known* aggregate type against its owning
table and returns early; anything unrecognized falls through to a `goat_identity_events`
lookup and fails. The first version of this emitter used `aggregate_type='feed_purchase'`
with no branch, so every feed purchase 500'd on commit. Migration `000211` redefines the
function with a `feed_purchase` branch (the whole-function redefinition shape every prior
module used; `000183` is the precedent and the body the Down restores). The defect was
caught by `TestCreateFeedPurchaseEmitsRecordedEventInTheSameTransaction`, which is exactly
why the emitter has a Postgres-backed test rather than a fake-based one — a mock outbox
would have passed while production refused every purchase.

## v1 boundaries (deliberate, revisit only with a maintainer decision)

- An accepted **Positive** flags the load — it does **not** block feed issuing (the kit
  manual requires quantitative lab confirmation of positives).
- No FCM push for toxin submissions/verdicts yet.
- No admin-web authoring of toxin tasks; tasks are born only from feed purchases.
- No deadline clock on a test.

## What the system validates vs. what it cannot

Validated: task creation (automatic, once per load), per-step in-app-camera capture of
the declared media kind, step order, the three waits (server clock), immutable attempt
history, reject-with-reason, CEO/CXO-only verdict, replay-safe writes, full audit trail.
NOT validated (rests on the evidence + the reviewer): that the powder in the well truly
came from that load, and that weighing/dilution quantities were correct.

## Three field defects on the guided flow (2026-08-26)

All three were reported from the phone and fixed at the root; each is worth keeping written
down because the shape recurs.

**The screen froze on a wait.** Step state is SERVER-composed and time-dependent, but the
phone's cache is only written on a network event. The countdown ran to zero and the screen
stayed on `waiting` forever — the reading step never opened, so "Send reading" stayed dead and
the round could not be submitted at all until the tester left the screen and came back, or found
the sync button. The detail screen now re-reads the server when a wait elapses (armed off the
earliest waiting gate, a little past its instant for clock skew, bounded retries). The phone
still decides nothing; it only asks again. `RefreshOnResume` alone is not enough for a screen
whose state changes with the CLOCK rather than with the user.

**The captured strip photo was invisible and forgettable.** The only sign a capture had landed
was a button label flipping to "Take the photo again", which reads as "that did not take" — so
the strip got photographed over and over. Worse, the capture lived only in the ViewModel's
in-memory state, so any ViewModel death forgot it even though the upload was durably queued. The
screen now SHOWS the photograph, and the ViewModel observes the DURABLE proof slot
(`observeLatest`) instead of remembering an id; submit re-reads that slot so a photo taken before
a process death still sends. For a step whose deliverable is an image, the image is the
confirmation.

**A raw user id reached the operator's screen.** Step attribution rendered
`f94de67d-c8b0-527a-a285-857946dc4c95 · 26 Aug, 1:43 PM` — the same copy-firewall defect the
counts approval queue shipped once. The person is now resolved to a NAME in the same bounded
query (one LEFT JOIN, never a per-row lookup), and an unresolvable person is DROPPED rather than
printed as an id.

## Not a bug: the proof-download route is gated on `task.read` alone

Raised in review of PR #115 as a P1 — "CEO/CXO toxin reviewers cannot load proof media",
because `GET /app/proofs/{proof_id}/download` requires `TaskRead` while the toxin grant to
`ceo_internal` is `ToxinRead` + `ToxinVerdict`. Recorded here because the reading is a
reasonable one and will be made again.

It does not reproduce. The toxin entry in `RoleCEOInternal` is an **addition** to that role's
permission set, not the whole of it: `ceo_internal` is the founder-visibility role and already
carries `TaskRead` among ~80 permissions. Resolved through the real route table:

```text
role=ceo_internal   downloadProof=true   taskRead=true    toxinRead=true
role=toxin_tester   downloadProof=false  taskRead=false   toxinRead=true
role=verifier       downloadProof=true   taskRead=true    toxinRead=false
```

`ceo_internal` is the only holder of `toxin.verdict`, so every principal who can open the
toxin drawer can also fetch the media it shows. The drawer's photos resolve.

The property is pinned by `TestToxinReviewerReachesProofMedia`, which asserts that **every**
holder of `toxin.verdict` authorizes the download route — so a future verdict holder added
without `task.read` fails, which is exactly the defect the finding imagined. Mutation-tested
when written: removing `TaskRead` from `ceo_internal` turns it red.

**The one real edge, deliberately left open.** `toxin_tester` holds no `task.read` and cannot
call the download route. That is latent, not broken: no toxin surface resolves a server proof
URL. The phone renders its own capture from the durable proof slot and only ever *writes*
`proof_ref`; the admin-web drawer that reads proofs is CEO-only. If a tester surface ever needs
to show a previous attempt's photo — the natural case is a retest after a reject — widen the
route with `ToxinRead` the way the proof-upload routes already OR in `ToxinExecute`. Do not
hand the tester `task.read`, which would carry vaccination SOP reads with it.

## The 12-hour start clock (maintainer decision 2026-08-26)

A feed load sits in the store until its strip test clears it, so a test nobody picks up is a bag of
feed nobody can use. **Twelve hours after the load is recorded, the round is overdue.**

**It is an elapsed-time SLA, not a business-day deadline.** A load recorded at 22:00 is overdue at
10:00 the next morning — the feed has been waiting twelve hours either way. Vaccination's
day-grain rule is about work PLANNED for a day and must not be copied onto this, which is work that
arrives whenever a lorry arrives.

**The card and the reminder key on different things, deliberately.**

| | condition | why |
|---|---|---|
| **Overdue chip** (red) | still `in_progress` at 12h | a round somebody opened, filmed twice and walked away from is as overdue as one nobody touched — the load is uncleared either way |
| **Push reminder** | past 12h **and no step recorded** | telling someone visibly working through the steps to "start" the test is how people learn to ignore notifications |

**The operator's clock stops at submit.** Once the reading is in, the remaining wait is the
reviewer's; holding a tester's card red for a queue they do not control would blame the wrong desk.

**Overdue outranks the step countdown** on the chip: the operator must see that a load has been
sitting longer than the farm allows before they see which step is next. The countdown is still on
the detail screen, so nothing is lost.

**`accepted` reads "Completed", not "Reviewed"** — the tester's work and the reviewer's are both
finished, and "Reviewed" left testers unsure whether anything was still owed of them.

**The server decides, the phone renders.** `is_overdue`, `status_chip` and `status_tone` are all
computed backend-side. A device with a wrong clock would otherwise hide a late load or redden a
fresh one.

### No private scheduler, no private overdue calculation

The operational task-kernel lock forbids a module owning a scheduler, an overdue calculation or a
reminder ladder of its own. This satisfies it the way the daily low-stock alert does:

- the deadline is ONE definition, `toxin/domain.StartDeadline`, read by the chip, the SQL and the
  reminder alike, so the message and the screen can never disagree about what "late" means;
- the reminder rides the SHARED operational cadence (`kernelstages.ToxinOverdueStage`) and owns no
  schedule;
- "once per day per task" comes from the **business date in the idempotency key**, not from state —
  the first tick of the day writes, every later tick writes nothing. That survives a worker restart,
  a mid-day redeploy, and both instances of an HA pair running it at once.

The audience is resolved from the **grant**, not a job title: toxin testing is granted to named
individuals (`perPersonGrants`), so a future holder of either director seat does not inherit the
reminder by sitting in the chair. The CEO's office is included because it owns the module. An empty
audience is logged loudly and sends nothing — a message nobody receives must not look sent.

Copy names the load, per the meaningful-notification rule: *"Maize from Kamadhenu Feeds (P) Limited
at CBE (4200 kg, batch 12) arrived 25/08/2026 and the strip test has not been started. It has been
waiting 15 hours."* Pinned by `TestOverdueReminderNamesTheLoad`.
## The Feed → Toxin report (maintainer decision 2026-08-26)

A web page under Feed, `/feed/toxin`, answering what the task list cannot: which loads arrived,
who tested them, what the strip said, and whether the screening is keeping up.

**THE GRAIN IS THE FEED LOAD, NOT THE TEST — this is the whole design.** A delivery whose strip
came back void is retested, so it carries two or three `toxin_test_tasks` rows. It is still ONE
delivery. A per-task report would count that bag of feed twice and show two outstanding problems
where the farm has one. Every figure on the page is therefore taken from the load's LATEST round,
selected with `DISTINCT ON (feed_purchase_id) ORDER BY round_no DESC` — an EXACT collapse, not a
ranking guess, because `toxin_test_tasks_round_uq (tenant_id, feed_purchase_id, round_no)` already
makes it unique. Pinned by `TestReportCountsLoadsNotRounds`, which drives a void-then-retest
delivery through the real Postgres and asserts the report lists one row for three task rows.

**Chips are a partition.** All / Waiting / In review / Cleared / Flagged are disjoint and
exhaustive over every `(status, outcome)` pair, so a load appears under exactly one and the badge
on a chip always equals the rows that chip lists. `TestReportFiltersPartitionEveryStatusAndOutcome`
asserts it in the domain, and `TestReportBucketSQLMatchesTheDomainPartition` drives every pair
through BOTH the SQL `CASE` and the Go function so the two implementations cannot drift.

**An accepted POSITIVE is Flagged, never Cleared.** Reading "cleared" off `status='accepted'`
alone would tell the farm a contaminated load is safe to feed — the single most consequential line
on the page, pinned by `TestAcceptedPositiveIsFlaggedNotCleared`.

**Access.** `GET /feed/toxin/reports` is gated on `toxin.read`, NOT `toxin.verdict`: reading the
record is a different authority from accepting a positive, and a future feed-desk reader must not
need the second to do the first. Both halves per
`docs/decisions/role-scoped-ui-is-capability-gated.md` — the `toxin_report` page-contract control
is the UI half, the route permission is the endpoint half.

**The range picker governs the WHOLE page** (maintainer decision 2026-08-26): Last 30 days
(default) / 60 / 90 / All time, and it applies to the KPI strip, the charts, the chip counts AND
the loads table alike.

That is a reversal of the first cut, which windowed the analytics and left the table unwindowed so
an old untested load could not fall off the page. Once a range control is visible, that split
becomes the worse bug: the reader believes they are looking at a filtered table and they are not.
The honest fix is to window everything and then SAY what is hidden — `summary.outside_window_note`
counts untested loads that arrived before the range ("1 load arrived before this range and still
has no result") across all history regardless of the selected window, so a narrow range can never
quietly bury work nobody has done.

The oldest-waiting KPI note reads WINDOWED for the same reason, matching the Waiting count beside
it. Taken across all history it named a load that was not among the ones counted — "2 waiting,
oldest 50 days" when neither of those two was 50 days old.

**Suppliers to watch** ranks by the share of a supplier's loads that came back positive or void,
and excludes suppliers with a single load — one bad load out of one is 100% on no evidence.

Out of scope in this first cut, deliberately: no row drawer (the strip photo and step timeline stay
on the CEO review surface), and no park filter — the report is small enough to read whole.
