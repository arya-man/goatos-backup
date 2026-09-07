# Proof Business ACK Progress

Last updated: 2026-09-07

## Current Goal

Make PC Care and every future proof-backed workflow follow the same operator truth as robust
vaccination/weighing flows: a proof blob upload is not final business success. Final green must
mean the backend business row references that proof.

## Verified Root Shape

- GCS/proof backend receipt can succeed while the feature-specific business link is still missing.
- For PC Care animal slots, the final backend truth is `pc_care_task_animals.*_proof_ref`.
- For PC Care task proofs and feed/water removal, the final backend truth is
  `pc_care_task_proofs` or `pc_care_removal_pen_proofs`.
- Android must keep showing an intermediate state such as `Video uploaded, saving to task...` until
  that business link is visible.

## Judge Findings

- Vaccination and weighing already reserve final completion for the business write/submit ACK.
- PC Care had the early-green bug: local proof upload `UPLOADED` rendered as `Video sent`.
- Feed/workflow have some optimistic queued wording risks, but no confirmed proof-upload-alone data
  loss path in the read-only audit.
- OCI phone QA seed is backend-safe for `goatos_e2e_*`, but Android Room/outbox stuck states cannot
  be pre-seeded into backend Postgres. Create those by phone actions, then inspect Android Room and
  backend DB together.

## Implemented In This Branch

- PC Care animal slot cards wait for server slot proof ref before final `Video sent`.
- PC Care task-proof cards wait for server task proof ref before final sent/captured state.
- Replacement captures do not go green against an old server proof ref.
- PC Care scan outbox rows drain ahead of heavy proof/register work.
- PC Care scan analytics now includes task, RFID, normalized RFID, outbox id, group key, and
  idempotency key.
- PC Care task-proof registration analytics now includes the link outbox id separately from the
  proof-upload outbox id.
- PC Care now emits terminal proof-upload analytics separately from backend business ACK analytics:
  `pc_care_slot_upload_synced` / `pc_care_task_proof_upload_synced` answer "did the blob upload
  finish?", while `pc_care_slot_business_ack` / `pc_care_task_proof_business_ack` answer "is the
  proof linked to the business row the app is counting?"
- Task-proof upload observers retry registration when task detail or removal pens arrive after
  proof rows, so analytics are not lost on screen reopen/order races.

## Required Test Pattern

Every proof-backed workflow must have a regression test with:

1. proof upload row is synced and has a server proof id,
2. business link row is missing, queued, failed, or backed off,
3. UI does not show final green/sent/completed,
4. retry path retries the small business write without re-recording or re-uploading the media.
5. analytics include separate events for proof upload terminal state, dependent business write
   enqueue/terminal state, backend business ACK visibility, submit enqueue, and submit terminal
   state.

## Subagent Capacity Rule

If a requested judge/subagent spawn fails due to capacity, close completed or old non-critical
agents and retry immediately. Do not stop on agent-capacity while stale agents can be safely closed.
