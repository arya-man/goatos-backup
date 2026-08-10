# Weighing Rework Task Cards

Date: 2026-08-10

When an operator submits weighing videos, the weighing bucket is considered
submitted while verifier review may still be pending. A later schedule for the
same shed is a separate campaign/bucket, so the operator can legitimately see
two cards for the same shed: one fresh weighing task and one prior-day rework.

If the verifier sends a weighing video back, the rejected observation is marked
`rework` and the owning bucket is reopened for the operator. The card must make
that prior-day work visually distinct:

- Normal due work shows the current due business date and the normal scan action.
- Submitted work shows the date and that verifier review is pending.
- Rework shows a "Sent back" status, the original planned business date, the
  latest verifier reason when present, and a redo action.

The UI must not block tomorrow's scheduling only because verifier review is
pending for today's videos. The safety requirement is clarity: a same-shed
rework card must not look identical to a fresh same-shed weighing card.
