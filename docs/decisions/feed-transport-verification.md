# Feed Transport daily verification

Maintainer decision (2026-07-29): Feed Transport is a separate Feed workflow.
It does not reuse Feed Distribution or Feed Packing sessions.

Timing correction (2026-08-10): the controlling Feed source requires packed,
diff-corrected feed to be loaded and staged outside sheds by Day N 15:00. The
older 15:30 task-creation rule below is superseded as a deadline: it currently
describes runtime compatibility behavior that creates work after the source
deadline. The target creates and assigns Transport early enough to finish by
15:00, persists the hard deadline, and may use a stricter effective-dated route
cutoff. There is no ordinary lateness grace. See
`task-timing-alerting-violations-and-appeals.md`.

- Compatibility behavior: at 15:30 Asia/Kolkata, current code creates one task
  for each active physical shed under an active park. Replace this with
  pre-deadline materialization/assignment; 15:30 is not the accepted due time.
- Grain: `(tenant_id, business_date, shed_id)`. There is no session, batch, workflow, or consolidation grain.
- An eligible operator opens the shed task and records exactly one fresh in-app-camera video. Gallery/import is not allowed.
- Submit appends a proof attempt and moves the task `due|rework -> verification_due`.
- Generic Verification category `feed_transport` reviews that attempt. Approve moves the task to `completed`.
- Reject marks that attempt rejected and moves the task to `rework`. The task remains assigned to the original operator.
- Rework requires a new live recording and appends a new attempt. Rejected attempts and proof references are never overwritten or deleted.
- The Android task list uses the same filter interaction as Feed Direction, limited to Transport's
  actual grain: business date, Farm, physical shed, and verification status. It has no workflow or
  session filter. Filtering is applied by the backend before keyset pagination, and the response
  returns the actor/date-scoped Farm and shed option vocabulary so Android does not derive options
  from a partial page.

Canonical write owner: `feed_transport_tasks` plus append-only `feed_transport_attempts`. Android reads the bounded task API through Room and sends proof upload + task submit through the offline outbox.
