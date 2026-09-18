# SOP-driven Herd Operations (maintainer decision 2026-09-13)

Status: accepted and implemented (phase 1; capture-form parity 2026-09-16, see below) · Owner: counts + tasks + sop + admin-web + Android
Framework source: `goatOS_Config_Framework.pdf` v2 (Categories, Task Types, Items, SOP = ordered
tasks + schedule, cross-category chaining).
Machine enforcement: `make sop-driven-herd-operations-guard`
(`tools/agent-hooks/check-sop-driven-herd-operations-guard.mjs`) plus the golden compile test
`backend/internal/tasks/domain/sop_followup_golden_test.go`.

## Decision

Every Herd Operations flow is driven by its SOP, authored on the web. The operator's steps after a
birth, a death, a shifting authorization and a reconcile card -- **which questions, which proof
(video × n, photo × n), and when each step is due** -- come from the `follow_up` section of the
PUBLISHED `sop_versions.form_dsl` for the SOP code, compiled at workflow open by the tasks engine
and **pinned on the workflow** (`workflow_instances.sop_version_id`). Publishing a new version on
`/counts/sops` changes the next workflow opened; a workflow already open keeps the steps it started
with (the same pin-at-start rule health protocols use: nothing mid-process changes under an animal).

This supersedes, for the Herd Operations SOPs only:

1. migration `000175`'s header -- "library documents, not a second execution engine";
2. `docs/decisions/birth-death-workflows.md` -- "instantiated from code-defined templates";
3. `docs/decisions/pen-reconciliation.md` -- "exactly one video, no questionnaire".

Nothing about the **capture** forms (Add birth / Add death / Raise shifting) changed in phase 1; they
keep their canonical fields, and the SOP's capture `fields` are passed through verbatim when the
operator steps are published. Shifting **completion** steps are seeded and authorable but the phone
still runs the fixed shifting execute screen; wiring it onto the engine is phase 2.

## The pieces

| Framework concept | Goat OS implementation |
|---|---|
| Category Registry (meta) | `sop_categories` (Commodity / Problem / Event / Action / Equipment), seeded by `000308`; editing UI = phase 2 under `/config`. Herd Operations items are **Action**. |
| Task Type Registry (meta) | `sop_task_types` with `answer_kind`, `engine_hook`, `parameter_schema`, seeded by `000308` from `tasks/domain/sopseed/task_types.json`; served to the builder as the `sop_task_types` / `sop_task_type_answer_kinds` option groups (tenant rows, never constants). |
| Item | `sop_definitions` + `category_key`, `subcategory`, `triggers` (chaining = phase 2). |
| SOP = ordered tasks + schedule | `form_dsl.follow_up.tracks[].steps[]` -- `task_type`, `title`, `detail`, `options`, `proof {video, photo}`, `schedule` (`immediately` / `after_event` / `at_fixed_time` / `series` / `after_step`), `section`, `hard_time_gate`, `wait_for_all`, `requires`, `when`. |
| Schedule entry | `workflow_actions` rows stamped by `tasks/domain.CompileTrack`, carrying `task_type`, `answer_type`, `engine_hook`, `proof_min_videos/photos`, `proof_refs`, gates, `rework_reason`. |

### Engine semantics are not free text

A step the server must act on names a registry task type whose `engine_hook` selects the behaviour:
`weigh_kg` (numeric kilograms), `tag_kid` (RFID promotion gate), `record_pen` (the park's kid pen,
answer `<shed_id>|<partition_label>`), `colostrum_feed` (Colostrum lens), `death_evidence`
(released to Verify on approval), `return_to_pen` (reconcile). The hook is matched on the step KEY,
so a relabel never detaches it; the web editor keeps those steps' key and type fixed and everything
else editable. Rows stamped before `000308` are backfilled so the generalized gates
(`hard_time_gate`, `wait_for_all`, `requires_keys`, `after_action_key`) reproduce the old
key-matched behaviour exactly.

### Day-one behaviour did not move

The seeded documents (`tasks/domain/sopseed/*.json`, embedded verbatim in `000308`) are added
IN PLACE to each tenant's currently published library version -- on STG that is Birth Recording
v1, Death Recording v1, Shifting v2 -- so the version a farm sees on deploy is the one it already
had, now carrying the steps (no workflow is pinned to a version before this migration; a later web
publish creates v+1 as usual). Reconcile is a new SOP and starts at v1. Those documents compile
byte-identical to the Go templates they replace -- pinned by
`TestSeededBirthCompilesToTheLegacyTemplates` (three birth moments × with/without the pen fallback),
`TestSeededDeathCompilesToTheLegacyTemplate`, and `TestMigrationEmbedsTheSeededDocuments`. Each was
mutation-tested. `templates.go` stays only as that oracle; the guard blocks any production call to it.

A tenant with NO published version for a code (created after the migration, a test fixture) runs the
seeded document, pinned to no version. A published version that is broken fails CLOSED with the
field named -- and cannot be published in the first place: `sop/app` validates `follow_up` against
the live registry and refuses a herd-ops version missing its required track.

### Multi-proof, and what the phone renders

A step may require several videos and photos. The phone queues each capture as its own upload
immediately and enqueues ONE completion/answer carrying all of them by outbox reference
(`proof_outbox_items` → `proofs [{ref, kind}]`); the backend rejects `422 proof_required` when a kind
is short. Answers: yes/no, pick-one, pick-many (joined with `|`), number, text, and the pen picker.
Uploaded proofs of a done step render INLINE (photo bytes, video player) -- `ProofMediaPreview`'s
`inlineRemotePhoto` is a detail-screen opt-in; lists keep it off (one signed read per proof id, bounded
to one workflow). A verifier's rejection reopens exactly the proof-bearing steps and copies the
reason onto them (`rework_reason`), so the operator sees what to re-shoot.

Known trade (documented, not hidden): the per-step pending-capture list is in memory. A process
death mid-step keeps the uploads (already queued) and asks for the step's captures again; the
orphaned upload is unattached and harmless.

### Reconcile

The card is still the unit of work and the verifier still judges one item per card (no approver).
Opening a card asks the backend for its questionnaire workflow
(`POST /app/counts/pen-reconciliation/cards/{card_id}/workflow`, idempotent), the operator runs the
steps on the shared workflow drill-in, and when the last step completes the tasks engine calls the
counts completion hook, which runs the SAME `Complete` path the legacy one-video route uses with
every captured proof (verification enqueue, idempotency and recovery are one implementation). A
verifier rework reopens the proof steps from the tasks side (`SubjectWorkflowVerdictHandler`, keyed
on the card id as `subject_ref_id`, registered in the ONE shared `RegisterWorkflowConsumers` so
every bus process behaves alike). The legacy one-video complete route stays served for old APKs.

## Proof (2026-09-13/14, OCI `goatos_sopqa`, Realme JJ6LVC8DCMFYMN4P as Amit Kumar)

- Web: Birth Recording → Edit operator steps → photo added to step 1, standing check moved to 90
  min, text step added → Published v4; later v5 (text step after Kid condition); Reconcile v2
  (question + video + photo).
- Phone: a birth recorded on the phone opened a kid on v4 (22 steps): yes/no + video + photo,
  video-only, photo-only, pick-many + both proofs, 3.4 kg + video, standing due at +90 min, the pen
  picker, free text; uploaded proofs (12.4 MB video, 113 KB photo) rendered inline. Publishing v5 left
  that kid on v4/22 while the next birth opened on v5/23. Reconcile: card → questionnaire → video →
  web Verify played the phone's video → reject → phone showed Rework with the reason → re-shoot →
  approve → Completed; a card opened after v2 ran 0/3. Offline: with the API down the re-shoot
  queued, the card stayed `rework`, and it drained to `pending_verification` on restart. Process
  death mid multi-proof recovered cleanly. Death: two videos → Submit → both on the backend, stamped
  from Death Recording v2.
- Final APK (commit after the guard fixes): the v4 kid's done steps render the uploaded photo and
  video inline; play / full-screen open / full-screen close each emit
  `workflow_proof_preview_action`. `make guardrails`, backend build/vet/tests for tasks / sop /
  counts / adminui / eventwiring, admin-web 642 tests, Android WorkflowDetail VM tests all green.
  The two durable buses (`kernelstages/bus.go`, `cmd/domain-event-consumer`) now call the shared
  `RegisterWorkflowConsumers` instead of a hand list, so the reconcile rework reopener is live on
  every bus process; the cascade guard reads that helper's constructor list.

## Capture-form parity (2026-09-16)

The Add birth / Add death forms now take a SOP-authored **capture card** -- photo/video slots
(`video | photo | either`, required or optional) and questions -- authored on `/counts/sops`
(`?part=capture`) and published with the SOP version. It sits beside the `follow_up` track, not
inside it: the card is what the operator records AT REPORT time, the track is the work after.

- **Absent card = today.** `ParseCaptureCard` reads a missing card as empty; the form renders, gates
  and enqueues exactly as before, and the request carries no `sop_capture`.
- **Wire shape.** Phone -> server: `sop_capture {sop_version_id, proofs{slot_key: proof_id}, answers}`
  on `POST /app/counts/birth-events` and `/death-events`. Server -> reviewers:
  `CountsApprovalCapture {version_label, rows:[{label,value,group?}], media:[{proof_id,label,kind}],
  missing_note?}` on the approval list, rendered verbatim by the admin-web approvals drawer and the
  phone Approval screen. `GET /app/counts/capture-cards/{kind}` serves the published card (Room-cached
  on the phone, refreshed on every open).
- **Older app vs new app.** A request with no `sop_capture` is ACCEPTED and its capture is recorded
  as "Not captured (older app)". A request that sends one is judged strictly: an unknown version is
  `409 capture_sop_version_unknown`; a missing/extra/wrong-kind slot is
  `422 capture_proof_slot_invalid`; a bad answer is `422 capture_answer_invalid`; both name the field
  in `field_errors[0].field`. The proof's kind is read from the proof register, never the client.
- **Birth capture is verified PER FORM PROOF SLOT** (maintainer correction, 2026-09-16). A birth with
  capture media emits `counts.birth.reported`; its consumer enqueues ONE verification item per slot,
  key `counts-birth-capture:<birth_event_id>:<slot_key>:<ref>`, subject "Birth report · <slot title>",
  carrying only that slot's proof plus the form answers as context rows (group "At report"). A
  questions-only card enqueues nothing. Verdicts are fenced by slot AND ref (a verdict on a superseded
  proof is ignored); the per-slot map rolls up into `capture_review_status`; a reject re-shoots ONLY
  that slot, as a `reshoot_report` step appended to the birth workflow.
- **Death is unchanged in shape**: one bundle item. Its bundle now opens with the report's capture
  media labelled "At report · <slot>" (and the answers as "At report" rows), then every step's proofs
  in step order. A rejected report proof is re-shot through the same `reshoot_report` step, and the
  recording replaces that proof in the stored capture.
- **Death follows its SOP steps.** The approval gate waits for EVERY authored step
  (`tasks/domain.DeathStepsComplete`), the bundle carries every step's proofs, and the phone records
  every step as drafts (videos, photos, answers in `workflow_step_draft_answer`, Room 67) and sends
  them from one Submit. The fixed two-video gate is gone on both sides and is blocked by
  `sop-driven-herd-operations-guard` rules `death-pair-hardcoded` / `death-pair-on-phone`.
- **Verifier items carry labelled media** (`verificationdomain.BuildMediaMeta`) and answer context
  rows for birth steps, death and reconcile.
- **Reconcile**: a workflow-backed card now completes through its own workflow (the completion SQL
  matched `workflow_id IS NULL` only), and the debt list carries the full proof set with media labels.
- Schema: migrations `000337` (workflow capture evidence, reconcile media meta), `000338` (counts
  approval capture columns), `000339` (birth-reported outbox index). These numbers were rebased after
  `origin/main` and are renumbered at merge.

**Unavoidable deploy-day differences with no SOP edited:** verifier items carry media labels (the step
titles) and birth step items carry their answers as context rows; a death rework now records the
verifier's reason and clears the step's proofs; a workflow-backed reconcile card completes instead of
failing; proof kinds are read from the register; the death Submit messages no longer say "both
videos ... backend".

Pinned by `TestSeededDeathDocumentBehavesAsToday`, `TestSeededReconcileQuestionnaireBehavesAsToday`,
`TestNoCaptureCardSubmitsBehaveAsTodayPg`, the Android seeded-behaviour cases in
`AddBirthViewModelSopCaptureTest` / `AddDeathViewModelSopCaptureTest` /
`WorkflowDetailViewModelDeathFollowsSopTest`, and the per-slot birth tests
`TestBirthCaptureEnqueuesOneItemPerFormProofSlot`, `TestBirthCaptureRejectReshootsOnlyThatSlot`,
`TestBirthCaptureStaleVerdictOnSupersededRefIsIgnored`, `TestBirthCaptureQuestionsOnlyFormEnqueuesNothing`.

## Phase 2 (partly done)

Shifting completion is SOP-driven since 2026-09-16 -- as a CARD-WITH-SLOTS on the existing
completion transaction, NOT on the tasks engine (`shifting-sop.md`: raise extras, completion card,
high-priority card, pinned per movement at raise; the feed-config fingerprint stays on the row lock).
Birth / death capture forms take SOP-authored captures and questions (capture-form parity, above).
Still open: the `/config` editor for the Category and Task Type registries; cross-category
`triggers` (Problem → Action → Commodity); migrating vaccination / feed / weighing onto the same
engine.
