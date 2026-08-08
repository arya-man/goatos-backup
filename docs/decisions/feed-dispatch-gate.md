# Feed dispatch gate: no rows before the clock, frozen the moment there are

**Maintainer decision, 2026-08-08.** SUPERSEDES the 2026-07-20 serve-path rule *"whenever I ask
for feed data by date, generate the feed for that date"* for feed direction and feed packing.

## The rule

A feed day's sheet has exactly two visible states, decided by its workflow's dispatch clock in
`feed_schedule_config` (per park x workflow):

```
                 before direction_time        at/after direction_time
normal (07:00)   no rows, "arrives 07:00"     generated ONCE, FROZEN, served frozen forever
experiment (14:00) no rows, "arrives 14:00"   same
```

Times are `direction_time` on **D-1**, in `Asia/Kolkata` — feed for day D is produced the day
before. Both parks currently carry normal 07:00 / experiment 14:00.

Feed **packing** has no clock of its own. It reads the SAME frozen rows as feed direction
(`loadServedRows`) and derives its bags from them, so freezing the direction sheet freezes the
packing worklist with it. "Normal packing freezes at 07:00, experiment at 14:00" is therefore one
mechanism, not two — do not add a second gate for packing.

## Why the previous behaviour was wrong

The 2026-07-20 preview generated the sheet live on EVERY read. Nothing was stored, so every read
recomputed from the current herd, and the projected shed count includes authorized-but-unexecuted
shiftings (2026-07-27). A crew that packed 40 bags at 08:00 would see different kilograms at 10:00
because a shifting was approved in between — the numbers moved under work that was already done.
The maintainer's words: *"once generated rows should not change based on shiftings bcz that will be
a mismatch."*

Hiding the day before its clock is the same decision seen from the other side: a number that is not
yet frozen is not yet real, so it should not be on a screen at all.

## What did NOT change

- **Draft** (`draft=true`, the config-authoring what-if) still live-computes an un-issued day. It
  branches before the serve path. It is now the ONLY path that does.
- **The horizon guard runs FIRST and still wins.** A day outside `[today, tomorrow]` is never
  freeze-on-read. Freezing a past day would stamp today's herd onto a day whose real counts are
  gone — and unlike the drifting preview, that fabrication would then be permanent.
- **An already-issued sheet** for any date still serves its frozen rows, horizon or not.

## Properties a future change must preserve

- **The freeze is stamped at the SCHEDULED instant, not at first-read time.** The sheet reads
  "issued 07:00" whether the first person opened it at 07:01 or 09:30, so the audit trail does not
  record whoever happened to look first.
- **Concurrent first-reads need no lock.** `PersistIssue` is idempotent on
  `(tenant, park, feed_day, workflow)`, so a racing second freeze is an exact replay.
- **The gate is per WORKFLOW, never per park.** At 08:00 normal is frozen and visible while
  experiment is still hidden with its 14:00 arrival time. A park-keyed gate leaks experiment early
  or hides normal late; both are invisible in a single-workflow fixture, which is why
  `TestPackingFreezesNormalAtSevenAndExperimentAtTwo` carries two clocks. Both failure modes were
  mutation-tested when this shipped.
- **The empty state must carry the backend sentence.** `lifecycle.message` names when the sheet
  arrives; Android binds it (`FeedLifecycleDto.message`) and both feed ViewModels prefer it over the
  generic "nothing to pack". Without it a gated morning reads as a fault rather than as "not yet".

## There is still no scheduler

Nothing runs at 07:00 or 14:00. The freeze happens on the FIRST READ after the clock, so an
unopened day is never frozen at all. Consequences worth knowing:

- If nobody opens the screen until 09:30, the sheet freezes at 09:30 **using the herd as of 09:30**,
  while its `issued_at` records 07:00. Shiftings approved between 07:00 and 09:30 are therefore
  inside the frozen numbers.
- A true 07:00 freeze needs the scheduler (`feed-direction-issue -action issue`). The Terraform for
  it is blocked on `check-deployed-job-flags.mjs` keying the manifest by binary
  (`manifestJobsByBinary`), which cannot express three jobs — `issue` / `amend` / `lock` — on one
  binary. The simple fix is an `auto` action that reads the clock and picks the action itself, so
  one job on one tick replaces three exact-time jobs.

## Code

- `backend/internal/feeddirection/app/lifecycle_gate.go` — the gate
- `backend/internal/feeddirection/app/lifecycle.go` — `servePreview` / `servePacking` apply it
- `contracts/openapi/app-api.yaml` — `FeedDirectionLifecycle.state` documents the states
- Tests: `TestServeBeforeDispatchClockShowsNoRowsAndNamesTheArrivalTime`,
  `TestServeAfterDispatchClockFreezesOnFirstReadAndNeverRecomputes`,
  `TestPackingFreezesNormalAtSevenAndExperimentAtTwo`
