# Milk Preparation — Counts Worklist Contract

Status: active, farm-day preparation plus four separate farm-session feeding Actions supersede the retired shed-grain implementation and legacy Slack/Sheets workflow, maintainer-approved 2026-07-29.

## Scope

`Counts -> Milk Preparation` uses the same Android information architecture as Birth/Death: a
current-day L0 worklist with status filters and one card per actionable farm-day, followed by a
separate L1 execution screen for the selected farm. Shed is never shown or selected. It is not a direct-entry dashboard and it does
not reuse Feed Packing's screen anatomy. Admin web remains the monitoring/read surface; Android
remains the operator execution and proof surface.

The canonical write owner for animal membership remains Herd Identity (`goats`). Counts owns the
farm-day preparation completion and immutable proof attempts. Android owns offline-first operator
capture; admin-web remains a read-only monitor of direction and verification state.

## Verification gate and evidence grain

One preparation is `(tenant_id, park_id, preparation_date)` and feeds the following India business
date. Operator submit creates `pending_verification`; it never means completed.

Every applicable step has its own distinct completed live in-app-camera video, bound to the exact
farm and step code:

- With goat milk: goat-milk quantity, boiling temperature, cooled temperature, UHT-milk quantity,
  and citric-acid mixing — five videos.
- Without goat milk: UHT-milk quantity and citric-acid mixing — two videos.

One proof reference cannot satisfy two steps. All two/five videos travel in canonical step order on
one generic Verification item (`module=milk_preparation`,
`ref_type=milk_preparation_completion`). One verifier APPROVE completes the whole farm-day
preparation. REWORK returns it to the same operator flow for a fresh complete set; prior proof
	attempts are immutable history. Exact command replay repeats only the idempotent verification
enqueue, allowing recovery from a queue-write failure without duplicating the proof attempt.

The same submission carries the operator answers as structured fields, not text embedded in proof
metadata: morning milk collected (litres), evening milk collected (litres), whether goat milk was
used, goat-milk quantity/boiling/cooled temperature when applicable, and UHT-milk quantity.
Citric-acid grams remain in the answer snapshot, but they are backend-calculated from the task's
total milk and are never entered by the operator. The citric-acid action is video-only.
The execution screen displays the full Milk Direction arithmetic by stage (`kids × ml × sessions`),
the total litres, and the calculated `total litres × 5.5 g` citric instruction before those questions.
The operator form begins with no implicit goat-milk answer. Questions unlock strictly in order:
morning milk, evening milk, goat-milk Yes/No, then each applicable preparation action. Within the
preparation actions, each manual numeric answer and its fresh video must both be complete before the
next action unlocks. For the calculated citric-acid action, its fresh video alone completes the step.
The selected Yes/No answer persists in the draft and locks after the first video.

## Grain proof

- Producer uniqueness: `goats(tenant_id, goat_id)`.
- Consumer row grain: `(tenant_id, park_id, shed_id, management_stage)` for stages K1/K2/K3.
- Android L0 work-item grain: `(tenant_id, park_id, preparation_date)`; `farm_tasks` is a bounded
  whole-scope list independent of the paged cohort rows.
- Membership: live, non-merged canonical goats only.
- Location joins: `locations(tenant_id, location_id)`, one-to-at-most-one, labels only.
- Whole-scope summary and page read the identical grouped membership set. Summary is invariant to
  `limit` and `offset`; `farm_tasks` is also invariant to cohort-row pagination, and the frontend
  never recomputes either from visible rows.
- Migration `000058` retires the superseded shed-grain rows in place and restores farm grain. Retired rows and proof attempts
  remain immutable history but are excluded from current work and verdict transitions.

## Source-backed volume matrix

| Cohort | Per active session | Active sessions | Daily per head |
| --- | ---: | --- | ---: |
| K1 | 200 ml | 1, 2, 3, 4 | 800 ml |
| K2 | 300 ml | 1, 2, 3, 4 | 1,200 ml |
| K3 | 200 ml | 1, 4 | 400 ml |

Citric acid is 5.5 grams per litre of prepared milk. Exact quantities remain integer millilitres;
citric acid rounds to one decimal only at the response/display boundary.

Milk Feeding has exactly four India-business-day sessions: session 1 at 08:00, session 2 at 12:00,
session 3 at 16:00, and session 4 at 21:00. Maintainer confirmation on 2026-07-30 supersedes the
earlier due-time-only wording: these are hard IST unlock times. A task remains visible but disabled
before its time, becomes actionable at the exact time, and remains actionable afterward. The backend
owns `available`, `available_at`, and `blocked_reason` and rejects an early submit even if a client is
stale or bypassed. These are not feed-direction packing/transport/distribution times.

K0 colostrum and ICU/clinical feeding do not have an approved per-head preparation quantity in the
supplied source. They are excluded, visibly and fail-closed, rather than represented as zero.

## Date and history

The endpoint accepts no historical date. `preparation_date` is the current India business date and
`feeding_date` is the following India business date. Historical review requires durable issued-run
rows; today's goat locations must never be rendered as a past preparation direction.

## Milk Feeding — Slack/Sheets retirement contract

Milk Feeding is a separate task grain from Milk Preparation. One task is
`(tenant_id, park_id, feeding_date, session_no)`. It is a separate L0 Action from Milk Preparation;
Milk Preparation never embeds feeding sessions. The eligible farm population is the live canonical
herd in K1, K2, K3, ICU-kid, or Quarantine Milk Kid stages. This includes ICU-kids even when Milk Preparation excludes them for lack of an approved per-head
quantity formula.

Its L0 worklist uses the same date bar, sync state, status filters, and farm-card anatomy as Milk
Preparation. The four session tasks remain separate rows and never appear inside Preparation.

Each session records the full conditional cascade:

- one Yes/No answer for every kid already on the farm watchlist;
- total kids fed and count not drinking cow milk after attempt 1;
- when non-zero, count not drinking after attempt 2;
- stable Goat OS goat IDs and optional remarks for new refusals after attempt 2;
- when still non-zero, count not drinking udder milk and then count not drinking ORS.

Counts are monotonic through the cascade: attempt 2 cannot exceed attempt 1, udder cannot exceed
attempt 2, and ORS cannot exceed udder. New refusal IDs equal attempt-2 refusals not already
represented by refusing watchlist kids. A watchlist kid graduates only after two consecutive
sessions answered Yes; a No resets the streak. New refusal IDs enter the durable farm watchlist.
Watchlist changes apply only after verifier approval, so rejected evidence cannot alter later forms.

The operator records two distinct mandatory fresh in-app-camera videos per session: clean bottles,
then mixing milk and filling the required bottles. Submit creates `pending_verification`; one generic
Verification item carries both videos and the answer snapshot. APPROVE completes the session and
atomically advances the watchlist; REWORK returns the same task to the operator for a fresh answer
and proof attempt while keeping prior attempts immutable.

Producer uniqueness is `milk_feeding_tasks(tenant_id, park_id, feeding_date, session_no)` for active rows. Answer
and proof attempts are unique by `(tenant_id, completion_id, attempt_no)`. The task list and status
summary use this same stable key and never infer completion from an embedded preparation report.
Android reads a bounded Room-backed task window and submits an offline idempotent command. Slack,
Redis, and Sheets are migration references only and cease to own current Milk Preparation/Feeding
answers, proof state, watchlists, or completion state after cutover.

RT-012 clinical escalation remains a separate Health-owned handoff. Milk Feeding preserves the
refusal facts and durable watchlist needed by that future consumer but does not invent clinical
diagnosis or treatment state.
