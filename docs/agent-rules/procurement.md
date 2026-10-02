# Procurement Agent Rules

> Moved verbatim from `AGENTS.md` (split 2026-09-24 to keep session start small).
> These rules are as binding as `AGENTS.md` itself. Only file location changed.

## PROCUREMENT IS SOP-DRIVEN END TO END (maintainer decision 2026-09-20)

Every procurement flow is AUTHORED on **Procurement › Procurement SOP** (`/procurement/sops`) with
the shared **List (default) | Flow** editors, and run by the engines that already own the work.
Six documents, each its own SOP so either half can be published without touching the other -- the
`sales.deal` / `sales.vendor` shape:

| SOP | What it authors |
|---|---|
| `procurement.animal_purchase` | the inspection + load FORMS (2026-09-14, unchanged) |
| `procurement.animal_purchase_intake` | the purchase load's WORKFLOW: record the animals, the office's decision, arrival at the farm |
| `procurement.vendor` | the SUPPLY register's form (split from `sales.vendor`, which keeps buyers) |
| `procurement.feed_purchase_intake` | a bought feed load's WORKFLOW: weighbridge slip, arrival, aflatoxin sign-off, the money |
| `procurement.feed_purchase_form` | what the Record feed purchase screens ASK |
| `procurement.toxin_test` | the aflatoxin PROCEDURE: the steps, their instructions, their waits |

**FIVE THINGS ARE NOT AUTHORED, and each is load-bearing.**

1. **The toxin ROUND stays the engine's.** The document says what the procedure IS -- how many
   steps, what each tells the tester, what is filmed, how long the extract sits, which step each
   wait gates. The state machine, the retest an Invalid strip mints, the CEO/CXO-only verdict, the
   reading vocabulary and the SERVER-CLOCK enforcement of every gate stay in `toxin`. That
   separation is what made it safe to open a medically-gated flow at all; it is the maintainer's
   recorded choice and reopening it needs a new one. `toxin/domain.Steps()` survives ONLY as the
   golden oracle for the seed -- nothing on the runtime path may read it (guard rule
   `toxin-steps-read-from-go`).
2. **A round runs the procedure it was OPENED on.** `toxin_test_tasks.sop_version` is stamped at
   creation, resolved IN SQL inside the writing transaction, and never changes. A RETEST is new
   work and is minted on whatever is published then. Under the row lock the repository refuses a
   procedure that is no longer the round's (`ErrProcedureChanged`).
3. **FOUR steps are engine-completed, never a tap**, because the fact each records already has an
   owner elsewhere and a tap would let the two disagree: `animal_purchase_decision` (completes
   when the load has NO animal still waiting -- the count is the PRODUCER's, taken inside the
   decision transaction), `feed_purchase_reached` (the ledger's own delivery write),
   `toxin_test_accepted` (an ACCEPTED round), and the sale's existing `sale_tag_animals`. Each
   needs a seeded task type carrying its hook (guard rule `engine-step-hook-missing`).
4. **Only the toxin ACCEPT is announced.** A rejected or Invalid round cancels itself and mints a
   retest in the same transaction, so the load is still owed a test and its step must stay OPEN.
   Announcing a reject would invite a consumer to read "we looked at it" as "it is done". The strip
   OUTCOME rides the event because an accepted Positive flags the load without blocking feeding.
5. **`.recorded` and `.reached` are two events, not one.** Reached is the load ARRIVING (stock, and
   the toxin task); recorded is the load being BOUGHT (the work owed on it). A load bought today
   and reaching on Friday emits both, three days apart.

**The FORMS share ONE engine**, told by an `EntryFormProfile` which section they live in, which ids
the module reads into typed columns, which must stay compulsory and which catalogs fill their
choices. Do not copy the validator for a sixth form. Typed ids are LOCKED because the downstream
reads are keyed on them; choices come from the module's own live vocabularies, never a list typed
into a document; a write names the version it rendered and is judged against exactly that one; a
client sending typed fields only (an older APK) is accepted unchanged.

**WHICH vendor document a row uses comes from the REGISTER's own data** -- the side its record type
belongs to (`procurement_vendor_catalog.register_side`) -- never from anything a client sends.

Canonical prose: `docs/decisions/procurement-sop-driven.md`. Machine gate:
`make procurement-sop-guard` (six rules, each with an adversarial self-test fixture). Migrations
`000370`-`000376`.

Confirmed TOXIN module rule (maintainer decisions 2026-08-25; a RECORDED, SCOPED exception
to the verifier verdict-exclusivity rule above that leaves that rule untouched): every feed
load recorded on `/procurement/feed-purchases` owes one aflatoxin strip test (SafetiX SHF
001-A), born automatically per feed-purchase row from `procurement.feed_purchase.reached`
— the load ARRIVING at the farm (maintainer decision 2026-09-03: a purchase is recorded when
bought, marked reached days later on `/procurement/feed-purchases`, and only then counts as
stock at the weight received and owes its test) — never hand-created, no calendar, no due clock. The test is a 7-STEP GUIDED FLOW with
PROOF AT EVERY WORKING STEP: steps 1/2/3/5/6 one in-app-camera VIDEO each, step 4 a
settling wait (the farm does NOT centrifuge — the extract sits ~1 hour), step 7 one
in-app-camera strip PHOTO plus the reading (Negative/Positive/Invalid). ALL THREE WAITS
ARE HARD-BLOCKED ON THE SERVER CLOCK (60 min after step 3 → step 5; 3 min → step 6; 8 min
→ step 7); the phone renders server step states and never derives gate logic from its own
clock. Steps are PERSON-INDEPENDENT among `toxin.execute` holders; each completion records
who. An Invalid strip or a rejected review CANCELS the whole round and mints a fresh
retest task in the SAME transaction (`round_no+1`; one live round per load, enforced by a
partial unique index); rejects require a reason and never name a step. REVIEW IS CEO/CXO
ONLY: `toxin.verdict` is granted to `ceo_internal` alone on its own routes — the module is
an approval gate in the `counts_approver` shape, deliberately NOT a Verification category,
so the verifier never sees toxin work and `verification.verdict` stays verifier-only.
Access is PER PERSON via `toxin_tester` (`perPersonGrants`; today the two named park
heads) — never on the park_head/director job. `toxin.execute` is ORed into the
`/app/proofs/*` routes. **CEO/CXO WATCHES AND JUDGES BUT NEVER RUNS A TEST (maintainer
decision 2026-08-26, correcting the 2026-08-25 grant): `ceo_internal` holds
`toxin.read` + `toxin.verdict` and NOT `toxin.execute`.** Do not add it back — a CEO who
could film the steps would be approving their own evidence. Enforced on BOTH halves per
the capability-gated lock: the step/submit routes refuse leadership at the route table,
AND `can_execute` on `ToxinTask` (the CALLER's permission, resolved per request) makes the
composed payload render every unfinished step `locked` and the phone card non-tappable, so
no camera is ever offered for a write the server would refuse. Pinned by
`TestToxinExecuteIsTesterOnlyAndNeverCEO`, `TestWatcherSeesNoActionableStep`, and the
`ToxinTaskListViewModelTest` watcher case. v1: accepted Positive FLAGS the load, does not block feeding; no
FCM. Canonical prose: `docs/decisions/toxin-testing-module.md`; pinned by
`TestToxinVerdictIsCEOOnly`, `TestToxinTesterCarriesOnlyTestingAuthority`,
`TestToxinModuleIsOfferedPerPersonNotPerJob` (each mutation-tested when written).

Confirmed THE APPROVE CARRIES THE NUMBER rule (maintainer decision 2026-08-20, SUPERSEDING
the separate-save-act half of the 2026-08-17 weighing weight-correction and 2026-08-18 feed
wastage measurement decisions): where a verification item declares a measurement, the verifier
types the value and presses **Approve ONCE**. There is **NO separate save button**, on the
phone or in the admin-web drawer.

**Why this is a lock and not a preference.** Recording the measurement RELABELS the
verification item, and the relabel is `row_version = row_version + 1`. The verdict UPDATE is
version-fenced (`AND row_version = $6`), so the Approve pressed straight after a save carried
the version the screen had loaded with, matched no row, and SILENTLY DID NOTHING. Two acts for
one judgement, the second broken by the first, with no error the verifier could see. Do not
reintroduce a save button: it recreates the defect exactly.

Four parts, each load-bearing:

1. **The number rides the verdict.** `measurement` on
   `POST /verification/items/{item_id}/verdict`. It names NO target — the record it lands on is
   resolved from the ITEM's own source, because a client that could name its own target could
   aim one item's approve at another item's record.
2. **Verification still does not know what the number MEANS.** It reaches the write through
   `verificationapp.MeasurementApplier`, registered per category at composition time exactly
   like the enqueue/withdraw/relabel seams producers already register. Each applier forwards to
   the SAME producer service its standalone route calls, so range checks, idempotency, audit and
   relabel are ONE implementation. Do NOT make verification read a producer's table.
3. **`RequiredForApprove` is TRUE wherever the reading is BORN on the verifier's screen.** Feed
   wastage's operator submits a VIDEO AND NO NUMBER, so approving blank would complete a pen-day
   with no wastage recorded at all — checked BEFORE the verdict, because the producer's own
   `ErrWastageMeasurementRequired` fires in the CONSUMER, after the verdict is durable, and
   strands the item mid-apply. Feed packing is the same shape, per feed item. **WEIGHING IS NOW
   ALSO TRUE, on BOTH grains** — see the blind-weighing lock below, which SUPERSEDES the original
   wording here ("weighing's operator already recorded a weight, so blank means 'his weight is
   right' and MUST stay a single tap"). Do not restore that sentence: it is now false on purpose.
4. **A REJECT never carries the number.** Rejection sends the work back to be recorded again, so
   a value written onto a record about to be redone is a number nobody will use. Reject is also
   never held on the measurement: a reading that cannot be taken off the clip is exactly the case
   that must be sent back.

Order inside one request: fence on the version she had on screen -> apply the measurement ->
re-read `row_version` (it moved through OUR relabel, not a competing verifier's) -> record the
verdict. Concurrency is still fenced, because the verdict UPDATE also requires the item to be
`pending`. A producer that refuses the value stops the whole approve rather than leaving an
approved item beside a number that never landed.

Both producer routes (`.../weight-correction`, `.../wastage/{id}/measurement`) STAY SERVED for
installed APKs that still show their own save button, and an item measured that way is still
approvable — the applier is asked whether a value is already recorded. No current client calls
them; do not build a new one that does.

Canonical prose: `docs/decisions/feed-distribution-verification.md` -> "THE APPROVE CARRIES THE
NUMBER". Pinned by `backend/internal/verification/app/verdict_measurement_test.go` (which keeps
the save-then-approve 409 reproduced as the defect being replaced) and the Android
`VerifyDetailViewModelAnalyticsTest` approve/reject pair; each was mutation-tested when written.

Confirmed THE VERIFIER IS WARNED, NOT TOLD rule (maintainer decision 2026-09-09, EXTENDING the
2026-08-21 blind per-item entry for feed packing, not retiring it): a packed weight the verifier
types that sits MORE THAN 500 g away from that feed item's plan is refused ONCE (422
`measurement_confirmation_required`, one field error per flagged box at
`measurement.entries.<key>` with code `above_plan` / `below_plan`), and the screen says only the
DIRECTION -- never the planned figure, never the gap, so she still cannot compute the plan. If she
is sure she ticks "I checked the video again" and presses Approve once more; the same approve
re-sent with `measurement.variance_acknowledged = true` lands and the reading is stored with
`planned_kg` and `variance_acknowledged` (migration `000287`). It is a CONFIRM, not a block (a hard
block was offered and declined), it is PER FEED ITEM, and the rule lives ONLY in the producer's
measurement applier BEFORE its write -- the plan is the completion's packed-against snapshot first,
the frozen sheet second -- so neither client knows the tolerance or the plan. The threshold is
`feeddirection/domain.PackingEntryConfirmToleranceKg` (0.5 kg) and is deliberately NOT the
leadership 0.2 kg `PackingVarianceToleranceKg`; do not merge them. Android keeps the server's
error CODE on the outbox row (`lastErrorCode`, `OUTBOX_MIGRATION_4_5`) so the screen keys on the
code, never on the sentence. Canonical prose: the same decision doc -> "THE VERIFIER IS WARNED, NOT
TOLD".

Confirmed THE DISTRIBUTION VERIFIER RECORDS THE TOTAL FEED rule (maintainer decision 2026-09-28,
applying the approve-carries-the-number and warned-not-told rules to feed DISTRIBUTION): the
distribution item carries ONE blind entry box, "Total feed given (kg)" (key `total_feed`), and
Approve is held until it is filled -- the feed is MIXED by the trough, so it is one combined weight
per pen-session, NEVER one per feed item; do not "align" it with packing's per-item boxes. A reading
more than 5% of the planned pen-session total away (the same pen-session's packed-against snapshot
unless that bag is in rework, else the frozen sheet summed over every item) is warned once with a
direction only (codes `total_above_plan` / `total_below_plan`, distinct from packing's so the web
copy keyed by code never says "500 g"). The tolerance is a PERCENTAGE on purpose
(`DistributionEntryConfirmTolerancePct`); do not merge it with packing's 0.5 kg. The reading lands on
`feed_distribution_completions.verified_feed_*` (migration `000455`) and a rework re-submit CLEARS it.
Requiring it locks `feed_distribution` sampling at 100%; 000455 deleted the stored sampling rows and
gave pending items the box. Canonical prose: the same decision doc -> "The distribution verifier
RECORDS THE TOTAL FEED".

Confirmed BIRTH EVIDENCE IS REVIEWED PER RECORDED STEP (maintainer decision 2026-09-16,
SUPERSEDING the one-bundle-per-track half of the 2026-07-28 birth rule): every video the operator
records on a kid or mother track -- each immediate step, each scheduled colostrum feed, Tag the kid,
each mother step -- is its own `birth_evidence` verifier item THE MOMENT IT IS RECORDED
(`ref_type=workflow_birth_action`, `ref_id=action_id`, that step's proofs only). The verifier no
longer waits three days for a kid track to finish, and a rejection no longer throws away ~14 good
clips: approve completes THAT step, reject sends back THAT step with the verifier's words on
`workflow_actions.rework_reason`, and the steps after it are NEVER held by the re-shoot (on a
per-step-reviewed template `in_review` and `rework` both satisfy a sequencing prerequisite --
`tasks/domain.StepRecorded` -- and the phone's optimistic pass agrees). The recorded step is locked
(`in_review`, 409 `action_in_review`) until its verdict. The card counts a recorded clip as the
operator's work done, reads "Awaiting verification" only when nothing is left to record, and
completes when the last clip is approved. The key is
`counts-birth-step:<action_id>:r<row_version>:<proofs>` so retries de-duplicate and a re-shoot
always opens a fresh item. Both mother and kid tracks; sampling applies as to any category; DEATH
IS UNCHANGED (two clips, admin approval first, one item, bounced pair re-shoots in order). The
retired `workflow_birth_signoff` bundle consumer stays only so a pre-cutover item lands its
verdict; nothing enqueues it. Canonical prose: `docs/decisions/birth-death-workflows.md` ->
"Birth is reviewed ONE RECORDED STEP AT A TIME". Pinned by
`domain.TestBirthStepUnderReviewOrSentBackNeverHoldsTheNextStep`,
`app.TestBirthStepReachesTheVerifierTheMomentItIsRecorded`,
`app.TestBirthStepRejectionSendsBackOnlyThatStep`,
`postgres.TestBirthEvidenceIsReviewedPerRecordedStepPg` and the Android
`WorkflowOptimisticSequenceTest` birth-rework case (each mutation-tested when written).

Confirmed BLIND WEIGHING VERIFICATION rule (maintainer decision 2026-09-21, SUPERSEDING the
optional-correction half of the 2026-08-17 weight-correction and 2026-08-20 approve-carries-the-
number decisions, for WEIGHING only): **the verifier is not shown the operator's weight, and the
number she types IS the weight.**

The maintainer's words: "whatever operators give that will be the weighing until verifier gives.
Whatever the verifier gives that will be the final weighing of that animal or that shed." So the
operator's number is the working weight while the proof waits, and the verifier's reading replaces
it as final — which is what `CorrectObservationWeight` already did. What changed is that she is no
longer ALLOWED to skip it, and no longer SHOWN what she is replacing.

Three parts, each load-bearing, and **any one alone is worse than none of them**:

1. **The label carries no weight.** `weighing/domain.CorrectedSubjectLabel` composes the sentence
   both surfaces render — pen + tag for an individual weigh, pen + frozen head count for a
   lump-sum one — and the `weightKg` PARAMETER IS GONE rather than passed and ignored, so a caller
   cannot put it back in one line that would read like a bugfix. The HEAD COUNT stays: it is
   snapshotted from the herd register at submit and frozen (2026-08-24), so it is not a number the
   operator typed and it anchors nobody; it says how many animals the pen total she is about to
   read covers.
2. **`RequiredForApprove: true`, on BOTH grains.** They share one category, and the decision names
   both. An unreadable video is a REJECT, never a guess.
3. **`HasRecordedMeasurement` must answer "has a VERIFIER set this weight", never "does this row
   have a weight".** It used to `return true` flat, which was CORRECT while the measurement was
   optional — verification only consults it for a `RequiredForApprove` category, so it was never
   asked. Left as it was, it would have waved through every unmeasured item, because EVERY weighing
   row carries a weight from the moment the operator captured it. It now reads
   `operator_weight_kg IS NOT NULL`, which is written on the first correction and never again, so
   its presence IS "a verifier has set this". This is the sharpest trap in the change.

**WHY BLIND.** The label was widened to carry the weight precisely so a verifier could catch a
120-kg-for-12-kg typo — but a reader who has already been told the answer confirms it. Hiding the
number is what makes her an independent second reading rather than a rubber stamp. The fix that
widening delivered must still survive: the SCANNED TAG stays on the label, because that, not the
weight, is what keeps fifteen rows from one pen distinguishable.

**IT LOCKS WEIGHING SAMPLING AT 100%, automatically.** `SamplingWaivable()` is derived from
`RequiredForApprove`, so weighing leaves the closeout's waivable list — it must, since
auto-approving an unwatched weighing video would complete a bucket with no verifier reading.
But `samplingsql.InSample` knows nothing about waivability and still reads whatever row
`verification_sampling_policies` holds, so a tenant already set below 100% would keep a narrowed
queue while the undrawn items are settled by NOBODY — pending forever, each holding its bucket
open against the unconditional close gate (ledger D-5). Migration `000383` DELETES the stored
weighing rows; the write path refuses new ones. **General rule: making a category non-waivable is
not complete until its stored sampling rows are removed in the same change.**

BOTH SURFACES, from one contract. Neither the admin-web drawer nor the phone decides any of this:
they render `subject_label` verbatim and honour `required_for_approve`, so the spec change reaches
both. Do not add a client-side weighing branch.

Canonical prose: `docs/decisions/blind-weighing-verification.md`. Pinned by
`TestWeighingApproveRequiresTheVerifiersOwnWeightReading`,
`TestWeighingSubjectLabelShowsTheVerifierNoOperatorWeight`,
`TestWeighingSamplingIsLockedBecauseTheVerifierIsTheDataSource`,
`TestSubjectLabelNeverCarriesTheWeightAtEitherGrain`,
`TestUnverifiedWeighingProofReportsNoRecordedMeasurement` and its two siblings — each
mutation-tested when written (reverting the spec flag, restoring the weight in the label, and
restoring the flat `return true` each turn one red).

Confirmed Approvals-on-mobile rule (maintainer decision 2026-08-05, SUPERSEDING the
2026-07-21 decision that removed approvals from mobile and moved them to admin-web
only): the birth/death/shifting approval queue is BACK on the phone, as its OWN
module (`approvals`), not as a tab inside Counts.

Two halves, and the second is the one a later session will break by accident:

1. **Approvals is a separate module.** Counts stays CAPTURE-ONLY on the phone
   (birth, death, shifting for holders of `counts.write`) and must NOT regain an
   approval tab. Approving is not capturing and the audiences barely overlap: the
   two named approvers hold no `counts.write`, and operators hold no approval
   authority. Pinned by `TestCountsModuleRoleMatrix`,
   `TestCountsModuleBarIsCaptureOnlyAndOmitsYouTab`, and the Android
   `TopLevelChromeTest`.
2. **The authority is granted PER PERSON, never per job.** The maintainer's words
   were "keep rbac per person, not per group". `permissions.RoleCountsApprover`
   (`counts_approver`) carries exactly `counts.approve_access` /
   `counts.approve_lifecycle` / `counts.approve_shifting` and NOTHING else — no
   bootstrap, no read, no write — and is granted to NAMED individuals alongside
   their job role. Today: Chandrakant (`pc_director`) and Dinakar
   (`growth_director`). Their job roles are byte-for-byte unchanged, so a future
   PC Director or Growth Director inherits no approval power by holding the job.

**Do NOT "simplify" this by adding the approval permissions to `pc_director` or
`growth_director`.** That hands the authority to every future holder of those
jobs, reverses the one-module-one-director segregation lock
(`director_module_segregation_test.go`), contradicts "growth_director runs
Weighing and ONLY Weighing", and overrides `health_director` as the documented
Counts owner. `TestApprovalsModuleIsPerPersonAndLeavesCountsCaptureOnly` and
`TestCountsApproverRoleCarriesOnlyApprovalAuthority` both go red if it is tried;
both were mutation-tested when written.

The named list is `perPersonGrants` in
`backend/cmd/seed-stg-login-grants/approvers.go` — adding an email there IS the
act of granting approval authority, on the phone and admin-web alike (one
permission, one set of routes, two surfaces). Role catalog row: migration
`000108_counts_approver_role.sql`. Canonical prose:
`docs/runbooks/current-active-rbac-roles.md` -> "`counts_approver` is granted by
NAME".

Reaffirmed and widened 2026-08-07: `perPersonGrants` is now the general
"this person, not this job" list. Chandrakant holds `counts_approver` + `operator`
alongside `pc_director`; Dinakar holds `counts_approver` + `pc_director` +
`growth_director` + `operator`. The maintainer was offered the alternative of
moving counts authority onto the `pc_director` ROLE and declined it, so the lock
above stands unchanged. Two consequences worth carrying forward: those `operator`
grants are TENANT-scoped (allowed only because they are layered on directors, and
each must carry a `stg-operator-scope: tenant approved` justification in its own
block — block-scoped enforcement in `check-stg-operator-scope.mjs`), and a tenant
`operator` grant does NOT add anyone to the vaccination drive operator pool, which
reads `workforce_positions` with `position_tier <> 'director'` rather than the RBAC
role.

RETIRED 2026-09-23. Extended 2026-08-07 (same day, second decision), those two ALSO
got the Herd Operations (Counts) CAPTURE module on the phone — birth, death,
shifting — keyed on the explicit tenant `operator` GRANT they held, never on their
director job. That is **no longer true**: `operator` was retired outright when the
farm's ground staff moved onto department manager roles, the maintainer was told
these two would lose ground capture on their phones, and chose it — they do not
record births, deaths or shifts. Migration `000394` revokes both grants and the
`hasRole(grants, RoleOperator)` branch in `leadershipModuleKeys` is deleted.

Their APPROVAL authority is untouched and is a different thing: `counts_approver`
still lets them approve birth / death / shifting on the Approvals module. Recording
is not approving — that separation is the whole point of the per-person approvals
rule above. If a leadership principal ever needs ground capture again, do NOT
resurrect a role-keyed branch: tick them the `counts` module on People / HRMS, which
is the per-person fact now and the mechanism every other module already uses. Pinned
by `TestHerdOperationsIsNeverOfferedFromARoleGrant`.

Why the GRANT and not the `counts.write` PERMISSION, which reads like the obvious
key and is wrong: `park_head` holds `counts.write` on the ROLE, and
`TestCountsModuleRoleMatrix` pins that a park head does NOT get the capture module.
Keying the offer on the permission compiled, passed the new test, and silently
handed Counts to every park head — the same per-job widening one layer over. That
pre-existing test is what caught it. The offer is now keyed on the `operator` grant,
which is exactly what `perPersonGrants` layers onto a named individual.

Note also that leadership module offers are NOT reachable from the database:
`candidateModuleKeys` returns `leadershipModuleKeys(grants)` for any principal
holding a leadership role and never consults `department_module_grants`. Giving a
director a module is therefore always a code change — there is no grant row that
does it. Pinned by `TestHerdOperationsIsOfferedPerPersonNotPerDirectorJob`
(mutation-tested three ways: branch removed, keyed on the job, keyed on the
permission — each turns it red).

SUPERSEDED 2026-10-02 (People / HRMS fixes): for a person WITH stored rows the phone menu is
their ticks whatever their role — a leadership role no longer caps it, so ticking a module on
People / HRMS gives it (`TestThePhoneMenuIsThePersonsTicksWhateverTheirRole`). The curated
`leadershipModuleKeys` still answers for a principal with no stored rows. Canonical prose:
`docs/decisions/people-hrms-access-fixes.md`.

Same change closed a copy-firewall defect on that queue: the phone used to render
`Raised by <uuid>` and `to shed <uuid>` because it composed the row's copy itself
from the echoed payload and had no name source. The backend now owns both lines
(`raised_by_name`, `summary_line` on `CountsApprovalListItem`), resolving ids to
names in ONE batched query per entity kind, and a fact whose name cannot be
resolved is DROPPED from the line rather than rendered as an id. Clients render
both verbatim; do not reintroduce client-side composition of that copy.

Confirmed VENDOR REGISTER TWO SIDES + SALES PHONE MODULE rule (maintainer decision 2026-09-05,
narrowing the SCREEN half — and only that half — of the 2026-08-27 "every buyer is a vendor"
decision in migration `000217`): the one vendor register is now read as TWO COMPLEMENTARY HALVES.
Each `record_type` declares its own side on `procurement_vendor_catalog.register_side` (migration
`000256`), and two pages read the same table through opposite halves: **Procurement > Vendors** the
supply desk's 35 types, **Sales > Vendors** the five buyer types (`Agent`, `Butcher`, `Company`,
`Farmer`, `Slaughter House`). Same table, same endpoint, same drawer, same form, same copy map —
ONE component renders both, and only which record types each carries differs.

The side is CATALOG DATA, never a list in Go: business-managed vocabularies come from Postgres, so
the farm can add a sixth sales category without a deploy. It is NOT a column on the vendor row —
a vendor's side is implied entirely by its record type, and storing it twice would let the two
disagree. The halves are COMPLEMENTARY BY CONSTRUCTION (sales = types marked `sales`, procurement =
everything else): every vendor is on exactly one side and NONE is on neither, so an UNCATALOGUED record
type — a real state, because `domain.Validate` deliberately does not check `record_type` against the
catalog — falls to PROCUREMENT rather than vanishing from both pages. An ABSENT side reads the whole
register (what the vendor picklist needs); an UNKNOWN side is REFUSED `vendor_side_unknown`, never
widened. Both page contracts name their side EXPLICITLY in the table `data_source`, and the renderer
reads it back out of its own contract rather than taking it as a prop, so there is one statement of
the fact. Only RECORD TYPES narrow — breed, state, city, status, feed, capacity unit and supply
frequency stay whole on both sides — and the narrowing happens SERVER-SIDE, so the Add-vendor form
cannot offer a category its page does not own.

On the PHONE, Sales is its OWN module (key `sales`, label "Sales"): the Sales ledger MOVED off
`/vendors/sales` to `/sales` and took a `/sales/vendors` tab with it, leaving Procurement (key
`vendors`) with TWO tabs, Vendors and Feed Purchases. The tab MOVED and is not duplicated — a tab in
both modules is two doors onto one ledger. The MODULE is offered on `sales.read` while its VENDORS
TAB is gated on `procurement.vendor.read`: the same three principals hold both today, but the
register keeps its own authority so a sales reader never reaches it through a second door. The web
leaf follows the same split — it TICKS with the `sales` module and is REACHED on `VendorRead`.
`sales` gained `SurfaceMobile`, so migration `000257` copies every existing web `sales` tick onto a
mobile row (the `000251` shape, with its own ledger table): a tick NARROWS an offer and never widens
one, so without it the module would be narrowed away on every existing phone. Canonical prose:
`docs/decisions/vendor-register-two-sides.md`. Pinned by
`TestTheTwoRegisterSidesPartitionTheWholeRegister`, `TestVendorSideIsRefusedRatherThanWidened`,
`TestSalesVendorsIsGatedOnTheRegistersOwnPermission`,
`TestSalesIsItsOwnPhoneModuleCarryingItsOwnVendorsTab` and
`TestSalesModuleAndItsVendorsTabAnswerToDifferentPermissions` (each mutation-tested when written).

UPDATED 2026-10-02 (People / HRMS fixes): the two halves are now GRANTED separately. The buyers
ride their own module `vendors_sales` and permissions `procurement.vendor.sales.read` / `.write`;
`procurement.vendor.read` / `.write` mean the suppliers. Sales > Vendors ticks with `vendors_sales`
and the phone's Sales > Vendors tab is gated on the buyers permission. The vendor handler enforces
the side on every read and write (a named side not held is 403 `vendor_side_forbidden`; no side is
the caller's own half unless they hold both; a vendor on the other half is not found). Canonical
prose: `docs/decisions/people-hrms-access-fixes.md`.

CEO/CXO visibility for Sales > Vendors is not an HRMS clean-up task. The leaf ticks with `sales`
but opens on `procurement.vendor.read`, so every future change to Sales/Vendors/page access must
prove `ceo_internal` holds both permissions AND that already-migrated CEO/CXO people receive the
stored `person_module_access` row/page through backfill or additive migration. A founder should
never lose a core commercial/register/config/oversight surface until Manohar manually re-ticks it
in HRMS; per-person ticks narrow ordinary users, not baseline CEO/CXO visibility.

Confirmed RANDOMIZED VERIFICATION SAMPLING rule (maintainer decision 2026-08-26): the CEO sets, per
verification category, the PERCENTAGE of that category's proof videos the verifier actually has to
watch. Her day is complete when she has cleared HER SHARE -- at 40% on feed packing, reviewing those
40% IS 100% of her work, and the progress number is backend-owned so no surface derives its own.

An UNSAMPLED video is AUTO-ACCEPTED, never left hanging, and that is the load-bearing half. Verifier
approval is not merely review for feed and weighing -- it is the gate that COMPLETES the work (a feed
pen-session stays pending_verification until an approve lands; a weighing bucket cannot close while
verification is pending, ledger D-5, unconditional). Hiding the unsampled ones would stall those
workflows forever, so the closeout stage approves them with
`verification_items.auto_resolution = 'not_sampled'` and NO verified_by, emitting the ordinary
`verification.verdict.approved` event -- every producer's consumer applies exactly as it does for a
human approve. There is no second apply path, and a waived item can never be counted as her work.
Sampling decides what gets WATCHED; a video nobody watched is never evidence the work was wrong, so
a waived item is always an approval and never a rejection.

Four narrowings, each load-bearing. (1) The draw is DETERMINISTIC AND MONOTONIC -- `sampling_bucket`
is a GENERATED column, in sample when `bucket < percent` -- so raising the share mid-day only ADDS
videos and can never retract one she is already holding. That is what makes "takes effect the same
day" safe. (2) The policy is EFFECTIVE-DATED: a change writes a row at TODAY's business date and a
past day keeps the percentage it actually ran at; the date is the SERVER's, never the client's.
(3) A category whose approve must CARRY a measurement (feed packing's packed quantities, feed
wastage's leftover weight) is LOCKED at 100% and the write is refused `sampling_not_available` --
there the verifier is the DATA SOURCE, not a spot check, and waiving would either record no quantity
at all or strand the item mid-apply. Derived from `MeasurementCorrection.RequiredForApprove`, never a
hardcoded list. (4) The CLOSEOUT settles only CLOSED business days, because a video waived the moment
it arrived could not be recruited back by a raise that afternoon.

THE SHARE IS A FLOOR, NOT A CEILING (maintainer decision 2026-08-27, raised in review). A verdict on
an item the policy did NOT draw is ACCEPTED and recorded as a HUMAN verdict (`verified_by` set,
`auto_resolution` NULL). There is deliberately no sampling gate on the verdict route, and adding one
would make bad work unreportable -- she watches an undrawn video, sees the work was wrong, and the
rejection is refused so the work proceeds to `completed` -- as well as discarding a review already
performed, since only a LOWERED share can drop an item she was holding. It mislabels nothing:
`not_sampled` is the contract for a video NOBODY reviewed, `Reviewed`/`Selected` are share-scoped so
an extra review cannot pass 100%, and the closeout skips any item a verifier already decided.
Sampling is NOT an authorization boundary; what takes an item out of her reach is leaving `pending`.
Reported as a P1 in review and closed as working-as-decided -- do not re-open it without reading
`context/repo-audits/verification-randomization-do-not-reopen-ledger.md` -> B-1, which carries the
reasoning, what a REAL defect here would look like, and the two stricter variants already costed.

`permissions.VerificationSampling` is CEO-ONLY and narrower than every other capability on /verify:
`pc_director` holds VerificationOversee and does NOT hold this. Oversight WATCHES the verification
workload; randomization DECIDES how much of it a human must watch, and a director setting that for
his own department's work is the separation of duty that keeps VerificationVerdict off leadership.
The VERIFIER's queue is narrowed by the policy; LEADERSHIP's is not -- the principal who sets the
percentage must be able to audit what it waived. Canonical prose:
`docs/decisions/verification-randomization-sampling.md`; schema: migration
`000214_verification_sampling.sql`; the cross-surface impact table (what sampling does to the KPI
strip, the vaccination live tracker, the People proof stats and the verifier push) is in that same
decision doc, and every row of it is asserted by `TestKernelStory_VerificationRandomization`. Two
rules fall out of it and bind future changes: a count of what a PERSON STILL OWES uses
`verification/samplingsql.InSample` (drawn items only), and a count of what a PERSON DID excludes
`auto_resolution IS NOT NULL` -- a settled item carries the closeout's `verified_at` and would
otherwise read as a verdict nobody cast, collapsing the reject rate with approvals no one decided.
Note also `backend/internal/verificationcatalog`: the category
set is now read by TWO processes (the API's registry and the worker's closeout), and a worker holding
a hand-copied subset would not fail loudly -- it would silently never settle the categories it was
missing. Declaring a category inline in `bootstrap/api.go` is blocked by
`TestBootstrapDeclaresNoCategoryOfItsOwn`.

Confirmed ONE-PARK-SCOPE-SOURCE rule (maintainer decision 2026-09-04): **the People screen
ticks (`person_access.scope_mode` + `person_park_scope`) are the ONLY authored answer to
"which park does this person work in"; `user_scope_grants` scope and
`workforce_members.primary_location_id` (the HOME park) are DERIVED from them, in the same
transaction, by `backend/internal/parkscope`.** Tenant mode → one tenant row per role; parks
mode → one row per role per ticked park; stale rows revoked. The editor carries a Home park
field, required when more than one park is ticked, and vaccination keeps assigning by home
park. Do not insert a `user_scope_grants` row with a scope of your own -- add the ROLE through
`WritePersonScope` / `SyncGrantScope` / `ReconcileUser` and let the person's scope decide
where it applies (`TestGrantScopeHasOneWriter` fails otherwise). Tenant-only roles (CEO,
verifier, directors, counts_approver, toxin_tester) are REFUSED parks mode, never narrowed;
a person with ONLY park roles is REFUSED tenant mode (the operator-scope invariant, enforced).
Drift check: `tools/dev/park-scope-drift.sql`. Why: three records drifted and
a Channapatna operator claimed Coimbatore milk work on 2026-09-01. Canonical prose:
`docs/decisions/per-person-page-access.md` -> "One source for which park".

Confirmed PAGE-GRAIN ACCESS rule (maintainer decision 2026-08-27, SUPERSEDING the MECHANISM
-- and only the mechanism -- of the 2026-08-21 procurement-director workspace decision, whose
OUTCOME is preserved byte for byte): **a person's admin-web sidebar is exactly the pages ticked
for them on /people.** One layer, editable by a human.

Until now TWO layers decided it and they disagreed. PERMISSIONS said what someone may do; a
LENS -- hand-written Go keyed on a ROLE -- then deleted nav leaves and page contracts regardless.
The Procurement Director HOLDS `feed_config.read/write`, `operators.*`, `roster.*` and
`verification.act` through the `feed_director` role he also wears, and saw none of it, because
`procurement_director_lens.go` kept only the Procurement and Feed groups and hid `/feed/config`.
Both layers were right about their own question; together they meant the People access editor
(which reads permissions) advertised modules he could not reach, and every future "this person
should not see that page" was a commit.

`adminui/app/procurement_director_lens.go` is DELETED. Its narrowing was written onto that
person's OWN rows by the backfill, once, as data (`NarrowForRetiredLenses`).

**THE VERIFIER LENS STAYS** (maintainer instruction, same day) and is not the same kind of
thing: it does not subtract from the ordinary console, it composes a DIFFERENT workspace -- a
queue, its own registry-built modules, its own landing. Retiring it would delete a product
surface rather than a narrowing. It is still checked FIRST, so a verifier is never page-narrowed.

**THE PHONE DOES NOT CHANGE.** Android composes its bar from the mobile module registry; a page
tick is web-only and never reaches it. Operator access is untouched.

Four properties, each load-bearing:

1. **AN EMPTY PAGE LIST MEANS EVERY PAGE OF THAT MODULE.** This is what makes a page shipped
   tomorrow reach whoever already holds the module, instead of silently reaching nobody until
   someone re-ticks thirty people. Narrowing is opt-in: you have to say "not that one".
2. **THE CATALOG IS ASSERTED AGAINST THE REAL NAVIGATION.** `permissions.ModulePages` carries
   every nav leaf; `TestEveryNavLeafIsATickablePage` fails if a leaf ships without a row (it
   would be unwithholdable) and `TestEveryPageContractRouteIsOwnedByAModule` fails if a page
   contract's route belongs to no module (it could never be narrowed). Adding a screen without
   a catalog row is a build failure, not a silent hole.
3. **FAIL OPEN ON ABSENCE AND ON ERROR.** A person with no stored rows is NOT narrowed -- they
   are still on the retired role path, and narrowing them to nothing would lock out anyone the
   backfill has not reached. A source error is logged and the full contract served. The sidebar
   is a convenience; every route behind it is independently permission-gated, and the 403 is the
   lockout.
4. **TWO REFUSALS ON THE WRITE PATH.** A page key from another module is REJECTED (a dropped
   tick reads as granted while granting nothing). A granted module with screens and NONE ticked
   is REJECTED -- it would resolve to every page by property 1, the opposite of what the admin
   just did on screen.

**A SCREEN IS OFFERED ONLY WHEN IT CAN BE OPENED**, and this is the fifth property rather than
a detail. Every page declares the permissions its own screen needs, and a screen the person
cannot open is never ticked -- so it can never render greyed. The catalog and the navigation
gate (`adminui/app.permissionsForNav`) are asserted IDENTICAL by
`TestPageCatalogPermissionsMatchTheNavigationGate`; they are two layers and drift between them
is what produced dead rows. Openability is computed from the person's WHOLE permission set,
not the owning module: Feed SOP is grouped under Feed and needs `sop.read` from Protocols &
SOPs, and checking only the owner hid it from the CEO. A module is where a screen is TICKED,
never where its authority comes from. Note also that permissions union across SURFACES, so
removing a module from web removes the SCREENS while the phone's own grant still carries the
ability -- consistent, because a route does not know which surface called it.

The live sweep that proves all of this is `tools/dev/audit-person-access.py` (930 checks over
all 31 people, both surfaces). Its PASS 3 opens the DATA ROUTE behind every visible leaf and
fails on a 403; that is what found NINE dead leaves for four real people, every one of them a
pre-existing leaf with no navigation gate at all, plus Counts Breakdown gated on `goat.read`
while `/counts/breakdown` checks `counts.read`. Run it after any change to the access model --
it needs the local stack, so it is deliberately not in CI.

Resolution is `permissions.PageAccessForAssignments`, read by BOTH the bootstrap narrowing and
the access editor, so the ticks the screen shows are the ticks the sidebar obeys. Schema:
migration `000220_person_page_access.sql`. Canonical prose:
`docs/decisions/per-person-page-access.md`. Pinned by
`TestRetiredProcurementDirectorLensIsReproducedByTicks` (the holder's two stacked roles produce
exactly the six leaves his live bootstrap served on 2026-08-27, and no page contract for
`/feed/config`, `/people`, `/verify` or `/`) and `TestCeoIsNeverNarrowed`.

Confirmed verifier admin-web workspace rule (maintainer decision 2026-08-03): the
verifier-only workspace, previously mobile-only, also runs on admin-web with the SAME
five evidence modules as mobile — Vaccination, Weighing, Counts, Feed, Health. `verifier`
now holds `admin_web.bootstrap`, but that opens the SHELL ONLY. A principal holding
`verification.review` and NOT `verification.act` receives the verifier LENS: the sidebar
is composed from the Verification type registry's navigation metadata (one group per
`NavigationModule`, one leaf per `PageKey`, each pointing at
`/actions?category=<disjoint category>`), and EVERY other admin-web page contract is
dropped so a typed URL fails closed at `requireAdminWebPageContract`. Never express this
as a per-role nav template — registering a producer category is the only way to add a
module, and `make nav-composition-guard` still applies. CEO/CxO holds review AND act as
the documented override and therefore keeps the full admin IA; the lens must never narrow
a leadership principal. `/actions` serves both personas, split by the page contract's
controls: `record_verdict` (`verification.review`) vs `request_rework`/`reassign_task`
(`verification.act`). A page rendering from LOCAL literal copy has no contract to
withhold and must gate itself with `adminWebRouteOffered` (`/approvals` does). Known
boundary: the rework/assign routes require `task.verify`/`task.assign` and a verifier
holds `task.verify`, so that half of the split is contract-layer, not a backend lockout on
that shared SOP route. Canonical source: `context/architecture/verifier-app-and-flow.md`
→ "Verifier WEB workspace"; code `backend/internal/adminui/app/verifier_lens.go`.

Confirmed AFTERNOON FEED CORRECTION rule (maintainer decision 2026-08-10,
SUPERSEDING the APPROVAL half — and only that half — of the 2026-07-27 projection
rule immediately below): **a RAISED shifting counts toward the feed sheet before
a park head approves it, and the 14:00 correction reopens any pen already packed
against the old count.**

The defect it fixes: a low-priority movement raised at 09:00 is due TOMORROW, but
tomorrow's normal sheet was issued at **07:00 that same morning** and is already
being packed. Ten animals arriving in a pen fed for one had no feed at all,
because the projection waited for authorization. Under-feeding animals that
really arrive is worse than over-packing for a movement the park head later turns
down.

Three parts, and each narrowing is load-bearing:

1. **Approval no longer starts the feed clock; only REJECTION stops it.** The
   projection now counts `authorization_state='pending' AND event_status='pending'`
   alongside the existing authorized set. The two branches are disjoint on
   `authorization_state`, so one movement contributes exactly once as it travels
   from raised to approved. `rejected`, `canceled` and `applied` are excluded by
   construction — a movement that is turned down stops feeding a shed at once.
   The effective date for a RAISED movement is the **ACTIONS lead time**
   (`ShiftingActionsDueFrom`: low priority raised before 13:30 IST → tomorrow, at
   or after 13:30 → the day after), **not** the raise day. This is the one place
   the two rules deliberately meet: an unapproved movement has no authorization
   instant, and the honest answer to "when do these animals eat here" is the day
   they are expected to walk. Anchoring on the raise day would feed a destination
   a full day before a 13:45 raise's animals move.
   Canonical rule: `counts/domain.FeedShiftingRaisedEffectiveBusinessDate`.
2. **The 14:00 correction REOPENS an already-packed pen — EVERY SESSION of it.**
   The correction (`correction_time`, already 14:00 for both workflows — this rule
   adds no new clock) recomputes the frozen sheet, and any pen whose packing was
   already submitted goes back to `rework` with an operator-facing sentence, its
   still-pending verification item `withdrawn`, and `verified_by`/`verified_at`
   cleared. An **already-APPROVED** video is reopened too: it proves the packer
   packed the OLD quantity, which is now the wrong quantity, so an approved clip
   is no more usable than an unapproved one. A verdict already CAST is kept as
   history rather than rewritten — only a still-`pending` item is `withdrawn`,
   because that is the one sitting in a verifier's queue pointing at a stale clip.
   **BOTH of a pen's bags come back** (2026-08-11, once packing returned to the
   shed-SESSION grain): head count scales the morning and the evening ration alike,
   so a partial reopen would leave one bag packed for a head count the farm no
   longer has. `ReopenPackingForFeedChange` names pens WITHOUT a session and applies
   no session predicate.
3. **Two narrowings that must not be widened.** *Experiment is EXEMPT* — and as of
   the 2026-09-01 per-animal decision below, that is a KEPT TRADE rather than an
   arithmetic fact. Its rations used to be absolute kg per pen, so a head-count
   change moved no quantity there and reopening one would have discarded a good
   video for a sheet that did not change. Experiment cells are now authored as
   grams per animal and DO move with the head count; the maintainer was shown that
   consequence and kept the exemption, accepting that an experiment pen whose count
   moves between packing and the correction keeps a video proving the
   pre-correction quantity. *HEAD COUNT ONLY, PER PEN* — `AffectedShedIDs` also fires for a
   relabelled ration group and is shed-wide, so driving the reopen from it would
   make the packers of Castro 1 and Castro 3 refilm because Castro 2 gained
   animals. Making an operator refilm is expensive; it is spent only where the
   number of mouths actually moved. Canonical rule:
   `feeddirection/domain.CellDiff.HeadCountChangedPens` →
   `app.reopenPackingForCorrection` → `ports.ReopenPackingForFeedChange`.

There is **no new state**: a reopened pen uses the existing `rework`, which
normalizes to the client bucket `pending` ("needs my action again"). That means
the CHIP CANNOT distinguish a reopened pen from one nobody has packed — the
backend-composed `rework_reason` on `FeedPackingRow` is the only thing that can,
so it must never be dropped from the contract or replaced by client-side copy.

**No lock is lifted and none may be.** The transport lock is 15:30, after the
14:00 correction, so the correction was never blocked by it; `ErrAmendAfterLock`
stays. Do not add a path that amends a locked sheet — past the transport cutoff
the feed has physically left and a correction cannot reach the shed.

Pinned by `TestFeedShiftingRaisedEffectiveBusinessDate`,
`TestRaisedAndAuthorizedRulesStayDistinct`, `TestDiffCellsReportsOnlyTheChangedPenOfASharedShed`,
`TestAfternoonCorrectionNeverReopensExperimentPacking` and
`TestPackingReworkReasonIsCarriedOnlyWhileThePenIsActuallyInRework`. Every one of
those was mutation-tested when written: deleting the experiment branch, keying the
reopen on the shed, or widening it past head count each turns one red.

Confirmed feed-direction shifting-projection timing rule (maintainer decision
2026-07-27; its APPROVAL requirement is SUPERSEDED by the 2026-08-10 afternoon
correction rule ABOVE — a raised movement now counts before approval — while
everything below about AUTHORIZED movements, zero lead, no priority branch and
the applied/pending_verification boundary stands unchanged. This rule itself
SUPERSEDED the priority-based lead-day rule — normal 2-day /
high-priority 1-day — that the projection previously applied): the feed sheet's
projected shed head count = the live herd PLUS every authorized-but-unexecuted
shifting, with NO lead time and NO priority branch. A shifting is a pending feed
input the moment a park head AUTHORIZES it, so it counts toward the next feed
sheet immediately (destination shed +heads, source shed −heads), affecting ONLY
the feed projection and never the census counts. "Forget high priority": normal
and high-priority movements are treated identically for feed timing (high still
completes same-day operationally, so it lands in the live herd quickly anyway).
A movement stops counting in the projection ONLY when it is `applied`
(verifier-approved), at which point its animals already sit in the destination
shed in canonical `goats` — so the delta must count BOTH `authorized` AND
`pending_verification` (operator completed with proof, not yet approved, animals
NOT yet relocated) and EXCLUDE `applied`, or the shed is either double-fed
(counting applied) or under-fed (dropping pending_verification). The "overdue"
flag fires only when a counted movement was authorized BEFORE the packing day
(feed day − 1) and is still unexecuted, so a freshly authorized move under the
zero lead does not spuriously read as overdue. Canonical source: the pure-Go
spec `backend/internal/counts/domain.FeedShiftingEffectiveBusinessDate` /
`FeedShiftingCountsToward` / `FeedShiftingIsOverdue` and the SQL it mirrors in
`counts/adapters/postgres/feed_projected_counts.go` (`event_status IN
('authorized','pending_verification')`). Proof:
`counts/domain.TestFeedShifting*` and
`counts/adapters/postgres.TestFeedProjectionTimingRule` /
`TestFeedProjectionExcludesAppliedMovements` (includes the pending_verification
case). This changes ONLY the shifting-aware feed projection; the 7:30-style
auto-issue scheduler and the calendar surfacing of next-day feed remain
separate, unbuilt items.

Confirmed Feed Direction stage-clock rule (maintainer decision 2026-08-10):
the source default is Day N 09:00 full direction for Day N+1, Day N 13:30
shifting cutoff, Day N 13:30-13:45 Diff, Day N 15:00 packed/diff-corrected feed
staged outside sheds, then Day N+1 09:00 and 15:00 serving slots. Packing,
transport, and distribution are time-bounded work, not timeless claim pools.
The source requires packing/loading/transport staging outside sheds to be
complete by Day N 15:00. The current Transport materializer's 15:30 creation is
a source/runtime defect, not an accepted extension: create and assign the task
early enough to meet the 15:00 hard deadline; route/park policy may be stricter.
Missing owner, stock/config, route, vehicle, system, or proof readiness is
attributed before any person-level candidate. Follow
`docs/decisions/task-timing-alerting-violations-and-appeals.md`; a verifier delay
is never charged to the operator.
