# Proof Business Ack Contract

Status: active guardrail, 2026-09-07.

## Rule

A proof blob reaching storage is not business success.

For any workflow that captures a photo or video and then changes operational
state, the UI's final green/done state must be derived from the business write:
scan capture, animal observation, feed completion, PC Care slot registration,
task submit, or equivalent. The proof upload row is only a dependency.

## Required Shape

1. The phone may upload proof media first and keep the uploaded proof id.
2. The dependent business outbox row must reference that proof upload row or
   proof id and retry independently.
3. If the business write fails after the media upload succeeds, retry the
   business write. Do not re-upload the media just to repair the link.
4. User-facing success copy such as `Video sent`, `Proof sent`, `Done`, or
   `Submitted` must wait for the business write's success or for a server
   read-model that proves the business row carries the proof ref.
5. Upload-only states must use pending copy such as `Video uploaded, saving to
   task...`.
6. Terminal failures must be visible and actionable. A proof or dependent write
   must not sit forever as generic `Uploading proof...` or `Waiting for network`
   once the outbox row is dead-lettered or in conflict.

## Analytics

Every proof-backed business workflow must emit bounded trace fields that connect:

- screen/surface and field key
- animal tag or task/shed/campaign grain where applicable
- local proof row id
- proof upload outbox id
- server proof id once known
- dependent business outbox id
- separate outcome and reason for proof upload enqueue/start/success/failure,
  business register/submit enqueue/success/failure, backend business ACK
  visibility, retry requested/accepted/failed, and terminal failure

## Regression Test

Any change that introduces or modifies a proof-backed workflow must include a
test proving that upload success alone does not render final green. The test may
use either a ViewModel state projection or a pure UI-state helper, but it must
model this case explicitly:

```text
proof upload succeeded, business write missing or failed
```

Expected UI: pending or failed, never final green.

Analytics tests must also prove the two receipts are not confused: proof-upload
success answers "blob reached storage/proof backend"; business ACK answers "the
feature row now references that proof and will count it."

PC Care's 2026-09-07 deworming issue was this exact failure: `proof_artifacts`
had completed GCS videos, but `pc_care_task_animals.video_proof_ref` was missing
for those animal rows, and Android showed `Video sent` from the proof upload row.
