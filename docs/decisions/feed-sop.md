# Feed SOP: the feed cards are authored on the web (maintainer decision 2026-09-16)

Status: accepted and implemented · Owner: feeddirection + feedsop + sop + adminui + admin-web + Android
Machine enforcement: `make feed-proof-collaboration-guard` (mode 1, the slot-list model) plus the
golden tests `backend/internal/feeddirection/domain/sop_test.go` (the seeded cards ARE the pre-SOP
behaviour) and `sop_migration_test.go` (migration 000342 embeds the seeds verbatim).

## Decision

The three feed SOP documents (`feed.direction`, `feed.packing`, `feed.transport`, library
documents since the 2026-08-18 SOP split) stop being library documents. The CARD the crew runs
each feed stage under is the **`feed` section of the PUBLISHED version**, authored on **Feed →
Feed SOP** (`/feed/sops`):

| Card | Document | What the author decides | Who reads it |
| --- | --- | --- | --- |
| **Distribution** | `feed.direction` → `feed.distribution` | the instruction; the captures (one to eight slots, each a key, title, hint, kind `video` / `photo` / `either`, compulsory or optional); questions the crew answers per pen-session | the sheet (pinned at ISSUE), the phone's distribution screen, the completion write, the verifier item |
| **Wastage** | `feed.direction` → `feed.wastage` | same shape (the seed is one leftover-feed video) | the wastage worklist and screen, the completion write, the verifier item (the approve still carries the leftover weight) |
| **Packing** | `feed.packing` → `feed.packing` | same shape (the seed is one packing video) | the packing worklist and screen, the completion write, the verifier item |
| **Transport** | `feed.transport` → `feed.transport` | same shape (the seed is one transport video) | the task (pinned when it is MATERIALIZED), the transport screen, the submit, the verifier item |

The maintainer's words, 2026-09-16: "feed direction now we are having one photo, two videos for
feed and water; I will change in future -- if I give any other option in between it should
come, if I remove anything it should go; same for packing, one video, photos also; and the same
for transport, wastage -- everything should be backend driven ... and in the verifier also
whatever we update here they should see, it should not be again a new deployment."

## What did NOT change, and is load-bearing

- **The collaboration contract** (`docs/product/feed-proof-collaboration.md`). A distribution
  pen-session is still ONE shared workspace across every operator of the park; every slot is an
  independent offline-first proof write; **no slot gates another** (a slot is enabled by its own
  state alone, machine-checked on the slot LIST now, not on three named getters); a slot shot on a
  teammate's phone is adopted by its server proof id **by slot key** -- including a key that did
  not exist when the phone's APK was built; the submit names every slot's reference (own outbox
  row or teammate's id) and is keyed on that proof set plus the answers, so two phones submitting
  the same pen are one write; the session lock (`pending_verification`) still propagates by the
  live status poll.
- **The verification gates and grains.** Distribution and packing per pen-session (packing per
  BAG), wastage per pen-day, transport per shed. One verifier item per completion, the approve
  carries the wastage number, sampling untouched.
- **The media invariant.** Every capture is the LIVE in-app camera, compressed and overlay-burned
  on the way out; a photo slot goes through the photo pipeline, a video slot through the video
  pipeline, an `either` slot through whichever the operator picked.

## The shape

- `form_dsl.feed` (`schema_version: goatos.sop-feed.v1`) = per document the stage blocks above,
  each `{instruction, proofs[{key,title,hint,kind,required}], questions[]}`; the question shape is
  the weighing removal card's (`sop/authored`). A `required` flag that is ABSENT reads as
  compulsory. Limits: 1..8 slots, unique keys in the id pattern, at least one compulsory slot.
- `feeddirection/domain.ParseFeedSOP` + `ValidateFeedSOP` name every problem by path;
  `feedsop/app.FeedSOPContract` runs them at version create through `sop/app.WithFormDSLContract`,
  so a card the crew could not run is never saved.
- **Seeds = day one.** `feeddirection/domain/sopseed/*.json` are the pre-SOP behaviour byte for
  byte: distribution = feed weight PHOTO + feed VIDEO + water VIDEO (slot keys are the proof
  register field_keys the phone has always stamped: `feed_distribution_feed_weight_photo`,
  `feed_distribution_video`, `feed_distribution_water_video`); packing = `feed_packing_video`;
  transport = `feed_transport_video`; wastage = `feed_wastage_video`. Migration `000342` adds the
  section IN PLACE to each tenant's currently published version (the 000308 / 000315 shape) and
  embeds the seeds verbatim.
- **Versioning and the pin.** A sheet is stamped at ISSUE with the published distribution card
  version (`feed_direction_issues.sop_version`) and packing card version (`packing_sop_version`);
  a transport task is stamped when MATERIALIZED (`feed_transport_tasks.sop_version`). The work runs
  on that version to the end: the reads, the completion writes and the verifier items of that
  sheet/task all read `RulesVersion(pinned)`, never the latest publish. A publish today reaches
  TOMORROW's sheet (issued under it) and every task materialized from now on; today's in-flight
  sheet keeps the card its crews started on. Version 0 / `NULL` = the seed. A pinned version the
  farm never published is refused by name on the write paths (`409 feed_sop_version_unknown`) and
  rendered with the seeded card on the read paths so today's work is still doable.
- **Evidence.** Every completion row stores `sop_proofs {slot key: proof ref}` + `sop_answers`
  (migration 000342, backfilled from the legacy columns). The legacy single-proof columns mirror
  the seeded slots (blank when the card dropped that slot; the CHECKs now require a non-empty
  `sop_proofs`, not the legacy columns). The submit is judged slot by slot against the pinned
  card: a compulsory slot missing, an unknown slot, one proof naming two slots, or the wrong
  medium for a slot is `422 feed_proof_slot_invalid` naming the slot; a required question
  unanswered is `422 feed_answer_invalid` naming the question. **An older phone that still sends
  only the legacy fields is mapped onto the seeded slot keys and judged the same way**; the HTTP
  handlers' legacy `proof_required` pre-checks apply only to a request with no card-shaped
  `proofs`.
- **The verifier sees the card without a deployment.** Each verification item is enqueued with
  the proofs in CARD ORDER plus `media_meta {label, kind}` from the card and the answers as context
  rows; the verification read composes the item's media from `media_meta` first, so the web
  drawer and the phone's verify detail show "Trough after feeding · photo" the day the card
  changes. Proven on the QA clone: a v3 card without the water slot produced an item with exactly
  two labelled media.
- **Web.** The `/feed/sops` drawer summarises each card (`feed-summary.tsx`); **Change SOP** opens
  the card editor (`feed-editor.tsx`, model `feed-model.ts`, round-trip of the seed byte-faithful,
  `feed-model.test.mjs`), reusing the weighing slot and question rows; Save / Publish go through
  `saveFeedVersion` / `publishFeedVersion`. Copy comes from the page contract (`feedSOPEditorCopy`,
  `fsop.*`). A publish took 1.7 s end to end in the live run.
- **Phone.** Every feed stage read carries the card (`sop` on the preview and worklist pages per
  workflow, on the captures read, on the transport task). The four screens render a card-ordered
  slot list (`FeedSopCardUi` / `feedSopCardItems`, one Compose item builder for all stages) with
  the card's own words; an `either` slot offers both verbs. Card sources, weakest first: the
  SEEDED card compiled in (a phone that has never been online still shows the crew today's SOP),
  the sheet's card cached in Room by the list (`observeDirectionCard` / `observePackingCard` /
  `observeWastageCard`, read from the list's envelope by date because the list's park segment is
  blank on the default farm selection; the transport task row carries its own), and the live
  captures read (distribution; authoritative, arrives with the teammate slots). Drafts are per
  slot key (SavedStateHandle for distribution, the durable capture-draft store with step = slot
  key for packing/wastage/transport, the old `video` step mapped onto the seeded key so a clip
  recorded before this build still fills its slot). The completion outbox rows carry
  `slot_proofs {key: own outbox row | teammate proof ref}` + `answers`; the dispatcher resolves
  each to a server proof id (waiting on a pending upload exactly as the fixed trio did) and
  mirrors the seeded slot into the legacy field. `FeedSopSlotController` is the shared machinery
  for the three single-operator stages; the distribution ViewModel carries the same shape inline
  because it also adopts teammates' proofs.

## Proven in the live run (QA clone `goatos_fcqa`, Realme phone, 2026-09-16)

1. Seeded card on the phone from the server (3 slots, backend hints, not the APK's).
2. Pin the pen's sheet to v2 (+ optional `trough_after_feeding` photo) → one Sync tap → the
   4th slot appears, marked Optional; the submit does not wait for it.
3. Publish v3 on `/feed/sops` removing the water video → pin → Sync → the water slot is gone.
4. Capture photo + video → submit with the optional slot empty → `pending_verification`,
   `sop_proofs` = exactly the two card slots, `water_proof_ref` blank → the verifier queue item
   carries two labelled media ("Feed weight photo" image/jpeg, "Feed distribution video"
   video/mp4). The first attempt surfaced the handler's legacy `water_proof_ref is required`
   pre-check (422) on the phone with the server's sentence; fixed to apply only to legacy-shaped
   requests.
5. Packing, wastage and transport worklists and tasks carry their pinned cards (transport tasks
   materialized with `sop_version = 1`).

## Second sweep, same day (edge cases, maintainer ask "play with everything")

Run on the combined QA tree (feed SOP + feed-config dispatch fixes), same clone and phone.

- **Editor validation** (22 malformed cards via the API, each refused by path): >8 slots, no
  compulsory slot, duplicate / blank / badly-formed keys, unknown kind, blank title, question
  without id, pick-one without choices, min > max, `only_if` on an unknown or LATER question,
  `allow_other` without an `other` option, unknown fields, wrong schema version. Long titles are
  accepted (no cap) -- the phone wraps them.
- **Questions on the phone**: choice / conditional number with unit / multi / text; the
  conditional appears only on its trigger value; a required question blocks Submit with its own
  sentence; typed answers land (`3.5`, `["water","sick"]`) and the verifier drawer shows the
  labels ("Leftover from last feed · 3.5 kg", "No water, Sick animal seen"). Authored through the
  real editor too (the wastage question) -- id derived from the title, default Yes/No.
- **Remove / re-add**: a removed slot and its questions leave the screen on Sync; the orphaned
  upload stays in the register and comes back as "Proof ready" if the slot is authored again.
- **Pin by clock, per park**: the same publish between two parks' experiment clocks left CBE on
  v20 and CPT on v21; the running day never moved.
- **Offline**: the screen renders from Room with no network at all; a capture taken offline
  queues and uploads on reconnect without a tap; process kill and cold start restore the slots.
- **Multi-operator**: same proof set from a second operator replays; a different set is 409;
  unknown slot / wrong medium / one proof for two slots are 422 naming the slot.
- **Packing v2** (video + required photo) and **transport v2** (`either` slot + number question,
  out-of-range answer refused by the server's sentence, corrected resubmit lands) on the phone.
- **The 14:00 correction on a "Part N" pen** (real lifecycle path): a goat moved into Godel 1 -
  Part 1 after both bags were packed -> both bags `rework` with the old-vs-new sentence, both
  verifier items withdrawn.

Defects found and fixed by the sweep (each with a red-then-green test):

1. **Pre-existing on main -- partition key mismatch.** `domain.PartitionMatchKey` ("part_3") was
   bound against the generated `partition_key` column ("part 3") in the three completion
   conflict re-reads and the packing reopen. A second operator's submit on any "Part N" pen
   answered 500 forever; the afternoon correction never reopened such a pen.
   `adapters/postgres/partition_key.go` is now the one definition.
2. The photo camera was bound only on the distribution route; a photo slot on packing, wastage
   or transport failed instantly.
3. A corrected resubmit after a server 422 queued behind its own dead-lettered predecessor (the
   outbox lane rule) and never drained; the ViewModels retire that one rejected row.
4. Completion writes accepted a distribution for a feed day not yet reached and a packing for a
   day with no sheet -> `422 feed_day_not_reached` / `409 feed_sheet_not_issued`.
5. Copy: `feedconfig:` / `feeddirection:` package prefixes reached the screen.

## Verifier parity follow-up (2026-09-16, done)

Every feed stage now hands the verifier each capture under its CARD TITLE with the kind it really
is, and the crew's answers as the item's context rows:

- an `either` slot the proof register never judged is left UNKNOWN on the item rather than guessed
  as a video -- the verifier queue asks the register at read time (see
  `context/architecture/verifier-app-and-flow.md`);
- all four bridges map `media_meta` POSITIONALLY through `verificationdomain.BuildMediaMeta`, and a
  blank ref is dropped together with its title (dropping the ref alone shifted every later title);
- distribution and transport hand back the ROW's stored `sop_proofs`/`sop_answers`, so a repair
  retry queues what the crew stored rather than what the retry request carried;
- the legacy Slack import names its three proofs by the seeded distribution card and types each
  from the Slack file's own mime.

## Not done here / follow-ups

- The phone's optional-slot capture and the `either` photo path were unit-tested but not driven on
  the device in this run; the Paparazzi goldens for the distribution and transport capture
  references were re-recorded with the seeded cards.
- The QA clone `goatos_fcqa` on OCI, the API on :8092 and the web on :3392 are throwaway and are
  to be dropped after landing.

## Phase A E2E (2026-09-17): deploy-day parity and what shipped

Maintainer rule: "how it is on stage, keep like that; nothing in daily operations should change
unless someone edits an SOP." Fixes that only restore intended behaviour shipped; rules that would
tighten today's behaviour without an SOP edit did not (below).

Shipped: the scheduled `feed-direction-issue` / `feed-transport-issue` commands pin the published
card; both scheduled lifecycle roots wire the packing store so the 14:00 correction reopens packed
bags; `feed.wastage.completed` passes the envelope schema; an older app is accepted against an
authored card with "Not captured (older app)" rows; packing and wastage re-create a missing verifier
item from the ROW on a retry; the verdict fence (below); the editor's unsaved keys follow the title.

**The verdict fence.** A feed verdict applies only to the submission round its item judged: the row
must still hold `source.evidence_id`, AND no newer verification item may exist for the completion
(payload `item_id`). The second check is what holds when a resubmit names the SAME captures again
(still allowed, see below): each round queues its own item because the enqueue key carries the
completion's `row_version` (distribution / packing / wastage; a bounce, the afternoon reopen and the
resubmit each bump it) or the transport attempt, and verification items are idempotent on that key
alone -- so a same-capture resubmit gets a fresh pending item, and a re-delivered or late verdict of
the earlier item is ignored. Pinned by `TestAStaleVerdictIsIgnoredEvenWhenTheResubmitReusesTheJudgedCapture`
and `TestEveryStageQueuesAFreshItemForAResubmitNamingTheSameCaptures`.

## Recommended, awaiting maintainer approval (not enabled)

Both are implemented in history (commit 5b302507d) and reverted for deploy-day parity. Today's
behaviour is pinned by tests that go red when either is enabled, so enabling one is deliberate.

1. **A rework resubmit must carry NEW captures** (packing, distribution, wastage, transport, and a
   bag the afternoon correction reopened). Today a resubmit may name the very capture the verifier
   rejected, or the video that proves the pre-correction quantity; it goes straight back to the
   verifier as a new item, and the distribution captures read keeps offering the rejected captures
   to every phone as "Proof ready", so a crew can re-send them without re-shooting. Defect it would
   prevent: rejected or superseded evidence re-queued as if it were a re-shoot (seen on the QA clone:
   packing, transport attempt 2 and a reopened bag all accepted their rejected clip). The documented
   transport rule ("every rework requires a new video", feed-transport-verification.md) has no
   executable check today. Pin: `TestReworkResubmitMayNameTheRejectedCaptureAsToday`,
   `TestCapturesReadStillOffersSentBackCapturesAsToday`.
2. **One capture proves one piece of feed work** (one pen-session, one bag, one pen-day, one trip).
   Today a capture already submitted for pen A is accepted for pen B, and a packing video is accepted
   as a distribution video: two verifier items "prove" two pieces of work with one clip. Defect it
   would prevent: the AGENTS.md packing lock "one clip cannot prove two bags" has no executable
   check (seen on the QA clone: pen A's three captures accepted for pen B). Pin:
   `TestACaptureProvingOnePenIsAcceptedForAnotherAsToday`.
