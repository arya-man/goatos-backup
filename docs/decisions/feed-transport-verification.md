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
- **Nor a PARTITION grain (reaffirmed 2026-08-12).** A shed's pens are packed as separate bags and
  fed separately, but they are LOADED AND STAGED as one trip, so transport is one task and one video
  for the whole shed. Migration `000143` fanned the materializer out over `shed_partitions`, and a
  partitioned shed such as Castro started listing `Castro - 1`, `Castro - 2`, `Castro - 3` as three
  transport tasks -- three videos of one load. That was never a recorded decision and contradicted
  both this line and AGENTS.md; `000155_feed_transport_restore_shed_grain.sql` is the forward repair.
  `000143`/`000146` are not amended, because STG records migration checksums.
  - The repair retires only UNSTARTED pen tasks. A pen task already carrying an attempt --
    `verification_due`, `rework`, or `completed` -- keeps its status and its proof, because an
    operator really filmed it and a verifier may already have judged it.
  - `feed_transport_tasks.partition_label` is KEPT and stops being written. Rewriting a filmed row's
    label would make the evidence trail lie about where the operator stood.
  - The uniqueness arbiter is untouched: with `partition_label` always `''`, `000143`'s
    `(tenant_id, business_date, shed_id, COALESCE(NULLIF(btrim(partition_label),''),'whole'))` key
    degenerates to one row per shed per day.
  - The shed FILTER is shed-grain too. Its option id went back to the bare shed UUID from the opaque
    `<shed>\x1f<partition>` composite, and `partition_label` is no longer accepted as a list filter --
    narrowing below the shed could only hide part of one shed's own work.
  - Pen grain belongs to PACKING and DISTRIBUTION, which really are one bag per pen. Do not copy
    their shape back to transport.
  - Pinned by `TestFeedTransportPartitionedShedStillGetsOneShedGrainTaskPageBoundaryExecutionDateParkScopeStatusMatrix`
    (mutation-tested: restoring the `LATERAL shed_partitions` fan-out turns it red with two `Shed A -
    Part N` rows) and `TestFeedTransportShedGrainRestoreRetiresUnstartedPenWorkAndKeepsFilmedEvidence`.
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
