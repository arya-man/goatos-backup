# Birth & Death Follow-up Workflows (revised maintainer decision 2026-07-28)

Status: accepted and implemented · Owner: counts + tasks · Mock: `mock/birth-death-mobile-mock.html`
Roadmap source: `docs/mobile/mobile-feature-action-flows-roadmap-2026-07-30.md`

## Decision summary

Birth and Death split from the single write-only form at `/counts/birth-death`
into **two modules with their own routes** (`/counts/birth`, `/counts/death`).
Each module opens on the **outstanding SOP work per goat**. Birth work starts
only after the existing approval applies the birth. Death work is different: a
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
   cancels the staged workflow. Birth remains apply-at-approval and opens from
   `goat.created`.
   The admin-web approval page keeps its current information + accept/reject
   behavior. It is not the media-verification screen, and no admin-web UI or
   approval route shape or role grant is redesigned by this decision; only the
   death decision semantics are specialized as specified below.
3. **Birth identity flow** — the operator does NOT scan an RFID at birth. The
   server auto-generates a sensible provisional ID (temporary tag, `K-…`); the
   kid lives under that ID until the final birth SOP step **Tag the kid**, where
   the operator assigns the permanent RFID via the existing
   promote-identifier flow. From then on the permanent ID is used.
4. **＋ menu is New kid only.** Twins are recorded as separate kids (as today);
   Abortion (RT-002 mother-only track) is out of scope until a later slice.
5. **Sequential operator lanes + live-camera evidence (maintainer decision
   2026-07-28).** Within an operator section, only the first incomplete action is
   enabled; completing it enables the next action. A later command is rejected
   server-side with `409 action_out_of_sequence`. Birth's fixed-time colostrum
   session strip is a separate lane from its main track, so a birth-day session
   is not blocked behind the day+2 RFID action. Birth and Death video actions are
   live in-app camera only: there is no gallery/import option. For Death, each
   capture remains a durable local editable draft; re-recording replaces that
   action's draft, and no upload/action completion is queued until the operator
   taps **Submit** after both drafts exist. **Vaccination is explicitly exempt
   and keeps its existing gallery picker.**
6. **Death is exactly two operator steps.** The operator sees only Record death
   video → Record post-mortem video. Admin approval and media verification remain
   backend/admin/verifier state and are never rendered as a third operator step.

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

- `workflow_instances` — one row per (tenant, template, subject goat).
  Natural key `workflow_instances_natural_uq (tenant_id, template_key,
  subject_goat_id)`. Columns: `template_key`
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

projection-review: producer grain = `workflow_actions` unique
`(workflow_id, action_key)`; consumer card grain = `workflow_instances` unique
`(tenant_id, template_key, subject_goat_id)` = 1 row per card. `actions_done`
/ `next_*` are counters over exactly that workflow's actions (join key
`workflow_id`, 1:N pre-aggregated on write in the same txn). Chip counts group
`workflow_instances` rows by derived bucket at `(tenant_id, module,
event_date)` grain — numerator and denominator both range over the same
`workflow_instances` key set; no join fan-out. The operator counters and
`next_*` range over the identical subset (`section=main`, non-approval actions).
Approval/media review uses `workflow_instances.awaiting_verification`; it never
changes the action denominator or adds another action row.
The operator chip buckets are mutually exclusive: a submitted workflow with an
open verifier gate contributes only to `Awaiting video`, even if its persisted
workflow state is `completed`. It moves to `Completed` only after verifier
approval closes `awaiting_verification`, so one death cannot inflate both totals.

Templates are **code-defined** in `backend/internal/tasks/domain`
(`TemplateBirthKid`, `TemplateBirthMother`, `TemplateDeath`), versions of the
legacy sheet blocks with the shifting `TRIGGER_EVENT` steps dropped (shifting
is its own gated module):

- `birth_kid` (8 main steps): kid clean? · iodine dipping of umbilical cord ·
  front teeth outside lower gum? · suck reflex? · 1st Colostrum (video) ·
  Take Weight of Kid (question_select) · kid standing? (EVENT+1H) ·
  **Tag the kid** (EVENT+2D 07:00 IST; completes via promote-identifier, see
  below). Plus 5 `colostrum_session` rows (07:00 · 11:00 · 15:00 · 18:30 ·
  22:00 IST on the birth date) that do NOT count toward `actions_total`.
- `birth_mother` (6 steps): babies still inside? · is the mother licking her
  babies? · Mother's Medicine (action; detail carries the medicine text) ·
  ORS water · is the mother eating? · ORS water (2nd round; legacy
  `FUNC_ORS_2`, scheduled EVENT+6H in this slice — the runtime-conditional
  scheduler is out of scope and recorded here as the simplification).
  Opened once per dam (shared by twins via the natural key + ON CONFLICT).
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
  open `birth_kid` workflow for the goat; if the goat's `dam_id` resolves, also
  open (or attach to) the `birth_mother` workflow for the dam. Reads the
  canonical goat row for dob/time-of-birth/dam/park/shed (the event payload's
  new `time_of_birth` key is parsed by this consumer).
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
  identifier is permanent, complete that action. (The client never chains this;
  the promote flow stays the single writer of identifier truth.)
- `verification.verdict.approved` / `verification.verdict.rework` (topic
  `verification`), filtered `source.module == "counts"`,
  `ref_type == "workflow_death_signoff"` → approved closes the workflow review
  gate; rework resets both video actions to `rework`.

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
generic Verify queue. That tab filters category `death_evidence` and shows the
single item with both proof videos and its backend-owned details. Verify also
exposes a separate **Birth** tab filtered to the reserved `birth_evidence`
category; it stays empty until the maintainer supplies and approves the complete
Birth verification producer flow.

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

- `GET /app/workflows` (`CountsWrite`) — params `module=birth|death`,
  `date=YYYY-MM-DD` (defaults to today IST), `filter=all|overdue|due|completed|
  awaiting_video`, `cursor`, `page_size≤20`. Keyset over
  `(next_due_at, workflow_id)`; returns card DTOs + `chips` counts for the
  requested day + `next_cursor`.
- `GET /app/workflows/{workflow_id}` — context header + operator action list
  (≤13 rows); internal approval/verification actions are filtered out.
- `POST /app/workflows/{workflow_id}/actions/{action_id}/answer` — question /
  question_select; body `{answer_value}`, `Idempotency-Key` header.
- `POST /app/workflows/{workflow_id}/actions/{action_id}/complete` — action
  type; body `{proof_ref?}`; `requires_video` without proof → 422
  `proof_required`. Initial death-video completions remain staged; a post-
  verifier-rejection re-shoot re-enters Verify when both are in.
- Operator action writes are strictly ordered within their section. Attempting a
  later step while an earlier sibling is incomplete returns 409
  `action_out_of_sequence`.
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
  app: when neither `animal_identifier_1` nor `temporary_identifier` is
  supplied, the handler generates a provisional temporary tag
  (`K-` + zero-padded random numeric suffix, uniqueness-checked) before
  prepare, so the stored approval payload is fully explicit. The Awaiting-RFID
  path is unchanged and is how "Tag the kid" resolves.
- New optional field `time_of_birth` (`HH:MM`, IST) on
  `CreateAdminGoatRequest`, stored as `goats.time_of_birth time`, carried in
  the `goat.created` payload (consumed by the workflow opener for EVENT+1H /
  session scheduling). Absent → the birth moment falls back to 07:00 IST on
  the DOB.
- The mobile form locks DOB to today, drops entry date (client sends today),
  drops the RFID/temporary toggle entirely, and adds the time-of-birth field.

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

- Abortion (RT-002) mother-only track; Twins pre-fill flow.
- Colostrum decaying multi-day series + per-farm session ownership config
  (`Colostrum Config`); this slice ships the fixed day-one 5-session strip.
- Runtime-conditional scheduling grammar (`FUNC_ORS_2`, `RUNTIMECOND:*`),
  refusal counts, and `MODAL_FORM` / auto action types.
- Retiring the legacy combined route's backend pieces beyond nav removal.
