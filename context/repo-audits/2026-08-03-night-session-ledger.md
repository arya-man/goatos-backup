# Session ledger — 2026-08-03 night, device E2E continuation

Continues `2026-08-03-device-e2e-handover.md`. Baseline `origin/main` = `9e0cf5bbf`.

Every claim below was re-run by the orchestrator. Agent reports alone are not
evidence — this session, 4 separate agents reported green while broken, one
`git reset --hard` destroyed another agent's commit, and one "test" was a
string-containment assertion over the SQL the same agent had just written.

## Working branch

All work sits on one detached branch built from `9e0cf5bbf`, not pushed:

| SHA | What |
|---|---|
| `2db1eb207` | P0 — close the wrong-goat proof-capture window |
| `06ccd3979` | scan-zone copy renders an empty last-scan slot |
| `c7794b7bd` | scan screen survives RFID reader connect/disconnect |
| `66d103b4e` | open vaccination drives roll forward to today |
| `cfaef1934` | reminder fires lost shed/vaccine names and picked them at random |
| `89e708291` | AGENTS.md — phone QA must never use or repoint default ports |
| `9b6e66e18` | rolled drive belongs to today, not to every day after |
| `76623501d` | one permission denial no longer dead-ends to Settings |

(`086ea1e8a` is the superseded banner-only first attempt at the P0; `2db1eb207`
builds on it.)

## FIXED — verified failing-then-passing by the orchestrator

### 1. P0 — proof video filed against the wrong goat (`2db1eb207`)
`ScanViewModel.requestGoatProof(row)`. Scanning goat B while goat A's camera was
still open silently dropped B, and the recording saved under A. The first fix
attempt only surfaced a banner — the camera stayed open, so an operator standing
at B could still record into A. That is a warning, not an interlock, and was
rejected.

Shipped behaviour: a scan of a DIFFERENT goat while `captureVideo()` has not yet
returned CANCELS that camera and reopens bound to the new goat. Once
`captureVideo()` has returned, the capture is non-cancellable and a later scan is
refused instead — a completed clip can never be discarded (this repo has a prior
incident where a fix discarded real field captures). The cancelled job's
`finally` is guarded by goat id so it cannot clear the new capture's state.

Copy: `"Finish the current animal's video first."` / `"<tag> still needs its video."`

Evidence: `tests="24" failures="0"`; revert the production file only →
`failures="3"`.

### 2. Vaccination drive did not roll onto today (`66d103b4e`)
Contract: a drive keeps showing on the CURRENT date until CLOSED.

The handover named two gates. There were **three**, and the one it named as the
blocker was not the blocker. `obligation_drive_membership` (`:774`, `:821`) only
feeds `drive_summary` counts. The event row is materialised in `batch_events`
(`:601`, `:613`, plus a `window_start` variant), which windowed strictly to the
drive's own day — so widening the status allow-list alone would have looked
correct and still returned `events=0`.

A first attempt widened *inclusion* only: the drive came back in a D+1 query
still keyed to D, so it was returned but not surfaced. The shipped fix rolls the
display date (`due_at`/`window_start`/`window_end`) to today's IST midnight while
`severity`/`status` keep reading the ORIGINAL date, so a rolled-forward drive
still reads `warning` rather than laundering itself back to `info`.

Gate is `obligation_batches.status = 'in_progress'` (work started). Verified
against real data: both live batches are `in_progress`, planned `2026-08-02`.

Evidence: red `open verification_pending drive not surfaced under today
(2026-08-03); items=[]string{}` → green. Real Postgres, `SKIP` count 0.

### 3. Rolled drive leaked onto EVERY future day (`9b6e66e18`)
Found by the maintainer on the phone — tapping the 2nd and the 3rd showed the
same cards. Not Room cache; the backend returned it.

`rolled_due_at` snapped to today unconditionally, with no reference to the
queried window, while the widened lower bound admitted the batch into any
window. Fix: a shared `calendarTodayInRequestedWindow` const ANDed into all four
widened bounds, so the rollover only applies when today falls inside `[$2,$3)`.

Verified live against the throwaway DB after the fix:
```
08-01 → 0   08-02 → 0   08-03 → 2   08-04 → 0   08-05 → 0
```
Was 2 on every future day.

### 4. Calendar suite was RED on origin/main (`cfaef1934`)
Invisible because Postgres tests are opt-in and skip silently, so `make ci-local`
stays green over them. Two failures, and they were NOT both fixture rot:

- `...CollapsesMultipleRulesInBatchIntoSingleDrive` — fixture defect. The product
  SQL correctly counts `count(DISTINCT oi.target_id)`; the fixture pointed both
  obligations at the same target. Commit `63834cccc` had deliberately moved this
  off `estimated_targets` (the 200-vs-400 parity fix) and the test was never
  updated.
- `TestReminderCadenceShedVaccineEnrichment` — **three real product bugs** in
  operator notification copy:
  1. Multi-shed drives lost their shed names. The cadence query read the scalar
     `shed_name`, which is NULL BY DESIGN for a multi-shed park-day drive, while
     `detail->'summary'->'shed_labels'` held all of them. So exactly the drives
     that need naming were unnamed.
  2. `dedupeAndSort` did not sort — it ranged a Go map while claiming insertion
     order. Which sheds/vaccines survived the 2-item cap was RANDOM per sweep;
     two pushes about one drive could name different sheds. Failed ~40% of runs.
  3. Enrichment dropped every non-representative candidate, on a premise that is
     false by construction for a park-day fire.

Evidence: full calendar suite `EXIT=0`, all 4 packages `ok`, `SKIP` count 0.
Baseline was 2 failures.

### 5. Scan-zone copy rendered an empty slot (`06ccd3979`)
`"11 scanned · last  · tap to add more"` — `.orEmpty()` on a null timestamp left a
gap between separators. Label is now built from the segments that exist.
No regression test: `feature-submit` has no test sourceset or test deps, and
adding them is a build-config change out of scope for a copy fix. Verified by
compile + full app suite (427 tests, 0 failures, goldens unchanged).

### 6. Scan screen lost on RFID reader connect/disconnect (`c7794b7bd`)
The V1 reader (IDT RHLS-3) is a Bluetooth HID **keyboard**. Every connect and
disconnect changes `Configuration.keyboard`/`hardKeyboardHidden`, and
`MainActivity` declared no `configChanges` — so the platform destroyed and
relaunched the Activity. The relaunch reset the Activity-scoped
`BootstrapViewModel` to `Loading`; on `Ready`, `GoatOsShell` built a FRESH nav
controller at `startDestinationFor(navState)` = `/vaccination` for an operator.
The operator was thrown to the sheds list mid-scan with the back stack gone.

Device evidence, same pid (Activity relaunch, not process death):
```
01:45:04.425 Device added: name='IDT RHLS-3'
01:45:04.881 finishDrawing of relaunch: MainActivity
01:46:19.930 Removed device: IDT RHLS-3 classes=KEYBOARD|ALPHAKEY|CURSOR|EXTERNAL
01:46:20.222 finishDrawing of relaunch: MainActivity
```
Fix: `android:configChanges="keyboard|keyboardHidden|navigation"` (verified as
`0x70` in the installed APK). This also explains the previously unexplained
"RFID banner flipped to Reader disconnected" during a real drive.

**Not confirmed on device** — mechanism- and log-proven only.

### 7. One permission denial dead-ended to Settings (`76623501d`)
Worse than the initial hypothesis, and NOT OEM-specific. The blocking screen is
`RoleBasedPermissionGate`, which **never called `PermissionGrantResolver`** — it
imported it and used its own inline copy with no "have we asked yet" guard,
inside `LifecycleEventEffect(ON_RESUME)`, which fires on FIRST composition. For a
never-asked permission `shouldShowRationale` is `false` by definition, so every
missing permission was classified permanently blocked before anything was ever
requested. Zero denials required; reproduces on stock Android.

Confirmed by device flags: no `USER_FIXED` on any permission — the OS never
considered them denied, only the app did.

Same file: the launcher callback overwrote the granted set instead of merging, so
granting the last permission discarded earlier ones and the gate could never
dismiss.

Fix counts requests, believes "blocked" only after 2 real asks, keeps the
Settings fallback for genuinely blocked. Evidence: `PermissionGrantResolverTest`
red `expected:<DENIED> but was:<PERMANENTLY_DENIED>` → `tests="7" failures="0"`.

**Not reproduced on a Xiaomi device**; no on-device verification of the fixed flow.

## PENDING

### P1 — leadership/operator counts do not reconcile (agent in flight)
One grain defect, several surfaces:

- **CEO Command Board buckets do not cover targets.** `targets: 40`,
  `awaitingVerification: 20`, `dosesVerified: 0`, `overdueNotGiven: 0`,
  `scheduledAhead: 0` → buckets sum to 20. Amit's 20 untouched CPT animals are in
  NO bucket. A CEO reads "on track" while half the animals have had nothing done.
  They are not overdue because the drive window runs to `2026-08-05`; the gap is
  that no bucket means "not started, still in window".
- **`review_count` is a 0/1 FLAG, not a count** — `canonical_read.go:1259`
  `'review_count', CASE WHEN grouped.has_review THEN 1 ELSE 0 END`. Same class as
  ledger finding 10 ("captured totals are a boolean", fixed for admin-web in
  `2e497a092`); this is a second site. **Pre-existing on `9e0cf5bbf`.**
- **`review_count` contradicts the card's own status.** `:1088` sets status
  `verification_pending` when `has_review OR submitted_count > 0`, but
  `review_count` reads `has_review` alone. The batch is `in_progress`, so status
  came from `submitted_count` while `review_count` fell to 0 — hence a card
  reading `verification_pending` and `0 of 4 sheds done` at once, for a drive
  where all 4 sheds were submitted. **Pre-existing.**
- **`scheduled_count` counts submitted work as still scheduled** — the FILTER has
  always included `verification_pending`. **Pre-existing by design**, badly named.
- **`shed_labels` carries the PROTOCOL name, not shed names** — `shed_count: 4`
  but `shed_labels: ["Per Animal Proof Vaccination QA"]`. Same wrong-source class
  as the reminder fix in `cfaef1934`, different site (calendar read path).

A bisect agent is confirming each field commit-by-commit against the real DB
rather than trusting the SQL read above.

### P2 — blocks `make land-main`
- **`make aggregate-projection-guard` FAILS on this branch.** Inherited from
  `66d103b4e`'s hunk, which demands five adversarial tests (`OneToMany`,
  `PageBoundary`, `DateShift`, `ScopeHierarchy`, `StatusMatrix`) it never got.
  Proven not to come from later commits:
  `AGGREGATE_PROJECTION_BASE=$(git rev-parse HEAD) node
  tools/agent-hooks/check-aggregate-projection-review.mjs` → PASS.
- **Nothing on this branch has been run together.** Each fix was verified in
  isolation; the calendar cherry-pick auto-merged a test file. Full `ci-local` on
  the combined SHA has not run.

### P3 — pre-existing, confirmed identical on `9e0cf5bbf`
- `internal/kernelstages`: 3 failures (`TestReminderCadenceP1Finding1NoRecipientRetryable`,
  `...Finding2DrainWithRecipients`, `...StageQueuesNotificationsAtEachLadderSlot`).
- `core-permissions/AppPermissionTest`: 3 failures. One asserts **"catalog never
  requests bluetooth scan or location (V1 avoids in-app discovery)"** — but the
  app DOES request `BLUETOOTH_SCAN`, which is what put the operator into the
  permission gate at all. Either the catalog gained a permission it should not
  have, or the test is stale. Worth a ruling.
- `feature-auth` declares NO test dependencies, so `RoleBasedPermissionGateTest.kt`
  has never compiled or run.
- `stableSameLocalDayDueAt` (`repository_integration_test.go:2393`) is `now()+2h`
  hour arithmetic instead of a `biztime.BusinessDayStart` anchor — violates the
  business-day-grain rule and is shared by many tests.

### P4 — awaiting a maintainer ruling, not code
- **Day strip design.** With rollover, an open drive renders on TODAY only; it
  vanishes from its own scheduled date and leaves no trace there. The maintainer
  observed that a `-1` day tab is then pointless. Either (a) rollover +
  today-forward only, dropping past tabs, or (b) no rollover — the card stays on
  its scheduled date and overdue-ness shows as status/severity. Current state is
  (a) without the tab change.
- **`DONE` counts an animal with no proof.** `scanRosterSQL` marks `done` when a
  scan capture exists, before proof; the shed-summary contract
  (`vaccinationexecution/domain/types.go:566-576`) treats "recorded, proof
  pending" as still `Due`. Two backend contracts disagree about "done".
- **`scheduledAhead` semantics** — currently 0 while 20 animals are plainly
  scheduled ahead.

### P5 — E2E not run
- **Weighing (Dinakar, growth_director)** — data-blocked. `weighing_campaigns=2`
  (both `draft`), `weighing_campaign_sheds=8`, but `weighing_work_items=0`,
  `weighing_expected_animals=0`, `weighing_observations=0`. Findings 6/7/8/9/10/
  11/13/NEW-5/6/8, the privesc port and item 4 are all weighing, and it still has
  zero device coverage. Getting real coverage needs seeded weighing data; the seed
  is not idempotent and Pramod's sheds are in that database.
- **Verifier approve → outbox → FCM.** INPUT IS READY: `vaccination_proof |
  pending | 4` (Pramod's 4 CBE sheds). Never driven.
- **pc_director (Chandrakant)**, **second-operator park scoping**, **leadership
  close of a drive**, **reader-disconnect during an active scan session.**

## Environment (as left)

```
:3300  admin-web            MAINTAINER'S — never touch
:8080  → 5433/goatos        MAINTAINER'S local stg replica — never touch
:8081  → 15544 (goatos-phone-qa)   phone QA, mock scan data
adb reverse: device:8080 → host:8081   on both phones
```

The port rule is now committed in `AGENTS.md` (`89e708291`). It supersedes the
old phone-QA wording, which read as permission to take `8080` and caused a real
incident: the maintainer's `5433`-backed API was killed to free the port, `8080`
was repointed at `15544`, and admin-web silently showed the 20-animal mock set in
place of the 324 CPT adults.

Phones: POCO X5 Pro 5G `dd861eff` (operator Amit, task
`92000000-0000-4000-8000-000000000712`), Infinix GT 30 Pro `143382555G111292`.
Amit's 5 wrong-goat scan captures + 5 proofs were dumped to
`amit-wrong-goat-evidence.tsv` before clearing so he could rescan; that dump is
the only stored reproduction of the original corruption and belongs in a fixture.
Pramod's task `91000000-...-702` was left untouched: 20 captures, 20 proofs,
4 pending verifications.

## Standing lessons re-confirmed this session

- Re-run every agent claim. 4 of this session's agents reported green while
  broken; one committed a test that asserted the SQL string it had just written.
- Never pipe `make ci-local` or Gradle through `tail` — the exit code becomes the
  pipe's. Gradle `BUILD SUCCESSFUL in 985ms` with no test execution is a FALSE
  GREEN; check the test-results XML attributes AND its timestamp.
- Postgres tests skip SILENTLY without `GOATOS_RUN_POSTGRES_TESTS=1`. A PASS
  containing `--- SKIP` is not evidence. This is why main's calendar suite sat red.
- Agents must never `git reset --hard` in a shared checkout. One did, and
  destroyed a verified commit; it was recovered from the object DB only because
  the SHA was still in the reflog.
- One agent per worktree. Two agents editing the same test file broke compilation
  twice and produced two rival commits for the same fix.

---

# Round 2 — after Amit's real-device re-run (2026-08-03 ~03:21 IST)

## P0 CONFIRMED FIXED IN THE FIELD

Amit redid the full CPT drive on the fixed build. Orchestrator-verified:

```sql
-- each proof vs the nearest PRECEDING scan
20 MATCH / 0 MISMATCH
```
Perfectly alternating SCAN→PROOF ×20, lag 2.9-7.6s, 20 distinct subjects, 20
distinct `content_hash` and `object_key` (no video reused across goats). Before
the fix the same task produced 5 proofs on 2 goats.

Also clean: 20 tags → 20 goats 1:1, shed prefixes correct (`C1-`→Castro 1),
20/20 `sop_task_scan_attempts` accepted — **no evidence of the reported "tag
reading not proper" in this run**. 4 submissions × 5 items, zero orphans, zero
cross-shed contamination in PERSISTED items, Pramod's CBE data untouched.

## WEIGHING HAS THE SAME RACE — WORSE. FIXED (`054cf20ce`)

`WeighingViewModel.matchTag()` had the identical bare `return` behind
`actionInFlight`, reached from a hot RFID Flow collector, with the row bound in
the launched closure. Two consequences, one worse than vaccination:

1. Video shot at animal B saved under animal A (same as vaccination).
2. **The WEIGHT too** — the dropped scan left `selectedRow` on A, so a weight
   typed while standing at B was recorded against A. Vaccination had no
   equivalent.

**Free-flow makes this WORSE, not safe.** `backend/internal/weighing/app/service.go:419`
clears `cmd.AnimalID` and takes the phone's `scanned_identifier` verbatim, with
no re-resolution and no roster cross-check — the client's binding IS the record
of truth. So the failure is a weight under the wrong SCANNED TAG:
indistinguishable downstream and unfalsifiable server-side.

Orchestrator-verified red→green: revert → `tests=17 failures=2`
(`the weight belongs to the animal the operator is standing at, expected …0408
was …0407`); apply → `tests=17 failures=0`. Full app suite 430 tests, 0 failures.

Shed-partition weighing is NOT affected (subject is the shed, no per-animal
binding). Same-shape surfaces named but NOT audited: `ShiftingExecuteViewModel`,
`FeedDistributionCompleteViewModel`, `FeedPackingCompleteViewModel`,
`FeedTransportViewModel`, `AddBirthViewModel`, `AddDeathViewModel`,
`CountsViewModel`.

## NEW P0 — cross-shed media leak into the verification queue (PRE-EXISTING)

Orchestrator-verified:
```
Castro 1  · 5 goats   5 videos   5 in-shed    0 foreign
Castro 2  · 5 goats  10 videos   5 in-shed    5 foreign
Castro 3  · 5 goats  15 videos   5 in-shed   10 foreign
Mandela 2 · 5 goats  20 videos   5 in-shed   15 foreign
```
The phone posts a CUMULATIVE payload (by shed 4 it re-sends all 20 goat_ids and
20 proof_refs) and `verification_items.media_refs` copies it verbatim. The shed
filter that correctly protects `sop_submission_items` was never applied to
`media_refs`. A verifier opening Mandela 2 reviews 20 videos, 15 from other
sheds. Pramod's CBE items show the same 5/10/15/20, so this predates tonight.
Root cause is the client's O(n^2) accumulating payload; the server-side item
filter is the ONLY thing preventing a real leak and must never be weakened.

## NEW P0 — 20 proofs marked `completed` with no bytes on disk

All 40 `proof_artifacts` say `upload_state='completed'`, but Pramod's 20 CBE
objects are not on disk. Every CBE signed download returns HTTP 500
(`no such file or directory`); Amit's 20 CPT return 200 `video/mp4`. The Infinix
hit this live at 03:15:19 — Jyothi opened Gandhi 1 and the player failed.
Two defects: the completion write does not verify the object landed, and a
missing object surfaces as a generic 500 instead of 404/410, while the queue
still reports `evidence_available: true`.

## NEW P1 — Android derives "Overdue" itself and ignores submission

Every BACKEND surface is correct (`workState: verification_pending`,
`severity: watch`, `summary.overdue: 0`). The CEO app still paints a red
**Overdue** chip beside **In review**.
`ShedsViewModel.kt:572 isOverdueWork()` falls through to a bare
`scheduleDate < today` comparison with NO submission term.
`ExecutionDto.kt:60-66` documents that the contract exposes only the original
`dueDate` and that assignment-aware fields must be added before Android consumes
them — Android consumed it anyway. Affects CBE too (live since the 08-02→08-03
midnight rollover); Amit's submission exposed it on a second park rather than
causing it. Fix belongs in the client: render backend `workState`/`severity`
verbatim instead of re-deriving.

## NEW MEDIUM findings

- `remaining_count` (= `total - completed`) contradicts `progress_pct: 100` on
  the SAME payload after the progress-semantics decision. `remaining_count` is
  the stale field.
- `packages/api-client/src/generated/app-api.ts:4072` still documents the OLD
  rule ("numerator counts VERIFIED completion only"). Code is right, contract
  text is wrong — clients are told to trust the text.
- Command-board `cohortMatrix` is blind to submission (`pending_count` keys off
  obligation status alone, which advances only on verification). One page shows
  "40 awaiting verification" above "40 pending" with nothing to reconcile them.
- `goat_identifiers.normalized_value` stores UN-normalized values while
  `sop_task_scan_captures.normalized_tag` is normalized; a join on the two
  returns 0 rows.
- `/version` reports `build_sha: "unknown"`, so binary-vs-source drift is
  invisible — a stale :8081 binary served `[verify, alerts]` for 13 minutes
  after the `you` restoration was committed, and nothing flagged it.

## CONFIRMED HOLDING after the data-shape change

- KPI buckets still partition targets: `0 + 40 + 0 + 0 = 40`.
- Calendar 5-bucket invariant per park: `20 = 0 + 20 + 0 + 0 + 0`.
- Day placement across 08-01..08-06: `0,0,2,0,0,0` — no rollover leak.
- CPT moved `overdue` → `verification_pending`, ring 0% → 100%, sheds 0/4 → 4/4.
- Verifier queue: 8 pending (4 CBE + 4 CPT), API matches DB row-for-row, real
  shed names, tenant scope honoured.
