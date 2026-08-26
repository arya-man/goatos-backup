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
migration `000207`). A bare `park_head` / `pc_director` / `growth_director` job inherits
nothing. Pinned by `TestToxinModuleIsOfferedPerPersonNotPerJob` (mutation-tested:
deleting the offer branch turns it red).

`toxin.execute` is ORed into the `/app/proofs/*` upload routes (the weighing
phone-QA-2026-08-03 lever), or every step video would be permanently unsubmittable for a
tester who is not a general operator. Pinned by `TestToxinExecuteReachesProofUploads`.

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
with no branch, so every feed purchase 500'd on commit. Migration `000207` redefines the
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
