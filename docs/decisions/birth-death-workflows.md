# Birth & Death Follow-up Workflows (revised maintainer decision 2026-07-28)

Status: accepted and implemented · Owner: counts + tasks · Mock: `mock/birth-death-mobile-mock.html`
Roadmap source: `docs/mobile/mobile-feature-action-flows-roadmap-2026-07-30.md`

## Decision summary

Birth and Death split from the single write-only form at `/counts/birth-death`
into **two modules with their own routes** (`/counts/birth`, `/counts/death`).
Each module opens on the **outstanding SOP work per goat**. Birth submission
creates every child and opens its work immediately; web approval controls only
whether those children enter herd counts. Death work is different: a
valid death report opens the staged workflow immediately while the goat remains
alive, so the operator can record both required videos before an admin decision.
Recording a new event moves behind the ＋ button, which opens the reworked form.
The per-goat action lists are backed by the workflow engine in
`backend/internal/tasks`, instantiated from code-defined templates (Delivery
Template mother/kid tracks, Death Template).

Maintainer decisions captured 2026-07-27 (Q&A):

1. **Full slice** — backend engine + APIs + mobile screens in this change.
2. **Death upload-before-approval override (maintainer decision 2026-07-28).**
   This supersedes only the 2026-07-27 rule that opened death work from
   `goat.exited`. Submitting a valid death report durably opens the death
   workflow while the goat is still alive; the operator records the death video
   and post-mortem video; an authorized existing admin-web approver (including
   `ceo_internal` or the applicable manager under the existing grants) accepts
   or rejects the report. Acceptance applies the guarded death exit/count
   change and releases the pair to Verify. Rejection leaves the goat alive and
   cancels the staged workflow. Birth opens from the immediate `goat.created`
   events; its later approval only activates herd-count eligibility.
   The admin-web approval page keeps its current information + accept/reject
   behavior. It is not the media-verification screen, and no admin-web UI or
   approval route shape or role grant is redesigned by this decision; only the
   death decision semantics are specialized as specified below.
3. **Birth identity flow** — the operator does NOT scan a child RFID at birth.
   The server generates one farm-coded provisional ID per kid (`CBE-12345` or
   `CPT-12345`); each kid lives under that ID until **Tag the kid**, where
   the operator assigns the permanent RFID via the existing
   promote-identifier flow. From then on the permanent ID is used.
4. **Canonical mother + litter contract (maintainer decision 2026-07-28).**
   Breed is visible and required. The operator must scan/type the mother's
   permanent RFID; identity resolves it tenant-safely to a canonical female goat
   before any child is created. The operator selects `1`, `Twins`, or `Triplets`.
   One submission atomically creates 1, 2, or 3 distinct canonical children,
   each with its own provisional ID and child workflow. The same litter event,
   litter size, and mother UUID are stored on every sibling's `goat_births` row.
   The scanned RFID is not copied as relationship truth.
5. **＋ menu is New kid only.** Abortion (RT-002 mother-only track) is out of
   scope until a later slice.
6. **Sequential operator lanes + live-camera evidence (maintainer decision
   2026-07-28).** Within an operator section, only the first incomplete action is
   enabled; completing it enables the next action. A later command is rejected
   server-side with `409 action_out_of_sequence`. Birth's fixed-time colostrum
   tasks render in the same operator task list. The first scheduled round waits
   for **1st Colostrum**; the rounds then unlock one by one at their scheduled
   times and remain available until completed. **Tag the kid** waits for every
   earlier main and colostrum task and is always the final kid task. Birth and
   Death video actions are
   live in-app camera only: there is no gallery/import option. For Death, each
   capture remains a durable local editable draft; re-recording replaces that
   action's draft, and no upload/action completion is queued until the operator
   taps **Submit** after both drafts exist. **Vaccination is explicitly exempt
   and keeps its existing gallery picker.**
7. **Death is exactly two operator steps.** The operator sees only Record death
   video → Record post-mortem video. Admin approval and media verification remain
   backend/admin/verifier state and are never rendered as a third operator step.

## Locked birth state machine

| Stage | Canonical children / herd count | Operator workflow | Verify queue |
| --- | --- | --- | --- |
| Birth submitted | 1–3 canonical goats are created immediately with distinct `CBE-#####`/`CPT-#####` provisional IDs; all are excluded from herd counts | One kid workflow per child plus the shared mother track opens from `goat.created` | Nothing enqueued |
| Web approval accepts | Existing children become herd-count eligible atomically at the litter grain; no goat is created here | Work continues unchanged | Nothing enqueued |
| Web approval rejects | Existing children remain canonical but count-ineligible for audit/reconciliation | Work remains available; rejection is not media review | Nothing enqueued |
| Operator completes every task for one subject workflow (the mother or one child), including that child's colostrum sessions and manual RFID tagging | Each child keeps the same goat UUID; its permanent RFID becomes active and the provisional identifier is retired | Only that completed mother/child workflow enters `awaiting_verification`; siblings continue independently | ONE `birth_evidence` item carries that mother or child's proof bundle |
| Verifier accepts one item | No count or identity change | Only that mother/child review gate closes | That subject's item is accepted; siblings are unchanged |
| Verifier rejects one item | No count or identity change; permanent RFIDs are never undone | Only that subject's video tasks return to `rework`; Tag the kid keeps the assigned RFID value but requires a new proof | One fresh subject-level item after that workflow's rework proofs are complete |

Count approval and birth evidence verification are independent. Approval never
creates a child, and verifier verdicts never add or remove a child from counts.

## Locked death state machine

| Stage | Goat lifecycle / herd count | Workflow actions | Verify queue |
| --- | --- | --- | --- |
| Death report submitted | Goat remains `alive`; count does not change | Death workflow opens with exactly two actions; first recording is enabled and second is blocked | Nothing enqueued |
| Operator records death video | Goat remains `alive`; count does not change | First local draft is editable/re-recordable; post-mortem recording becomes enabled | Nothing enqueued |
| Operator records post-mortem video | Goat remains `alive`; count does not change | Both local drafts remain editable/re-recordable; Submit becomes enabled | Nothing enqueued |
| Operator taps Submit | Goat remains `alive`; count does not change | Both proof uploads and ordered action completions are durably queued; the two actions lock as submitted | Nothing enqueued until admin acceptance |
| Admin tries to accept before both videos exist | Approval stays `pending`; goat remains `alive`; count does not change | Existing staged proofs remain available | Request fails `409 death_evidence_incomplete` |
| Admin rejects | Goat remains `alive`; count does not change | Workflow and actions become `canceled`; staged proof refs are cleared | Nothing enqueued |
| Admin accepts after both videos exist | Goat becomes `dead`; this is the one herd-count transition | Both recordings stay complete; `workflow_instances.awaiting_verification` opens atomically with approval + death | The resulting durable `goat.exited` event releases one item carrying both videos |
| Verifier accepts | Goat stays `dead`; no second count change | The workflow verification gate closes; both actions remain complete | Item is accepted |
| Verifier rejects | Goat stays `dead`; the accepted death is never reversed | Both recording actions become `rework`, proof refs are cleared, first recording is enabled, second is blocked, and the current verification gate closes | Re-shoot required |
| Operator records both re-shots sequentially | Goat stays `dead`; no second admin decision or count change | Both recordings complete and the workflow verification gate reopens | A fresh item is enqueued directly to Verify |

Approval and verification are deliberately separate decisions. Admin approval
decides whether the reported death applies to canonical lifecycle/count truth.
Verification decides whether the two operator videos are acceptable evidence.
A verifier rejection can demand new evidence but cannot resurrect the goat or
reopen the admin approval.

## Engine shape (slice scope)

New tables come from migration `000034_birth_death_workflows.sql`. Migration
`000035_death_upload_before_approval.sql` adds the durable death-report/rejection
handoff and backfills still-pending death approvals so older reports also receive
their upload workflow. Migration `000036_death_two_operator_actions.sql` removes
the legacy `park_head_signoff` action and recomputes existing death cards at the
two-action grain.
Migration `000037_birth_mother_and_litter.sql` adds `goat_births`, one row per
child with `mother_goat_id` and `litter_size` (`1|2|3`). Existing legacy births
remain unknown rather than receiving fabricated backfill values.
Migration `000039_birth_litter_immediate_identity.sql` adds the litter event,
child ordinal, and count-decision state. Existing birth rows backfill as
approved; new birth rows start pending and are excluded from the herd-register
projection until the litter approval is accepted.
Migration `000043_birth_workflow_litter_grain.sql` keys mother/kid workflows by
`birth_event_id`, allowing the same dam to receive a new mother track on every
delivery without weakening event-redelivery idempotency.

- `workflow_instances` — one row per birth delivery or death follow-up. Birth's
  natural key is `(tenant_id, template_key, subject_goat_id, birth_event_id)` so
  the same mother receives fresh work for every litter; Death remains unique on
  `(tenant_id, template_key, subject_goat_id)`. Columns: `template_key`
  (`birth_kid | birth_mother | death`), `subject_goat_id`, `dam_goat_id`,
  `event_at timestamptz` (birth/death moment), `event_date date`
  (Asia/Kolkata business date — the list's date filter key), `park_id`,
  `shed_id`, `state` (`open | completed | canceled`), and **write-maintained
  card fields**: `actions_total`, `actions_done`, `next_action_key`,
  `next_action_title`, `next_due_at`, `awaiting_verification`. These are
  updated in the same transaction as every action write so the list and chip
  counts read `workflow_instances` alone (compute-on-write; no join to
  `workflow_actions` on the hot list read).
- `workflow_actions` — the per-goat steps. Columns: `workflow_id`, `action_key`,
  `seq`, `section` (`main | colostrum_session`), `action_type`
  (`question | question_select | action | approval`), `title`, `detail`,
  `requires_video`, `options jsonb` (question_select bands), `due_at`,
  `status` (`pending | in_review | completed | rework | canceled`),
  `answer_value`, `proof_ref`, `completed_by`, `completed_at`,
  `verification_item_id`. Natural key `(workflow_id, action_key)`.
- `goat_births` — identity-owned birth metadata at child grain.
  Unique `(tenant_id, child_goat_id)`; both child and mother use same-tenant
  composite foreign keys to `goats`. Unique `(tenant_id, birth_event_id,
  child_ordinal)` proves litter membership. `count_status` is the only mutable
  birth decision (`pending | approved | rejected`).

projection-review: producer grain = `workflow_actions` unique
`(workflow_id, action_key)`; consumer card grain = `workflow_instances` unique
`(tenant_id, template_key, subject_goat_id)` = 1 row per card. `actions_total` /
`actions_done` / `next_*` are counters over exactly that workflow's visible operator actions,
including scheduled colostrum (join key
`workflow_id`, 1:N pre-aggregated on write in the same txn). Chip counts group
`workflow_instances` rows by derived bucket at `(tenant_id, module,
event_date)` grain — numerator and denominator both range over the same
`workflow_instances` key set; no join fan-out. The operator counters and
`next_*` range over the identical subset (all sections, non-approval actions).
Approval/media review uses `workflow_instances.awaiting_verification`; it never
changes the action denominator or adds another action row.
The operator chip buckets are mutually exclusive: a submitted workflow with an
open verifier gate contributes only to `Awaiting video`, even if its persisted
workflow state is `completed`. It moves to `Completed` only after verifier
approval closes `awaiting_verification`, so one death cannot inflate both totals.

Templates are **code-defined** in `backend/internal/tasks/domain`
(`TemplateBirthKidAt`, `TemplateBirthMother`, `TemplateDeath`), versions of the
legacy sheet blocks with the shifting `TRIGGER_EVENT` steps dropped (shifting
is its own gated module):

- `birth_kid` (8 main steps): kid clean? · iodine dipping of umbilical cord ·
  front teeth outside lower gum? · suck reflex? · 1st Colostrum · Take Weight
  of Kid (required positive numeric kilograms input) · kid standing? (EVENT+1H) · **Tag the kid**
  (EVENT+2D 07:00 IST). The scheduled colostrum series is derived from the
  recorded birth moment in IST. On the birth date, include only the 07:00 ·
  11:00 · 15:00 · 18:30 · 22:00 slots whose 15-minute pre-notification window
  has not started; a birth exactly at the cutoff misses that slot. On the next
  business date, include all five slots. Therefore each child receives 5–10
  `colostrum_session` rows (6–11 colostrum feeds including 1st Colostrum). Every
  scheduled row stores the exact IST `due_at` derived from the birth timestamp,
  displays that access date and time, rejects completion before it with
  `409 action_not_yet_due`, and stays enabled after it until completed. These
  rows render as normal actionable tasks in the same Overdue / Scheduled /
  Completed list, not in a separate sessions strip, and the detail remains
  bounded at 13–18 rows. Every task requires one video. **Tag the kid** is
  appended after this series and cannot start until every other kid task is done.
  Tagging scans/types the permanent RFID through promote-identifier, then
  completes only after its own video; promotion alone never completes it.
- `birth_mother` (6 steps): babies still inside? · is the mother licking her
  babies? · Mother's Medicine (ONE action listing Chocolate Injection 1.5 ml
  SQ; Meloxicam Paracetamol 4 ml IM; Exapar 20 ml; and Glucoboost 100 ml mixed
  with 150 g concentrate) ·
  ORS water · is the mother eating? · ORS water (2nd round), due exactly 50
  minutes after the first ORS action was actually completed (not from birth).
  The first round's canonical `completed_at` is persisted and sets the second
  round's `due_at`; the API renders round two blocked and rejects completion
  with `409 action_not_yet_due` until that exact timestamp (49:59 fails, 50:00
  succeeds).
  Opened once per dam (shared by twins via the natural key + ON CONFLICT). Every
  one of these six mother steps requires exactly one video; the medicine video
  covers the whole four-medicine task, not one upload per medicine. A question
  stores its answer and proof together. The mother card/header renders the
  mother's active permanent RFID ahead of her internal display ID; a kid's
  provisional identifier must never label the mother workflow.
- `death` (exactly 2 persisted operator video steps): record death video · record post-
  mortem video (both live-camera `requires_video`; completion without `proof_ref` is 422
  `proof_required`). Admin approval and verifier review are represented only by
  backend approval/verification records plus `workflow_instances.awaiting_verification`.

Scheduling stays **business-day/IST-anchored** (`biztime`): offsets are applied
to the event moment, and `event_date`/session times use `Asia/Kolkata`. No
hour-grain vaccination semantics are touched.

## Event wiring (both ends registered)

Consumers (new, in `backend/internal/tasks/app`, registered at all bus wiring
sites alongside the existing appliers):

- `goat.created` (topic `identity.events`), filtered `origin_type == "birth"` →
  open `birth_kid` workflow for the goat linked to the payload's canonical
  `dam_id`, then open (or attach to) the `birth_mother` workflow for the dam.
  Legacy events with no resolvable dam still open only the kid track. Reads the
  canonical goat row for dob/time-of-birth/park/shed and uses the payload's
  already-canonical `dam_id` (the event payload's new `time_of_birth` key is
  parsed by this consumer).
- `counts.death.reported` (topic `counts.events`) → open the staged `death`
  workflow immediately from the still-live goat's canonical park/shed facts.
  It is written atomically with creation of the pending death approval request.
- `counts.death.rejected` (topic `counts.events`) → cancel the staged death
  workflow; it is written atomically with the rejection, whose transaction
  applies no identity/count effect.
- `goat.exited` (topic `identity.events`), filtered `exit_reason == "died"` →
  load the approval-released proof pair and idempotently enqueue Verify.
- `goat.identifier.added` (topic `identity.events`) → if the goat has an open
  `birth_kid` workflow with a pending `tag_the_kid` action and the added
  identifier is permanent, record the assigned RFID prerequisite. The action
  remains pending until its mandatory tagging video is submitted.
- `verification.verdict.approved` / `verification.verdict.rework` (topic
  `verification`), filtered `source.module == "counts"`,
  `ref_type == "workflow_death_signoff"` → approved closes the workflow review
  gate; rework resets both video actions to `rework`.
- The same verdict topics filtered to `ref_type == "workflow_birth_signoff"` →
  `ref_id` is one mother or child `workflow_id`; approved closes only that
  workflow's review gate, while rework clears only that subject's proofs without
  undoing permanent RFID.

Producer side: admin approval proves both videos inside the same transaction as
the death exit/status flip and opens `awaiting_verification`.
If either video is absent, the transaction rolls back with
`death_evidence_incomplete`, so neither the approval status nor the goat/count
can partially advance.
The resulting `goat.exited` event enqueues ONE verification item
(category **`death_evidence`**, vertical/module `counts`, ExpectedMedia
`["video","video"]`) carrying BOTH proof ids, ref
`(module=counts, ref_type=workflow_death_signoff, ref_id=workflow_id)`,
idempotency key
`counts-death-evidence:<workflow_id>:r<review round>:<proof refs, in order>`.
An authorized verifier reviews it under the dedicated **Death** tab in the
generic Verify queue. Birth uses the adjacent **Birth** tab and category
`birth_evidence`. Each mother or child workflow enqueues independently as soon
as all of its own operator actions are complete. Every item carries only that
subject's task proofs with `ref_type=workflow_birth_signoff` and
`ref_id=workflow_id`. The key includes the workflow, review round, and ordered
proof bundle, so retries deduplicate while a verifier-requested re-shoot creates
one fresh item for only that subject.

All workflow consumers are registered at the API bus, outbox relay, durable
domain consumer, and kernel-stage wiring sites. Event and verdict handlers are
idempotent. Redelivery of `goat.exited` heals a transient Verify enqueue failure,
while Verification's `(tenant_id, idempotency_key)` uniqueness prevents a
duplicate item for the same review round and proof pair.

The proofs are part of that key deliberately, mirroring counts shifting's
`counts-shifting-verification:<event>:<proof>`. Verification's `CreateItem` is
`ON CONFLICT (tenant_id, idempotency_key) DO NOTHING`, so keying on the workflow
alone means that after a verifier rejection the re-shot pair collides with the
rejected item, creates nothing, and strands the workflow awaiting a verdict that
can never arrive. Keying on the evidence pair makes a re-shoot a new review item
while a retry of the same completion still de-duplicates. Regression:
`tasks/app.TestReworkedDeathEvidenceReEntersVerifierQueue`. After verifier
rejection, both uploads reset to `rework`; because the admin-approved goat is
already dead, completing the second re-shoot returns directly to Verify without
a second admin approval. Verifier rejection never resurrects the goat.

All of the above are registered in
`context/architecture/domain-event-registry.json`.

## API surface (app, mobile)

The Birth/Death list header carries a bounded previous-day attention indicator. The backend returns
at most the five most recent Asia/Kolkata business dates where an open per-goat workflow has a
non-null `next_due_at` earlier than now. The Android client persists that summary in Room, shows a
bell badge only when such dates exist, and opens a five-second popup. Tapping a date selects that
business date and the existing Overdue filter. Completed, future-due, canceled, and
awaiting-verification-only cards do not enter this previous-day attention summary.

- `GET /app/workflows` (`CountsWrite`) — params `module=birth|death`,
  `date=YYYY-MM-DD` (defaults to today IST), `filter=all|overdue|due|completed|
  awaiting_video`, `cursor`, `page_size≤20`. Keyset over
  `(next_due_at, workflow_id)`; returns card DTOs + `chips` counts for the
  requested day + `next_cursor`.

- `GET /app/workflows/{workflow_id}` — context header + operator action list
  (≤18 rows); internal approval/verification actions are filtered out.
- `POST /app/workflows/{workflow_id}/actions/{action_id}/answer` — question /
  question_select; body `{answer_value, proof_ref?}`, `Idempotency-Key` header.
  A `requires_video` question without `proof_ref` returns 422 `proof_required`.
- `POST /app/workflows/{workflow_id}/actions/{action_id}/complete` — action
  type; body `{proof_ref?}`; `requires_video` without proof → 422
  `proof_required`. Initial death-video completions remain staged; a post-
  verifier-rejection re-shoot re-enters Verify when both are in.
- Operator action writes are strictly ordered within their section. Attempting a
  later step while an earlier sibling is incomplete returns 409
  `action_out_of_sequence`.

Migration `000041_birth_mother_video_medicine.sql` updates already-open mother
workflows to the same six-video contract and reopens any legacy completed mother
step that has no proof. This deliberately reverts prior proofless answers rather
than treating them as evidence. The migration also replaces the existing
Mother's Medicine detail in place; it does not create four child tasks.
Migration `000044_birth_ors_second_round_gate.sql` reopens only unverified ORS-2
completions recorded before their persisted 50-minute deadline; already-verified
history is not rewritten. Migration `000045_birth_ors_reopened_card_sync.sql`
atomically restores the workflow card counters for those reopened rows.
- `POST /admin-web/counts/approvals/{request_id}/approve` keeps the existing
  admin-web contract. For death, attempting it before both videos exist returns
  409 `death_evidence_incomplete`; a successful retry after both uploads applies
  the death exactly once.
- `POST /admin-web/counts/approvals/{request_id}/reject` keeps the existing
  admin-web contract and requires its existing rejection reason. For death it
  leaves canonical lifecycle/count truth unchanged and durably cancels the
  staged workflow.

## Birth submit deltas

- `POST /app/counts/birth-events` no longer requires an identifier from the
  app. It requires a canonical park and generates one distinct provisional tag
  per litter child (`CBE-#####` or `CPT-#####`, deterministically derived from
  the idempotency key + ordinal and uniqueness-checked). The Awaiting-RFID path
  is unchanged and is how "Tag the kid" promotes each child.
- New optional field `time_of_birth` (`HH:MM`, IST) on
  `CreateAdminGoatRequest`, stored as `goats.time_of_birth time`, carried in
  the `goat.created` payload (consumed by the workflow opener for EVENT+1H /
  session scheduling). Absent → the birth moment falls back to 07:00 IST on
  the DOB.
- The mobile form locks DOB to today, drops entry date (client sends today),
  drops the kid RFID/temporary toggle entirely, and adds the time-of-birth
  field. Breed is always visible and required. Mother RFID is a required
  scan/type field, and litter size is a required segmented choice: `1`, `Twins`,
  or `Triplets`.
- Submit-time identity validation resolves the mother RFID to the canonical
  mother goat UUID. In one transaction the approval row and every child goat,
  identifier, `goat_births` relationship, audit row, identity event, and
  `goat.created` outbox message are created. Each birth row starts
  `count_status=pending` and the herd projection excludes it.
- Web approval locks the entire litter and changes all sibling birth rows to
  `approved` only when the number of rows exactly matches `litter_size`; the
  projection trigger then adds every child. Approval returns a `birth_event`
  result and never invokes goat creation.
- A successful mobile sync means all canonical children exist. Android clears
  every entered value, returns to Birth, shows how many child workflows were
  created, and refreshes the normal Room-backed workflow list. It does not show
  pending-approval cards; count approval remains on the web queue.

## Death flow

Death report → staged death workflow opens → operator records death video with the live
camera → only then operator records post-mortem video with the live camera → operator may re-record either draft → operator taps **Submit** to lock and queue both videos → existing admin-web approval accepts/rejects → accept
atomically applies the guarded death exit (the only point where herd counts
change) and releases both proofs to Verify → verifier accepts or rejects → a
verifier rejection clears both proofs and resets both recording actions for a
mandatory re-shoot → completing both re-shots sends a fresh item directly to
Verify without a second admin approval.

Admin rejection is terminal for that staged report: counts/lifecycle remain
unchanged and the workflow is canceled. Verifier rejection is evidence rework,
not death rejection: the goat remains dead. The admin-web approval UI, its
information layout, and its existing role/permission behavior are unchanged.

On Android, successful submission tells the operator to open Death and record
both required videos. The workflow is server-created immediately and appears
through the normal Death workflow sync/list path; the client does not fabricate
the follow-up actions locally.

Before Submit, both videos are Room-backed drafts that survive refresh/process
recreation. Their rows show `Recorded` and a **Re-record video** control; the
outer workflow remains `0/2` because no server action has been submitted. Submit
is disabled until both drafts exist. Tapping it queues both proof uploads and
their ordered action completions, keeps and locks the local drafts while sync is
active, and shows disabled `Uploading…`. Only after both backend action
completions succeed does the footer become disabled `Submitted`; a failed upload
remains visible through Sync status. The Death list then shows
`2/2 · Submitted` and remains openable for operator review. There is no `Done`
control.

## Nav

The `counts` module bootstrap contribution `birth_death → /counts/birth-death`
is replaced by `birth → /counts/birth` and `death → /counts/death` (labels in
en/hi/kn/te). The old combined route is removed from Android navigation; the
add forms live at `/counts/birth/add` and `/counts/death/add` (L1 hosted
destinations, no root chrome).

## Out of scope (recorded)

- Abortion (RT-002) mother-only track. Once the server accepts a delivery, the
  form resets and returns to the Birth list.
- Per-farm colostrum session ownership config (`Colostrum Config`). Scheduling
  is already birth-time-derived; ownership remains on the normal backend task
  assignment path rather than importing Slack user IDs.
- Runtime-conditional scheduling grammar (`FUNC_ORS_2`, `RUNTIMECOND:*`),
  refusal counts, and `MODAL_FORM` / auto action types.
- Retiring the legacy combined route's backend pieces beyond nav removal.
