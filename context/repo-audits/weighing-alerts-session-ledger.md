# Weighing / Alerts Session Ledger (2026-08-01)

> State of play (30-second read): this session made two binding maintainer decisions on
> module ownership and weighing-planning authority (now in `AGENTS.md`, "Confirmed module
> ownership and weighing planning authority" block — do not restate it here, read it there).
> Working from those decisions, the session found and started fixing the reason alerts don't
> work at all: **`position_module_duties` has 0 rows and `notification_requests` has 0
> rows** (verified live against the phone-QA DB below) — every verifier push for every
> module, not just weighing, currently resolves to zero devices. A fix (`deriveVerifierDuties`
> + `assertVerifyDutyCoverage` in `backend/cmd/seed-position-duties/main.go`) is written and
> present in the working tree but **UNCOMMITTED** and **not run against a DB to confirm it
> populates the table** — do not assume it works, run it and re-check the count first.
> Two audits ran this session; their confirmed findings are not yet all fixed. See §3 for the
> open list and §5 for what could not be verified.

## 1. Repo state at write time

- Repo root: `/Users/ravi/mesha/goatos-main-qa`
- `git status --short`: 112 changed paths, all **uncommitted** (no commits made this
  session covering this work — `git log --oneline -15` shows only prior sessions' commits,
  the newest being `999b2c617 feat: weighing leadership surface`).
- `git diff --stat`: 78 files, +7396/-2671.
- `backend` builds clean: `cd backend && go build -buildvcs=false ./...` — no output, exit 0.
- Do not push or commit anything from this ledger; that was explicitly out of scope for
  writing it.

## 2. Maintainer decisions (2026-08-01) — pointer only

Full text lives in `AGENTS.md` under **"Confirmed module ownership and weighing planning
authority (maintainer decision 2026-08-01)"**. Summary for orientation, not a substitute:

- Vaccination → `pc_director`. Weighing → `growth_director`. Feed → `feed_director`.
  Counts → `health_director` (distinct from `pc_director`).
- Planning a weighing task is CEO-only; `growth_director` monitors/executes but does not
  raise it. `/app/weighing/planner/catalog` and
  `/app/weighing/planner/parks/{park_id}/buckets` carry `weighing.plan` despite being GETs
  — verified at `backend/internal/permissions/routes.go:206,208` and
  `backend/internal/permissions/permissions.go:86`.
- Counts ownership is a maintainer EXTENSION, not a handbook fact — no counting
  department/handbook exists anywhere in source. Recorded honestly in `AGENTS.md`, not
  restated here.
- **Counts stays an OFF feature.** `health_director` gets ownership (notification
  recipient) but not `counts.read`/`counts.write` — verified those two permission
  constants exist at `backend/internal/permissions/permissions.go:123,140` and that
  `ceo_internal` already holds `counts.read` (comment at line 452); granting them to
  `health_director` would flip `moduleStatusAvailable` (defined
  `backend/internal/workforce/app/bootstrap_copy.go:58`) from dormant to live. An agent
  granted these once this session by mistake; it was reverted. If you see
  `health_director` with `counts.read`/`counts.write` again, that is the same mistake
  recurring, not a deliberate change.
- Feature flags/capabilities need to become DB-tweakable (no app release) — **not started**
  this session; still the hardcoded Go map at `bootstrap_copy.go`. Treat as open work, not
  landed.
- Alerts/FCM must be feature-scoped AND role-scoped, never mixed — this is the design
  intent behind `pendingModuleProfiles` in
  `backend/internal/notificationbridge/verification_notify_consumer.go:102-152` (one
  profile per module, each with its own wording, leadership position, and tap route).

## 3. The load-bearing finding: verify-duty coverage is empty everywhere

**Verified live**, `psql postgres://postgres:goatos@127.0.0.1:15544/goatos`:
```
select count(*) from position_module_duties;   -- 0
select count(*) from notification_requests;     -- 0
```
Root cause, confirmed by reading the code (not the session's prior summary):

- `backend/cmd/seed-position-duties/main.go:282-284` (`deriveDuties`) emits only
  `"execute"` and `"manage"` duty rows.
- `ResolveModuleDutyRecipients` (`workforce/adapters/postgres/roster_repository.go`,
  referenced at `main.go:96-100`) joins on `duty_type = 'verify'`.
- Net effect: with zero `'verify'` rows ever seeded, **every** verifier push for
  **every** module (vaccination included, not just weighing) resolves to zero devices —
  a notification-fanout E2E test would pass while delivering nothing, because it never
  gets far enough to fail.
- The module-code trap is real and confirmed: `moduleVaccination = "pc.vaccination"`
  (`backend/internal/notificationbridge/verification_notify.go:48`) while the module's
  own name-ish string elsewhere is `legacyVaccinationSourceModule = "vaccination"`
  (`verification_notify_consumer.go:51`) — a seeder or profile keyed on the wrong one
  fails silently, no error, no log.

**A fix is already written in the uncommitted diff, not yet proven:**
- `backend/cmd/seed-position-duties/main.go` adds `deriveVerifierDuties` (derives
  `'verify'` rows for the `video_verifier` seat, one per module returned by
  `notificationbridge.PendingNotificationDutyModules()` — deliberately not a
  hand-copied list, per the comment at `verification_notify_consumer.go:156-165`, because
  a hand-copied list is exactly how this gap was created) and
  `assertVerifyDutyCoverage` (seed closeout: fails loudly if any notified module has zero
  active verify-duty holders, `main.go:339-363`).
- `main_test.go:63-64` asserts every module in `PendingNotificationDutyModules()` maps to
  `dutyTypeVerify`.
- **Not verified this session**: whether running this seeder against the phone-QA DB
  actually brings `position_module_duties` off zero. The DB check above was run BEFORE
  confirming the fix executes — re-run `go run ./backend/cmd/seed-position-duties` (or
  whatever the Makefile target is) and re-check the count before trusting this is closed.

## 4. Landed and verified this session (uncommitted, in the working tree)

Every item below was checked against the actual file, not assumed from a prior summary.

| Item | Verified at | Note |
|---|---|---|
| Design-system token guard | `tools/agent-hooks/check-design-system-tokens.mjs` (new, untracked) + `tools/agent-hooks/design-system-baseline.txt` | Baseline currently holds **23 entries**, not "~65" — the "135→~65" figure in the session's own framing does not match the file on disk; treat that count as unverified/wrong and use 23 as current truth. Guard fails on new drift outside the baseline (`--write-baseline` regenerates it). |
| `MeshaType.pillStrong` token | `apps/goatos-android/core/core-designsystem/.../theme/MeshaType.kt:53` | `TextStyle(fontSize = 12.sp, fontWeight = FontWeight.W800)` — exists. Whether it "closes a real design-system gap" is a design judgment, not independently checkable from the diff alone. |
| Gradle orphan reaper | `tools/agent-hooks/reap-stale-gradle-workers.sh` (new) wired at `tools/ci/run-local-ci.sh:299` (`bash tools/agent-hooks/reap-stale-gradle-workers.sh \|\| true`) | Wiring confirmed; not executed this session (Gradle lock held by other agents — do not run it either). |
| Park-scope truncation fix | `backend/internal/platform/httpmiddleware/auth.go:475-487` | Comment at 475 states the old bug plainly ("answering for parkIDs[0] would show a director half their herd and no error"); returns `park_selection_required` (line 483) instead. Confirmed the fix is in the file, not just described. |
| Weighing park resolution hoisted before the observation write | `backend/internal/weighing/app/service.go:358-372` (and again ~403-409 for the second write path) | Comment: "Resolve the notification routing park BEFORE persisting... a write that would silently drop a park id... is rejected before anything is written." Confirmed present in both call sites checked. |
| Silent vaccination fallback removed | `backend/internal/notificationbridge/verification_notify_consumer.go:102-152` (`pendingModuleProfiles`), each module keyed and worded separately; no default-case fallback observed in the file | Consistent with the AGENTS.md line: "an unclaimed module notifies nobody loudly rather than the wrong people quietly, which is how weighing proofs reached the vaccination verifier and PC Director in vaccination wording." New coverage exists at `main_test.go` (see §3) but the parent claim of dedicated fallback-removal tests in `verification_notify_consumer` itself was not individually re-verified beyond confirming the map has no fallback branch. |
| Dev flavor `google-services.json` → real project | `git diff apps/goatos-android/app/src/dev/google-services.json` | Confirmed: `project_id` changed `goatos-placeholder` → `goatos-stg`, `storage_bucket` → `goatos-stg.firebasestorage.app`. |
| Notification fan-out test harness | `tools/local/assert-notification-fanout.sh` (new) | Confirmed present with `since` / `watch` / `expect <spec>` modes, reading `DATABASE_URL` default `postgres://postgres:goatos@127.0.0.1:15544/goatos`, asserting against `notification_requests` joined to `workforce_members`. See §6 for the two-layer test doctrine this backs. |

## 5. What could not be verified — say so, don't assert it

- **The 32 open tracked tasks** the session referenced: not independently reconstructable
  from this repo/session context alone (no task-list file found under
  `context/repo-audits/` or elsewhere holding that exact count). Do not assume 32 is
  accurate; the next session should pull it from wherever the task list actually lives
  (task tool state, not a repo file) or rebuild the open list from `git status` + the two
  audits below.
- **"23 of 44 candidates confirmed" (segregation audit) and "27 confirmed, 10 blocking
  E2E" (deep audit)**: no artifact for either audit was found in
  `context/repo-audits/` under this name; the closest existing files are
  `weighing-implementation-do-not-reopen-ledger.md` and
  `weighing-phase1-2-do-not-merge-blockers.md`, neither of which matches these exact
  counts. **Could not verify these two audits exist as discrete, countable artifacts in
  this repo** — if they were produced only in the coordinating session's transcript and
  never written to a file, they are not durable and will not survive to the next session.
  Recommend the next session locate or recreate them as files before trusting the counts.
- **QA users' exact roles/scopes** and the **9-person STG login profile** mentioned as
  "environment facts": not re-derived from this repo; the STG/9-person facts belong to
  prior sessions' memory (`goatos-stg-9person-login-grants`, `goatos-stg-leadership-login-profile`
  in MEMORY.md), not this session's diff — do not conflate them with the throwaway
  phone-QA DB below.
- **Design-system baseline "135→~65" reduction**: contradicted by direct inspection (23
  lines in `design-system-baseline.txt` right now). Recorded as wrong in §4, not restated
  here.
- **ADC Drive/Docs/Sheets scope grant and Drive-quota-warning**: outside repo scope,
  unverifiable from files; carried forward only as a note, not a repo fact.

## 6. How to test alerts — the two-layer doctrine (record once, don't re-derive)

1. **Layer 1 — recorded decision.** `notification_requests` is written BEFORE any FCM
   call (see the header comment in `tools/local/assert-notification-fanout.sh`), so it is
   the backend's durable decision: who, channel, wording, tap route. Assert on it with
   `tools/local/assert-notification-fanout.sh expect <spec-file>`, spec lines are
   `must|mustnot|route <person> <type-substring> [<route-substring>]`. This separates "we
   never decided to tell anyone" (routing bug) from "we decided but delivery failed"
   (transport bug) — the two look identical from a phone.
2. **Layer 2 — real delivery.** Prove pushes actually land using the physical phone plus
   multiple emulator instances of the SAME AVD started `-read-only` (permits concurrent
   instances), each signed in as a different role AT THE SAME TIME. Switching accounts on
   one device cannot observe another role's push — by the time you re-log-in as the
   director, the notification already fired or was dropped. Concurrency is not optional
   for this layer.

Both layers matter because Layer 1 can be green (rows exist, correctly addressed) while
Layer 2 is still broken (bad FCM token, credential, or transport), and vice versa — right
now Layer 1 is failing outright (0 rows, §3), so Layer 2 cannot even be attempted
meaningfully until the seeder fix in §3 is proven.

## 7. Environment facts worth preserving

- Throwaway phone-QA Postgres: container `goatos-phone-qa`, port **15544**,
  `postgres://postgres:goatos@127.0.0.1:15544/goatos?sslmode=disable` (confirmed live —
  used for the row-count checks in §3). Reset-first: re-running the seed against an
  already-seeded DB fails on an immutable published protocol — drop/recreate the
  container rather than re-seed in place.
- `apps/goatos-android/app/src/dev/google-services.json` now points at the real
  `goatos-stg` Firebase project (§4) — anything that previously worked against
  `goatos-placeholder` (nothing should have) is no longer applicable.

## 8a. Durable audit artifact (written after this ledger, same session)

The four audits referenced in §5 ("could not verify these two audits exist as
discrete, countable artifacts") have now been recovered from their task-output
files and consolidated into
`context/repo-audits/2026-08-01-weighing-alerts-audit-findings.md` — a
deduplicated findings table, an ordered E2E readiness gate, the per-module
notification matrix, the C1-C8 role-activation corrections, and a rejected-
candidates log. Read it before re-deriving any of the counts in §5.

## 8. Next-session entry points

1. Run `backend/cmd/seed-position-duties` against the phone-QA DB (port 15544) and
   re-check `select count(*) from position_module_duties where duty_type='verify'`. Do
   not report §3 as closed until this returns > 0 for every module in
   `PendingNotificationDutyModules()`.
2. Once verify-duty rows exist, re-run `select count(*) from notification_requests` after
   driving a real weighing/vaccination proof through the app to confirm Layer 1 fires.
3. Locate or recreate the two audit artifacts referenced in the brief (23/44 and 27/10 —
   see §5) as durable files before relying on their counts again.
4. Do not grant `counts.read`/`counts.write` to `health_director` (§2) — this has already
   been done and reverted once.
5. Feature-flag DB-tweakability (§2) is unstarted; do not assume otherwise.
