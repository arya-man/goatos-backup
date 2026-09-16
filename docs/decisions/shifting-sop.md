# Shifting is SOP-driven: raise extras, completion card, high-priority card

**Maintainer decision 2026-09-16** (SOP → operator + verifier parity program, workstream C).
Companion to `shifting-verification.md` (approve-first, atomic apply, feed fingerprint), `feed-sop.md`
and `weighing-sop.md` (the same card-with-slots shape). Machine gate: `make shifting-sop-guard`.

## Decision

The `shifting` SOP (library code since migration 000175) carries a `form_dsl.shifting` section with
THREE CARDS, authored on `/counts/sops`:

| card            | when the phone renders it                     | seed (= today's behaviour)                          |
|-----------------|-----------------------------------------------|-----------------------------------------------------|
| `raise`         | on the raise form, before approval            | nothing (no captures, no questions)                 |
| `completion`    | on every completion                            | one live-camera **Shifting video**, compulsory      |
| `high_priority` | ADDED to the completion of a high movement    | **Feed packing video** + **Feed given to animal video** |

Each card is an instruction, an ordered list of CAPTURE SLOTS (key, title, hint, kind
`video | photo | either`, compulsory or optional) and QUESTIONS -- the shared building blocks in
`backend/internal/sop/authored`. A card may be questions-only. The seeded slot keys are the exact
proof-register field keys the phones have always stamped (`shifting_<step>_video`), so a capture
uploaded before this change is a capture of the seeded slot.

**Card-with-slots, NOT the tasks engine.** Shifting keeps its atomic apply transaction, the
approve-first gate, the Feed Config fingerprint re-check under the row lock, its idempotency and
`BounceShiftingEventForRework`. The SOP decides only what the operator captures and answers. The
dormant seeded `follow_up` track on the shifting SOP is no longer required (`sop/app`
`requiredFollowUpTracks`) and the web editor opens the cards editor, not the steps editor.

## The pin

A movement is stamped at RAISE with the version in force (`shifting_events.sop_version`; the phone
may echo the version it rendered as `sop_version`, and an unknown one is refused
`shifting_sop_version_unknown`). Its raise card was judged on that version and its completion runs
on it to the end, whatever is published in between. `NULL` / `0` is the seeded document. Queued
verifier items are never relabelled. The pending-execution read serves each row's pinned cards
(`sop`, `high_priority_sop`), resolving each version ONCE per page.

## Validation (`counts/domain.ValidateShiftingSOP`)

- `completion`: 1..8 slots, at least one compulsory -- the move must be proven by something the
  verifier can see.
- `high_priority`: 1..8 slots, at least one compulsory -- the feed evidence cannot be authored away
  while the Feed Config fingerprint still gates a high-priority completion.
- `raise`: 0..8 slots, may be questions-only or empty.
- Slot keys unique across the three cards; question ids unique across the three cards; unknown
  keys refused at save (`UnknownShiftingSOPKeys`).

## What the operator, the approver and the verifier see

- **Raise**: the destinations read carries the published raise card; the phone renders it; the
  raise is judged BEFORE anything is written (422 `shifting_answer_invalid` /
  `shifting_proof_slot_invalid` naming the item). The approver's snapshot
  (`raise_capture_evidence`, the shared `CountsApprovalCapture` shape) rides the approval payload
  and `CountsApprovalListItem.capture`: version label, answers in farm words under "At raise",
  captures under their slot titles, and a note naming what an older app did not send.
- **Completion**: `POST .../complete` carries `proofs` {slot key: ref} and `answers`. Judged
  against the pinned completion (+ high-priority) card: missing compulsory, unknown key, one ref
  in two slots, wrong kind -> 422 `shifting_proof_slot_invalid` (`slot`); required question ->
  422 `shifting_answer_invalid` (`question`). The legacy columns `proof_ref` /
  `feed_packing_proof_ref` / `feed_given_proof_ref` mirror the seeded slots (`proof_ref` falls back
  to the first capture; it is never NULL on a completed move). The 000053 feed-evidence CHECK is
  re-added strictly weaker in 000329 so an authored high-priority card may rename or replace the
  two seeded clips.
- **Verifier**: ONE `shifting_move` item; media in order completion -> high priority -> raise, each
  with `media_meta` {slot title, kind the register judged} (raise captures read
  "At raise · <title>"); context rows "Moved from" / "Moved to", then the answers grouped
  "At raise" / "Completion" / "High priority". The idempotency key keeps the pre-SOP shape for a
  seeded submission (`ShiftingVerificationKey`), so an older phone's retry collapses onto the item
  it already created.
- **Rework**: a resubmit keeps the stored answers when it carries none; the applied move stays
  applied. Resubmitting the rejected capture is accepted exactly as before this change (its
  same-refs key collapses onto the existing item) -- deploy-day parity, maintainer 2026-09-16.

## Older app (program decision 7)

A request carrying NEITHER `proofs` NOR `answers` predates the SOP fields. It is ACCEPTED: its
fixed fields are mapped onto the seeded slots (a seeded key the farm authored away is re-targeted
onto the first free video slot of the same section), what it sent is judged for kind and placement,
and every compulsory slot / required question it could not send becomes a row
`"<title>: Not captured (older app)"` for the approver and the verifier. A request carrying the
new fields is judged strictly. One exception keeps its pre-SOP answer: an older app's HIGH-priority
completion that omits either feed clip is refused `422 feed_proofs_required` -- that phone always
had both clips to send, so the refusal forces no update, and leniency must never apply a
high-priority move with no feed evidence (found by the 2026-09-17 E2E). Both shapes hash identically for a seeded submission (the canonical
completion fingerprint folds a seeded-only `proofs` map back onto the legacy triple), and the raise
fingerprint is byte-identical to the pre-SOP handler's (goldens pinned in
`shifting_sop_handler_test.go`), so an installed phone's retry is never a same-key conflict.

## Schema

- `000328_shifting_sop.sql`: `shifting_events` + `sop_version`, `raise_sop_proofs`,
  `raise_sop_answers`, `raise_capture_evidence`, `sop_proofs`, `sop_answers` (no defaults,
  metadata-only); the seeded section added IN PLACE to each tenant's published `shifting` version
  (pinned by `TestMigrationEmbedsTheSeededShiftingSOP`).
- `000329_shifting_feed_evidence_check_authored.sql`: the same-named CHECK re-added strictly weaker
  (NOT VALID then VALIDATE, no transaction, lock_timeout).

## Where the rules live

- Document + seed: `backend/internal/counts/domain/shifting_sop.go`, `sopseed/shifting.json`.
- Judge (the only minter of `ErrShiftingProofSlotInvalid`): `backend/internal/counts/app/shifting_sop.go`.
- Rules source (the only reader of `sop_versions` on counts' behalf):
  `backend/internal/shiftingsop/adapters/postgres/rules_source.go`; save-time contract:
  `backend/internal/shiftingsop/app/sop_contract.go`.
- Web editor: `apps/admin-web/features/sops/shifting-{model,editor,summary}.ts[x]`.
- Phone: `ShiftingExecuteViewModel` runs two `FeedSopSlotController`s (completion, high priority)
  from the pending item's pinned cards. Durable drafts: the completion card under the movement id,
  the high-priority card under `<movement>:high` (one answers row per entity), and one-shot reset
  markers (rework cutoff, Feed Config fingerprint) under `CaptureFlow.SHIFTING_MARKERS` so they never
  count as captures on the Actions list. A reset is applied ONCE and is cutoff-based: re-opening a
  movement mid-rework keeps a clip recorded after the reset, and Room never re-fills a slot with a
  rejected take. Clips an older build stored under `shifting` / `packing` / `feeding` fill their seeded
  slots. A cached row without `sop` runs `ShiftingSopSeed.kt`, which `make shifting-sop-guard` pins to
  the backend seed JSON. The Actions list's "captures done / required" counts the pinned cards'
  compulsory slots. The raise form runs one controller from the destinations read's `sop`; the
  approvals queue renders the backend `capture`.

## Proof

- Domain: `counts/domain/shifting_sop_test.go` (seed = pre-SOP behaviour, migration embeds the
  seed, validation by path, legacy mapping, priority-scoped slots, mirrors, verification key shape,
  questions-only raise).
- App: `counts/app/shifting_sop_test.go` (pinned-not-latest, refusals by name, legacy triple,
  older-app rows, unapproved-before-judge, media meta + grouped rows incl. raise, either kind,
  unknown pin, unwired seed, one rules read per page, rework reuse refused).
- HTTP: `counts/adapters/http/shifting_sop_handler_test.go` (pins published / echoed version,
  refuses a required raise answer writing nothing, identical retry replays after a later publish,
  legacy fingerprints unchanged, seeded slot map = legacy triple, legacy proof_required pre-check
  only for the legacy shape, approvals list carries the capture) and the error-mapping test.
- Postgres (OCI): `counts/adapters/postgres/shifting_sop_integration_test.go` (stored map +
  mirrors + replay, authored high card satisfies the 000329 CHECK, feed_config_changed still
  refused, publish-after-raise keeps the pin, rework queues a fresh item and keeps applied);
  `shiftingsop/adapters/postgres/rules_source_integration_test.go` (published / pinned / batch /
  cache / unknown; migration adds the section in place).
- Bridge: `countsbridge/shifting_media_meta_test.go`. Contract: `shiftingsop/app`,
  `sop/app/shifting_followup_test.go`.
