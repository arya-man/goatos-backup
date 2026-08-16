---
name: feed-proof-flow
description: Use when building, reviewing, or testing ANY feed-chain surface (feed direction, packing, transport, distribution) — backend feeddirection, mobile feature-feed/capture screens, proof processing, or their tests. Encodes the feed proof collaboration contract (shared parallel shed-session, peer operators, free slot split), the media invariant (compressed + overlay always), per-stage grains (packing per-bag, transport shed-grain), and the multi-device testing protocol. Machine gate - make feed-proof-collaboration-guard.
---

# Feed Proof Flow

Canonical contract: `docs/product/feed-proof-collaboration.md`. If code, a doc,
or your plan disagrees with it, the contract wins. The superseded "sequential
capture" wording in `docs/decisions/feed-distribution-verification.md` must
never be resurrected.

## The collaboration contract (distribution)

- One shed-session (shed/partition × session × date) = ONE shared workspace,
  mirrored to every operator of that park under their own login.
- Three proof slots: feed weight PHOTO, feed distribution VIDEO, water
  distribution VIDEO. All camera-only (no gallery/import — the weight photo
  carries a scale NUMBER; a gallery still is a reading from another day).
- **Slots are independent and PARALLEL.** No slot gates another. Three phones
  capture simultaneously into the same session. Never add "step unlocks next
  step" logic — `FeedDistributionSequenceTest` pins this; the guard enforces it.
- **Free slot split.** One operator may fill all three; three may take one each;
  any 2/1 split is valid. Never hardcode role-to-slot.
- Attribution is per-proof ("Captured by X · time"). Submit is session-level by
  any operator once all slots fill; lock (`pending_verification`) propagates to
  every phone; duplicate submits are idempotent. Review is verifier-only —
  operators are peers, there is no peer review.

## The media invariant (all feed stages + all proof capture)

Photos AND videos upload compressed + overlay-burned (timestamp, operator,
context, location). A raw original reaching the server (`upload_original=true`,
`proof_processing_failed` in logcat) is ALWAYS a defect — the fallback masking
it is not a pass. Three field bugs came from mime-blind validation (video
duration probe on JPEG, `file:/` single-slash URI mangling, mime-blind Gate-3):

- Gate-3 backstop in `CaptureRepository` must branch `image/*` →
  `validateImageFile`; processed validation must pass the output mime type.
- `AppProofMediaProcessor` must keep overlay burn + JPEG compression on the
  photo path and overlay composition on the video path.
- Pinned regression tests (guard fails if deleted):
  `ProcessedPhotoValidatorRegressionTest`, `FeedDistributionSequenceTest`, and
  the gate-3 real-JPEG test in `CaptureRepositoryTest`.
- Tests for proof media must always assert compressed + overlay present
  (maintainer verbatim rule).

## Per-stage grains (do not blur them)

- Direction: frozen per shed×partition×session×date; never live-computed reads.
- Packing: per BAG per session (Morning/Evening = two bags, two videos); one
  mandatory live video; own verification + rework loop.
- Transport: SHED-grain, never partition-grain — one trip/video per physical
  shed; append-only attempts; due→verification_due→completed/rework.
- Distribution: the 3-slot shared session above; verifier judges the proof SET
  on one verification item.

## Testing protocol (multi-device, mandatory for device claims)

- Multiple real phones, each logged in as a DIFFERENT operator, same session.
- Vary splits across sheds: 3-way, single-fills-all, 2/1.
- After EVERY upload assert cross-phone reflection both directions (thumbnail +
  attribution), back/re-enter persistence, and process-death survival.
- Submit-lock must propagate to all phones without app restart.
- Server truth over screen truth: completion row with all proof refs, pulled
  artifact visually shows the overlay, size confirms compression.
- Never claim a device fix without driving the exact scenario on a real phone.

## Machine gate

`make feed-proof-collaboration-guard` — fails on sequential slot gating,
mime-blind validation, dropped overlay/compression, or deleted regression
tests. Runs in the CI aggregate.
