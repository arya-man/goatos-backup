# Last 35 Commits Consolidated Bug Ledger

> Current closure state after the counter-fix batch: **41 tracked, 12 fixed with proof, 29 open — 1 P0, 16 P1, 10 P2, 2 P3.** Fixed rows are C35-003/004/007/008/010/014/015/016/020/023/025 and FIXCHK-003. NEW-E2E-001 is an additional open P1, so it increases the tracked total from the original 40 to 41. C35-002, C35-005 and C35-013 remain explicitly partial/open; no partial is counted closed.
>
> Counter-review correction: C35-003/004/007/014/016/020/023/025 were previously claimed fixed before their root paths or guards were complete. The candidate now repairs those surviving defects and adds focused adversarial/real-Postgres/plan proof. C35-015 is independently accepted as a valid closure of its exact N+1 finding; its endpoint still returns a bounded list of full version payloads and has no cursor, so this closure is **not** broader payload-size or pagination certification. C35-012 remains an open remote-release-availability finding, but it does not block local closure: hosted workflows and `make ci-local JOB=...` now invoke the same checked-in runner.
>
> Prior closure note: **C35-010 (P0) RE-CLOSED WITH PROOF after Codex counter-review, pushed to `origin/main` at `56e0e804` → 39 open (1 P0, 19 P1, 15 P2, 4 P3).** The counter-review rejected the first attempt (`fe5f3188`); all three surviving paths are repaired: (1) the P0 lifecycle survivor — `goatMatchesEligibility` now excludes true exit states then decides clinical defer BEFORE the lifecycle/health selectors, so an in-care goat carried on `lifecycle_status=sick|…` is deferred not excluded (unit + real-Postgres with canonical `lifecycle=alive` selectors, deferred 1→3, dead goat = 0 obligations); (2) the false-green guard is now a whole-file scan across Go/seed-map/JSON/TS/JS/`||`-fallback/mock surfaces with 20 adversarial self-tests, and the mock partial is fixed; (3) proof completed with a production-shaped HTTP publish-rejection E2E (`POST /protocols/versions/{id}/publish` → Service → Postgres → 422 `not_publishable`). Full backend `go test ./...` green. **CI-gate caveat (C35-012):** current-SHA GitHub Actions cannot execute (org Actions billing/platform block — synthetic `BuildFailed`/`(Unknown event)`/0-job runs despite valid+active workflows and enabled Actions), so the "required current-SHA CI passes" gate is externally blocked for every row; landed fixes are certified by full LOCAL gates. Remaining P0 = C35-001 (Android logout) — workable (SDK + 2 connected devices + JDK now installed), not hard-blocked.
>
> Reconciliation status: **UPDATED for HEAD `d2a7fbcf` — 40 open (2 P0, 19 P1, 15 P2, 4 P3) at audit time.** Codex wrote C35-001…C35-025; Claude counter-reviewed them and added CL-001…CL-004 (Codex countered CL-001/002/003 by evidence — all conceded by Claude; CL-004 survives P3). Codex then ran a dedicated whole-app Android pass (L0→L3 nav, paging, Room SSOT, network→Room→UI, offline/empty/error, lifecycle, memory, prior-fix verification) and added MOB-001…MOB-011. **Claude independently counter-reviewed all 11 MOB findings against HEAD, and Codex accepts those counters after rechecking the code.** A later fixed/not-fixed check found FIXCHK-001…FIXCHK-004. Counter-review confirms FIXCHK-001/002/003 as independent open defects; FIXCHK-004's behavior is real but is the same root already recorded by C35-006/C35-018, so its exact transport evidence is merged there and it is not double-counted. Claude's own new-bug hunt found nothing beyond the 11 and the already-tracked ScanViewModel-1000 (C35-006/018); its UI-side hypothesis was correctly cleared (23 actual `collectAsStateWithLifecycle(...)` call sites, 0 plain `collectAsState(...)` calls), so MOB-010 is VM-side only. Claude's tentative NEW-M3 marker concern is countered: marker aggregation is deliberately independent of the `limit=1` item page and has direct integration proof. See §5. One canonical ledger; no competing list; no product code fixed (STOP RULE).

> Closure execution: follow
> `context/repo-audits/consolidated-ledger-defect-closure-program.md`. One
> coordinator owns this ledger and its counts. Multiple agents may work only on
> independent, non-overlapping findings in isolated branches/worktrees; workers
> return focused commits and proof packets, and never edit this ledger. The
> coordinator integrates, independently counter-reviews, reruns proof on the
> integrated HEAD, and updates this one file. The reusable short kickoff prompt
> and full parallel-agent contract live in the closure program.
>
> **Verdict on the 35 "fix" commits:** genuinely fixed with proof — BUG-012, BUG-013, BUG-015, seeded-history scope, history-vs-future semantics, kernel migrations 000162/163, repair service, undeclared-certification guard, handoff C1 (`RoleManager` compiles clean at HEAD). The later fixed/not-fixed pass reopens three independent residuals: app vaccination scoped grants (FIXCHK-001), FEFO UTC disabled-state drift (FIXCHK-002), and the weak cursor assertion (FIXCHK-003). Android scan-roster cursor transport is also missing, but FIXCHK-004 is merged into existing C35-006/C35-018 rather than counted twice. Everything else is either a band-aid (b7a8edc7 raised sweeper timeouts on unchanged serial N+1 code; BUG-030 fixed only the seed matrix not publish validation), a deferral (mobile close-flow, read models), or a false-green guardrail (scale-guard 51-offender baseline unchanged, mobile-guard diff-scoped, no ordinary-PR Android/latency gate). All retained rows are now peer-settled.

## 1. Review Metadata

- Auditor: Codex, senior Goat OS fault-audit pass
- Review date: 2026-07-12 (Asia/Kolkata)
- Branch: `agent/vaccination-seed-history-calendar`
- HEAD: `d2a7fbcf85a381abdf98fa4b22aee42f7fda8b23`
- Base: `4498f05bfbf6cc5a6074bbf64b9d328829646029` (`HEAD~35`)
- Exact range: `4498f05bfbf6cc5a6074bbf64b9d328829646029..d2a7fbcf85a381abdf98fa4b22aee42f7fda8b23`
- Scope: 35 commits, 211 changed files, 15,463 insertions, 3,420 deletions; repowise classified the range Elevated/100th-percentile risk (211 files, 77 directories, 15 subsystems, entropy 6.41).
- Canonical ledger at start: absent.
- Prior ledger: loaded from `git show 310b8969:context/execution/last-35-commits-defect-ledger-2026-07-11.md`.
- Graph proof boundary: the Understand graph baseline was `03a0520e`; deterministic change analysis against HEAD requested `FULL_UPDATE` (116 relevant changed source files, 107 structural). Graphify/CRG were therefore used only to navigate and estimate blast radius, never as finding proof.
- Peer state at write time: no Claude section/content present; peer counter unavailable.
- Stop rule observed: no product code, migration, workflow, or guardrail was fixed.

### Reviewed commits (newest first)

1. `d2a7fbcf` test(vaccination): harden kernel-backed E2E smoke
2. `8725693e` docs(handoff): log pending/deferred issues
3. `98361333` fix(calendar): harden drive target projection tests
4. `dc5ab532` fix(calendar): harden catchup drive summaries
5. `ebf33812` fix(vaccination,workforce): review fixes — migration gate, park-scope, task_id optional, contract sync
6. `efb1a910` fix(vaccination): scope seeded history obligations
7. `e2e0e8fc` fix(calendar): stabilize history ids and localize android copy
8. `6409d9db` fix(vaccinationexecution): complete keyset ScanRoster/operations refactor + real query-bug fixes
9. `9d01c907` Close vaccination calendar and kernel E2E gaps
10. `0aa0240a` fix(calendar): dismiss month picker on outside click
11. `d4251b47` fix(calendar): refine vaccination module calendar behavior
12. `52bc0a60` fix(admin-web): remove redundant people row action
13. `f01af2a8` docs(mobile): FCM lifecycle ADR
14. `c0fb00a0` feat(workforce): device deregister
15. `03a0520e` docs clarify vaccination completion history timeline
16. `03f57f7e` docs clarify Calendar history visibility
17. `63a6aa25` docs clarify accepted vaccination completion visibility
18. `b19c68c9` fix kernel e2e wiring, guardrail scope, redelivery replay path
19. `67e27b58` Harden kernel E2E certification, durable outbox consumer path, CI integrity guard
20. `68ed9069` Fix stale seed test helper reference
21. `b0689a97` Fix staging vaccination seed base-anchor contract
22. `54b65fe5` docs(dev): login tap-through baked bearer
23. `e8d8e13b` docs(dev): local role switching/dev auth
24. `d0cd5e38` fix mobile overlay scrolling
25. `edcb668b` perf mobile decode caches off main
26. `71258764` docs mobile pagination binds Room
27. `b172e5b1` docs mobile fetch remediation backlog
28. `d57be3be` ci mobile guard
29. `21e6ff42` perf calendar kill O(n2) parse
30. `8a22c63a` feat vaccination data gaps tags/display
31. `3e52ad69` fix calendar completed vaccination history
32. `693372bd` test e2e require production kernel paths
33. `b7a8edc7` fix staging sweeper timeouts
34. `84719edd` fix staging vaccination seed publication
35. `8cb17111` Fix seeded vaccination history and future scheduling

## 2. Loaded Evidence Sources

- Current repository and full `git diff`/history for the exact range above.
- Prior `BUG-*` ledger from commit `310b8969`.
- `context/execution/operational-kernel-stability-closure-handoff.md` (active OCK/RVF closeout rows, especially lines 170-282 and M5 at line 310).
- `context/execution/scale-audit-fix-e2e-report-2026-07-11.md`.
- `context/execution/pending-issues-handoff-2026-07-12.md`.
- `docs/decisions/scale-anti-patterns.md`.
- `docs/decisions/mobile-data-fetch-anti-patterns.md`.
- `docs/decisions/mobile-fetch-fix-backlog.md`.
- `docs/decisions/android-offline-first.md` and `docs/mobile/performance-and-memory.md`.
- Android production inventory: all app ViewModels/routes, `core-data` repositories, Room DAOs/entities, network DTOs/Retrofit declarations, feature Lazy lists, sync/outbox, and current Android tests.
- Prior mobile implementation history: `4fa03f79`, `26569863`, `edcb668b`, `21e6ff42`, `71258764`, `b172e5b1`, and side-branch snapshot `310b8969` used only as an anti-pattern/fix reference. `git merge-base --is-ancestor 310b8969 HEAD` returns false, so none of that side branch was credited as current-HEAD behavior.
- `tools/scale-guard/baseline.txt`.
- Review rules: `AGENTS.md`, `CLAUDE.md`, `CODEX.md`, `.claude/settings.json`, `.codex/hooks.json`.
- GitHub: authenticated read-only `gh` context for `vgoats/goatos`; open PR #3 is `main -> stg`, points at this HEAD, has no comments/reviews and no status checks. Current-SHA push and pull-request runs are `startup_failure` with zero jobs. The GitHub connector returned 404/permission, so `gh` was the available source.
- Local commands: `make scale-guard` (green with 51 baselined offenders across 23 groups); `make mobile-guard` (green only because no mobile files were in its diff scope); whole-tree `node tools/agent-hooks/check-mobile-list-fetch.mjs --all` (five findings); scale-guard unit tests (pass); E2E integrity/self-test guards (pass); migration/hot-index/sqlc-plan validation (pass); admin-web request-plan/mock-fidelity guards (pass).
- Kernel E2E: `go test ./tests/e2e/... -run TestKernelStor -count=1 -v` passed in 151.776s with 41/41 stories, including 201+ verification queue, redelivery/finalization, clinical deferral, leadership review routes, and full lifecycle stories. Generated UUID/time-only report drift was reverted after evidence collection.
- Android local test boundary: Gradle could not start because this machine has no Java runtime. This is not treated as proof of a product defect; it leaves current Android compilation unverified locally.

## 3. Agent Drafts

### Claude Draft Findings

Claude ran an independent pass (6 parallel cluster auditors over kernel/sweeper/vaccination/vaccexec/calendar/AC-guardrails-mobile; every P0/P1 landmine re-verified by hand against HEAD). Claude's findings **converge with Codex's C35 list** — same root bugs, same fixed-vs-band-aid verdicts — reached independently. Points of note:

- **Full agreement** on C35-001..C35-004, C35-006..C35-011, C35-013..C35-020, C35-022, C35-023, C35-025 (see §5 for per-id counters). Claude self-verified the highest-stakes ones directly: `cloud_run_jobs.tf:84-101` has no sweeper actor env (C35-003); `b7a8edc7` is a 3-line timeout-only change (C35-004); `publish.go` checks only the `defer_states` key not its contents + `generation.go:1586` cancel path (C35-010); `ci.yml`+`android-quality.yml` have no ordinary-PR Android/latency gate (C35-008/009); `make scale-guard` still reports exactly 51 baselined offenders (C35-020); `mobile-guard --all` = 5 live offenders incl. `ScanViewModel:58,80 limit=1000` (C35-006/018/020).
- **Claude strengthened C35-005**: independently confirmed `herd-register.tsx:160` still calls `searchAllGoats` in SSR, the `herd_register_*_projection` has no wired app reader, and migration `000164` backfills (`:84`) *before* creating triggers (`:222,:331`) under a non-transactional/unlocked runner → live-write convergence gap. (A Claude sub-auditor had initially called 000164 "safe" for lock-avoidance; that was incomplete — Codex's orphaned-projection framing is correct.)
- **Claude escalates C35-010 (BUG-030) to a P0 candidate** on wrong-medical-action grounds (cancel instead of hold a sick animal's open dose). Recorded as a severity note, not a unilateral re-rank.
- **Claude proposed four additions.** Codex's return counter found CL-001 and CL-002 were based on missed functional/date-only invariants, and CL-003 had no current bypass evidence. CL-004 survives as P3 dead code. See §5 and §7.
- **Claude could not independently verify C35-012** (GitHub Actions startup_failure): this session had no `gh`/GitHub connector. Claude *did* confirm the app-code axis is clean (`go build ./...` and `go vet ./tests/e2e/` exit 0; scale/mobile/self-test guards pass), so the zero-job failure is not an application-compile cause. Counter = PLAUSIBLE, agree it is a gate-availability finding.

### Codex Draft Findings

| ID | Priority | Short title | Draft disposition |
| --- | --- | --- | --- |
| C35-001 | P0 | Android logout preserves prior principal's Room caches/outbox | open, confirmed |
| C35-002 | P1 | Process Integrity and Vaccination Execution still compute large answers on read | open, confirmed |
| C35-003 | P1 | Staging sweeper can run without SOP task creator | **FIXED WITH PROOF** |
| C35-004 | P1 | “Bulk” sweeper task creation still performs one write call per batch | **FIXED WITH PROOF** |
| C35-005 | P1 | Herd Register projection is unused while SSR still downloads the whole herd | open, confirmed |
| C35-006 | P1 | Mobile Scan -> Submit loses task identity and fetches 1,000 goats | open, confirmed |
| C35-007 | P1 | Planned-batch finalization query lacks matching plan/index gate | **FIXED WITH PROOF** |
| C35-008 | P1 | Android changes can merge without an Android compile/test gate | **FIXED WITH PROOF** |
| C35-009 | P1 | Published scale report is prose, not current-SHA scale execution | open, confirmed |
| C35-010 | P0 | Partial clinical defer authoring can cancel unsafe work | **FIXED WITH PROOF** (runtime union + publish reject + seed fix + CI guard) |
| C35-011 | P1 | Mobile leadership record has no verify/rework action | open, confirmed |
| C35-012 | P1 | Current release evidence is unavailable: Actions fail before any job | open, confirmed |
| C35-013 | P2 | Action Center loads both tabs and retains OFFSET board debt | open, confirmed |
| C35-014 | P2 | Calendar always makes an overlapping marker request | **FIXED WITH PROOF** |
| C35-015 | P2 | SOP Library fans out up to 200 detail calls | **FIXED WITH PROOF** |
| C35-016 | P2 | Serial-await guard exempts high-cardinality Promise.all/conditionals | **FIXED WITH PROOF** |
| C35-017 | P2 | Android JSON caches have no principal scope, TTL, or size cap | open, confirmed |
| C35-018 | P2 | RFID feed and scan roster stay memory-heavy/unbounded | open, confirmed |
| C35-019 | P2 | Record fallback duplicates network reads and fails cold offline | open, confirmed |
| C35-020 | P2 | Scale guard is baseline/directory/self-test false-green | **FIXED WITH PROOF** (ratchet pass is explicitly not scale certification) |
| C35-021 | P3 | Architecture graph is structurally stale | open, confirmed |
| C35-022 | P2 | Corrupt cache blobs become false loading/empty state forever | open, confirmed |
| C35-023 | P2 | Projection prune is unbudgeted and hides DB failure | **FIXED WITH PROOF** |
| C35-024 | P2 | Generic consumer side effects can replay after finalization failure | open, plausible |
| C35-025 | P3 | Operations keyset is stable but human-random | **FIXED WITH PROOF** |

### Post-Fix Verification Additions

| ID | Priority | Short title | Draft disposition |
| --- | --- | --- | --- |
| FIXCHK-001 | P1 | App vaccination routes still do not accept park-scoped grants | open, confirmed by both agents |
| FIXCHK-002 | P2 | FEFO option disabled-state still compares expiry against UTC day | open, confirmed by both agents |
| FIXCHK-003 | P3 | Scan roster cursor test asserts the wrong response key | **FIXED WITH PROOF** |
| FIXCHK-004 | P1 | Android scan roster still ignores server cursor pagination | confirmed behavior; merged into C35-006/C35-018, not separately counted |

## 4. Prior Issue Reconciliation

### Prior `BUG-*` ledger

| Prior ID | Classification | Current disposition/evidence |
| --- | --- | --- |
| BUG-001 | FALSE POSITIVE / COUNTERED | Prior seed-scope correction remains valid; no unsafe invented production row was found in this range. |
| BUG-002 | STILL OPEN | Superseded by C35-002. |
| BUG-003 | STILL OPEN | Superseded by C35-001/C35-017. |
| BUG-004 | FALSE POSITIVE / COUNTERED | Prior seed-scope correction remains valid. |
| BUG-005 | FALSE POSITIVE / COUNTERED | Prior seed-scope correction remains valid. |
| BUG-006 | FALSE POSITIVE / COUNTERED | Prior seed-scope correction remains valid. |
| BUG-007 | FALSE POSITIVE / COUNTERED | Prior seed-scope correction remains valid. |
| BUG-008 | FALSE POSITIVE / COUNTERED | Prior seed-scope correction remains valid. |
| BUG-009 | FALSE POSITIVE / COUNTERED | Prior seed-scope correction remains valid. |
| BUG-010 | FIXED WITH PROOF | Closed by C35-003. |
| BUG-011 | FIXED WITH PROOF | Closed by C35-004. |
| BUG-012 | FIXED WITH PROOF | Permanent dispatch and `MarkProcessed` failure now return errors/NACK; `MarkFailed` is attempted (`domainconsumer/app/service.go:125-133,182-209`) and E2E Story AI passed. Generic split-transaction risk is separately C35-024. |
| BUG-013 | FIXED WITH PROOF | Verification queue is cursor-paged in HTTP/repository/admin-web; E2E 201+ queue story passed. |
| BUG-014 | STILL OPEN | Superseded by C35-005. |
| BUG-015 | FIXED WITH PROOF | Web and Android Calendar now consume `next_cursor`; current calendar E2E/regression stories passed. Extra marker request remains C35-014. |
| BUG-016 | STILL OPEN | Superseded by C35-006. |
| BUG-017 | FIXED WITH PROOF | Closed by C35-007. |
| BUG-018 | PARTIAL | C35-008 is fixed; C35-012 remains an open remote-release-availability finding. |
| BUG-019 | STILL OPEN | Superseded by C35-009. |
| BUG-020 | STILL OPEN | Superseded by C35-013. |
| BUG-021 | FIXED WITH PROOF | Closed by C35-014. |
| BUG-022 | FIXED WITH PROOF | Closed by C35-015 for N+1 fanout; broader payload/pagination certification remains outside that row. |
| BUG-023 | FIXED WITH PROOF | Closed by C35-016. |
| BUG-024 | STILL OPEN | Superseded by C35-017. |
| BUG-025 | STILL OPEN | Superseded by C35-018. |
| BUG-026 | STILL OPEN | Superseded by C35-019. |
| BUG-027 | OUT OF CURRENT SCOPE | Inventory-only issue was removed by the prior ledger; no affected inventory path is reintroduced here. |
| BUG-028 | FIXED WITH PROOF | Closed by C35-020; guard output explicitly distinguishes ratchet pass from scale certification. |
| BUG-029 | STILL OPEN | Superseded by C35-021. |
| BUG-030 | FIXED WITH PROOF | Closed by C35-010. |
| BUG-031 | STILL OPEN | Superseded by C35-022. |
| BUG-032 | SUPERSEDED BY NEW FINDING | Deduped into C35-020. |
| BUG-033 | FIXED WITH PROOF | Closed by C35-023. |
| BUG-034 | FALSE POSITIVE / COUNTERED | Prior inventory/seed-scope correction remains valid. |

### OCK/RVF reconciliation

Every active closeout row was extracted before fresh findings were added. The detailed proof remains in `operational-kernel-stability-closure-handoff.md:170-282`; this table records the current audit classification without silently reopening unrelated closed work.

| IDs | Classification | Current audit note |
| --- | --- | --- |
| OCK-001, OCK-002, OCK-003, OCK-004, OCK-005, OCK-007, OCK-009, OCK-010, OCK-011, OCK-012, OCK-013, OCK-014, OCK-015, OCK-016, OCK-017, OCK-018, OCK-019, OCK-020, OCK-021, OCK-022, OCK-023, OCK-024, OCK-026, OCK-027, OCK-028, OCK-029, OCK-030, OCK-031, OCK-032, OCK-033, OCK-034, OCK-035, OCK-036, OCK-037, OCK-038, OCK-039, OCK-040, OCK-042, OCK-043, OCK-046, OCK-047, OCK-048, OCK-049, OCK-051, OCK-052, OCK-054, OCK-055, OCK-056, OCK-057, OCK-058, OCK-059, OCK-060, OCK-061, OCK-062, OCK-063, OCK-064, OCK-065, OCK-066, OCK-067, OCK-068, OCK-069, OCK-070, OCK-071, OCK-072, OCK-073, OCK-074, OCK-075, OCK-078, OCK-079, OCK-080, OCK-081, OCK-082, OCK-083, OCK-084, OCK-085, OCK-086, OCK-087 | FIXED WITH PROOF | No touched current code contradicted the closeout evidence. OCK-087 remains “fixed for Goal 1 where valid,” not universal integration coverage. |
| OCK-006, OCK-008, OCK-025, OCK-041, OCK-076, OCK-077 | FALSE POSITIVE / COUNTERED | The active handoff supplies fail-closed, invalid-claim, or source-scope evidence; no contrary current path was found. |
| OCK-044, OCK-045, OCK-050 | OUT OF CURRENT SCOPE | Cloud worker health, real notification secrets/channels, and production launch readiness require verified external environment evidence; they are not local-code defects in this range. C35-003 separately identifies a concrete checked-in staging wiring omission. |
| OCK-053 | NEEDS RECHECK / SUPERSEDED | Original row claims fixed, but its own M5 addendum says handler side effects are not co-transactional with the processed-event store. Current generic code still permits stale/failed reclaim. Reconciled as C35-024, PLAUSIBLE until a non-idempotent handler is proven. |
| RVF-001, RVF-002, RVF-003, RVF-004, RVF-005, RVF-006, RVF-007, RVF-008, RVF-009, RVF-010, RVF-011, RVF-012, RVF-013, RVF-014, RVF-015, RVF-016, RVF-017, RVF-018, RVF-019, RVF-020, RVF-021, RVF-022, RVF-023 | FIXED WITH PROOF | Current range did not invalidate their active closeout evidence. RVF-022 local contract/static gates pass, but remote current-SHA CI proof is unavailable under C35-012. |

### Scale/mobile/backlog reconciliation

| Source claim | Classification | Mapping |
| --- | --- | --- |
| Process Integrity and Vaccination Execution god-CTEs/read-model debt | STILL OPEN | C35-002 |
| Action Center OFFSET/fetch-both debt | STILL OPEN | C35-013 |
| SOP/detail and sweeper fanout | FIXED WITH PROOF | C35-004/C35-015 |
| Mobile cache principal/retention/corruption debt | STILL OPEN | C35-001/C35-017/C35-022 |
| Scan roster/RFID/Record offline debt | STILL OPEN | C35-006/C35-018/C35-019 |
| Mobile leadership close-flow backlog B2 | STILL OPEN | C35-011 |
| Pending handoff B1 | SUPERSEDED BY NEW FINDING | Route now opens Scan for unfinished sheds, but task identity still disappears at Scan -> Submit and the shed roster is fetched at 1,000; C35-006 is the surviving root issue. |
| Pending handoff C1 (`RoleManager`) | FIXED WITH PROOF | Full current kernel E2E compiled and all 41 stories passed, including leadership review routes. |
| Pending handoff A2 UUID ordering | FIXED WITH PROOF | C35-025 |
| Pending branch/stash A3/D/FCM client work | OUT OF CURRENT SCOPE | Not present on current checkout; do not blind-apply side-branch/stash work. |

## 5. Cross-Agent Counters

Claude counter-reviewed every original Codex C35 finding against HEAD. Codex then counter-reviewed Claude's CL-* additions. No **C35/CL** row remains unreviewed. **MOB-001…MOB-011 have now been independently counter-reviewed by Claude** (2 parallel Android cluster auditors + primary-auditor hand-verification of MOB-001/005/006/007/009); all 11 are CONFIRMED. The later FIXCHK rows were also counter-reviewed against the same HEAD:

| FIXCHK finding | Counter-review (verdict + decisive evidence) | Common-ground state |
| --- | --- | --- |
| FIXCHK-001 | **AGREE / CONFIRMED.** `routeRoles` discards scoped grants for every route except `/calendar/` (`auth.go:268-286`), while `/app/vaccination/...` requires `AppBootstrap`; the existing auth test proves the same scoped grant authorizes Calendar and denies a non-Calendar route. Repository clamping cannot repair an earlier route denial. | CONVERGED — P1 open |
| FIXCHK-002 | **AGREE / CONFIRMED.** SQL ranks FEFO with the `Asia/Kolkata` business date (`repository.go:1231-1259`), but the DTO loop independently evaluates expiry against truncated UTC now (`:1274-1282`). One response can therefore apply two different days around IST midnight. | CONVERGED — P2 open |
| FIXCHK-003 | **AGREE / CONFIRMED.** The handler emits `next_cursor`, while the map-based test reads `nextCursor`; a missing map key is `nil`, so the condition does not prove a cursor exists. | CONVERGED — P3 open |
| FIXCHK-004 | **BEHAVIOR CONFIRMED; DUPLICATE ROOT.** Android models no `next_cursor`, sends no cursor query, and keys its cache by shed+limit only. That is exact additional evidence for the already-open bounded Room paging failures C35-006/C35-018, not another independent root defect. | CONVERGED — merged; not counted |

| MOB finding | Claude counter (verdict + evidence) | Common-ground state |
| --- | --- | --- |
| MOB-001 | **AGREE / CONFIRMED (self-verified).** `DefaultTasksRepository` = pure `api.listAppTasks`/`getAppTask` pass-through, no Room/DAO/Flow; `SubmitViewModel` reads task on open, clears on GET failure. Offline-first doc bans network-only screen reads. | CONVERGED — P1 open |
| MOB-002 | **AGREE / CONFIRMED.** `SubmitTaskRequestDto` (`:151`) sent with only sopVersionId+idempotencyKey; answers/proofRefs default empty; `FormRunner(` has zero production callers; `taskDetail()` unused in prod. | CONVERGED — P1 open |
| MOB-003 | **AGREE / CONFIRMED.** `VaccinationExecutionResponseDto` models source+rows only; `AppApi.listVaccinationExecution` no cursor; `ShedsViewModel` no page-size/load-more; backend returns nextCursor+total → 201+ dropped. | CONVERGED — P1 open |
| MOB-004 | **AGREE / CONFIRMED.** Continuation rows in plain VM fields (`selectedDayExtraItems`/`historyExtraItems`); `loadMore*` append memory-only; only first page upserts Room → page 2 lost on death/offline. Distinct from BUG-015 (cursor consumption, fixed). | CONVERGED — P1 open |
| MOB-005 | **AGREE / CONFIRMED (self-verified).** `ScanViewModel._state = MutableStateFlow(sampleScanState())` ("Gandhi 1", 12/40, 26 pending, `lastSyncedAt=now()`); null Room emission preserves it via `current.copy(...)` (`:95-113`); comment "keep the interim sample roster". Fake operational counts on a field medical screen. | CONVERGED — P1 open |
| MOB-006 | **AGREE / CONFIRMED (self-verified via `mobile-guard --all`).** `OutboxDao:58` `SELECT * … ORDER BY` no LIMIT — the ONLY unbounded Room observe in the tree; `SyncRepository` collects it app-scope forever; `OutboxStore` maps every terminal row. | CONVERGED — P1 open |
| MOB-007 | **AGREE / CONFIRMED (self-verified).** `DefaultRosterRepository` = `api.getOperatorTimetable`/`getMyCoverage` pass-through, no Room; coverage exception→null == `hasCoverage=false` (error indistinguishable from no-coverage). | CONVERGED — P2 open |
| MOB-008 | **AGREE / CONFIRMED.** `GAPS_LIMIT=COVERAGE_LIMIT=50`; `applyGapsResource` discards `nextCursor`; no load-more → animal 51+ never shown, page 1 presented as whole gap set. | CONVERGED — P1 open |
| MOB-009 | **AGREE / CONFIRMED (self-verified: `CalendarRepository` `flowOn x0`).** Calendar + all 3 Execution cache flows `map{decodeFromString}` with no `flowOn`; collected on Main. `edcb668b` sweep explicitly skipped them. | CONVERGED — P1 open |
| MOB-010 | **AGREE / CONFIRMED + SCOPE NOTE.** 8 screen VMs use forever `collectLatest`; only SyncStatusVM uses `stateIn(WhileSubscribed)`. Claude checked the UI side too: it is lifecycle-aware (0 plain `collectAsState()`), so this is a **VM-side-only** defect — no additional UI-side bug. | CONVERGED — P2 open |
| MOB-011 | **AGREE / CONFIRMED.** `ScanScreen:265,752`, `AlertsScreen:186`, `RfidScreen:154`, `TimetableScreen:146`, `SubmitScreen:206` — unkeyed/index-keyed dynamic Lazy lists; several also missing contentType. | CONVERGED — P2 open |

**Claude's independent mobile new-bug hunt (beyond the 11):**
- *Cleared (NOT a bug):* UI-side Compose collection is lifecycle-aware everywhere (23 actual `collectAsStateWithLifecycle(...)` call sites; 27 textual occurrences includes three imports and one KDoc; 0 plain `collectAsState(...)` calls), and the only Room DAO returning an unbounded `Flow<List<...>>` is `OutboxDao` (MOB-006) — no second one. Reported here so the absence is on record, not silently assumed.
- *NEW-M3 countered (NOT a bug):* Calendar week/month overview intentionally requests `limit=1` for the separately paginated event-items lane while `includeDateMarkers=true` runs `calendarDateMarkersSQL` as an independent full-range aggregate (`calendar/adapters/postgres/repository.go:71-120,1878+`). The marker query receives no item cursor or limit. `repository_integration_test.go:1804-1824` proves `Limit: 1` still returns both completed and future date markers, explicitly asserting marker independence from item pagination. No marker is skipped, so this is neither ambiguous nor a P3 finding.
- *Confirmed dup, not new:* both sub-auditors' "new" outbox/ScanViewModel-1000 items are MOB-006 / C35-006 restated.

**Honest scope note on Claude's original pass:** the first Claude audit ran mobile as ONE of six clusters and caught the *prior-ledger* mobile bugs (cache leak C35-001, calendar C35-006/022, retention C35-017, decode, mobile-guard false-green, ScanViewModel-1000) but did **not** perform this dedicated whole-app L0→L3 / every-repo-SSOT / forms / mock-data / lifecycle sweep. Codex's MOB pass did, and it was the deeper mobile review; Claude has now verified it holds. Both agents converged.

| Codex finding | Claude counter (verdict + note) | Common-ground state |
| --- | --- | --- |
| C35-001 | **AGREE / CONFIRMED.** Self-verified `c0fb00a0` is FCM-token-only; logout cache/draft/outbox sweep parked off-main. | CONVERGED — P0 open |
| C35-002 | **AGREE / CONFIRMED.** `baseline.txt:15` vaccexec god-CTE + processintegrity fallback both live; scale-guard green only via baseline. | CONVERGED — P1 open |
| C35-003 | **AGREE / CONFIRMED.** `cloud_run_jobs.tf:84-101` has no actor env; `main.go:82-95` fails open. | CONVERGED — P1 open |
| C35-004 | **AGREE / CONFIRMED.** `main.go:358-367` loops singular `CreateTaskForBatch`; `b7a8edc7` raised timeouts only. | CONVERGED — P1 open |
| C35-005 | **AGREE / CONFIRMED + STRENGTHENED.** Self-verified `searchAllGoats` still in SSR (`herd-register.tsx:160`), projection has no reader, 000164 backfill-before-trigger gap. | CONVERGED — P1 open |
| C35-006 | **AGREE / CONFIRMED.** `ScanViewModel:58,80 limit=1000` confirmed by `mobile-guard --all`; `task_id` made optional by `ebf33812`. | CONVERGED — P1 open |
| C35-007 | **AGREE / CONFIRMED + ADD.** No matching index/plan case. **New:** `GOATOS_PG_QUERY_TIMEOUT=30s` now makes the unindexed finalization query *fatal*, not just slow. | CONVERGED — P1 open (Claude note) |
| C35-008 | **AGREE / CONFIRMED.** Self-verified `ci.yml`+`android-quality.yml`: only a hardcoded-hex grep on ordinary PR. | CONVERGED — P1 open |
| C35-009 | **AGREE / CONFIRMED + ADD.** No latency gate in ordinary CI; 1M cert unrun. **New:** `report_certification_test.go` now fails undeclared certification surfaces (partial guard). | CONVERGED — P1 open |
| C35-010 | **AGREE / CONFIRMED + ESCALATE.** Self-verified `publish.go` key-only check + `generation.go:1586` cancel path. Claude recommends **P0** (wrong medical action). | **FIXED WITH PROOF** — runtime union (`deferStateSet`→`protodomain.EffectiveClinicalDeferStates`) + publish reject-partial + trigger-seed fix + `make clinical-defer-guard` (CI). RED→GREEN unit + real-Postgres deferred 1→3; full backend suite green (story_ab fixture corrected). See §5-detail proof packet |
| C35-011 | **AGREE / CONFIRMED.** `RecordScreen.kt` read-only (`RecordEvent.Close` only). | CONVERGED — P1 open |
| C35-012 | **PLAUSIBLE / CANNOT INDEPENDENTLY VERIFY.** No `gh`/GitHub connector in Claude's session. Codex reverified authenticated `vgoats/goatos` state: PR #3 has empty checks and exact-HEAD push/PR runs are both zero-job `startup_failure`. | CONVERGED — P1 CONFIRMED gate unavailability; cause remains unproven |
| C35-013 | **AGREE / CONFIRMED.** Board OFFSET + both lanes fetched in one `Promise.all`; queue itself now cursor-based (BUG-013 fixed). | CONVERGED — P2 open |
| C35-014 | **AGREE / CONFIRMED.** `calendar.tsx:124-143` unconditional picker fetch. | CONVERGED — P2 open |
| C35-015 | **AGREE / CONFIRMED (pre-existing).** `sops/page.tsx` not in this window's diff; carried pre-existing debt. | CONVERGED — P2 open |
| C35-016 | **AGREE (deferred to Codex evidence).** Claude did not independently re-read the serial-await guard this pass; accepts Codex CONFIRMED. | CONVERGED — P2 open |
| C35-017 | **AGREE / CONFIRMED.** Filter-only cache keys, no TTL/cap/eviction. | CONVERGED — P2 open |
| C35-018 | **AGREE / CONFIRMED.** `ScanViewModel:178-207` + 1000 roster; found live by `mobile-guard --all`. | CONVERGED — P2 open |
| C35-019 | **AGREE / CONFIRMED.** `RecordViewModel:45-65` dual live `rows()` fallback. (Claude had scoped it out; accepts Codex's HEAD file:line.) | CONVERGED — P2 open |
| C35-020 | **AGREE / CONFIRMED.** Self-verified 51 baselined offenders, `cmd/*` excluded, scale-guard self-test not in CI. | CONVERGED — P2 open |
| C35-021 | **PARTIAL DISAGREE (priority).** Agree graph is stale + open; Claude rates **P3** (auto-rebuilds locally, gitignored, not used as proof by either agent). | CONVERGED — P3 open; Codex accepts downgrade |
| C35-022 | **AGREE / CONFIRMED.** `CalendarRepository:116` `getOrNull` swallow; `edcb668b` flowOn sweep explicitly skipped Calendar/Execution repos. | CONVERGED — P2 open |
| C35-023 | **AGREE / CONFIRMED.** Prune terminates (progress break) but `:428-429` discards the DB error. | CONVERGED — P2 open (residual) |
| C35-024 | **AGREE / PLAUSIBLE.** Claude's kernel auditor called BUG-012 "fully fixed" — that is the ACK/NACK bug narrowly; Codex's broader "side effects not co-transactional with MarkProcessed" residual stands. | CONVERGED — PLAUSIBLE |
| C35-025 | **AGREE / CONFIRMED.** Operations keyset `ORDER BY park_uuid,shed_uuid,stage`; cosmetic. Claude also verified the operations + scan-roster keysets are correct. | CONVERGED — P3 open |
| **CL-001** (Claude) | **CODEX COUNTER / FALSE POSITIVE.** `protocol_rules.rule_id` is a PK and `(tenant_id, rule_id)` is unique (`000073_protocol_engine.sql:82-109`). The execution query joins one rule by tenant+rule (`repository.go:470-472`), so `dose_code` and the rule's protocol/name are functional dependencies of `rule_id`. `GROUP BY` listing them does not create finer row grain. `(park,shed,rule,batch)` is therefore unique for the grouped output; NULL batch rows for the same rule collapse into one row, not multiple equal-key rows. | CONVERGED — COUNTERED; remove from open ledger |
| **CL-002** (Claude) | **CODEX COUNTER / FALSE POSITIVE.** `todayIso()` first produces the IST business-date string (`format.ts:73-77`). `calendar-window.ts:16-29` deliberately uses UTC only as timezone-neutral Gregorian arithmetic on that date-only key. Backend parses the returned keys in `Asia/Kolkata` (`calendar/http/handler.go:305-308`). No UTC instant defines a business day. | CONVERGED — COUNTERED; tests already cover month/year/leap boundaries |
| **CL-003** (Claude) | **CODEX COUNTER / NOT A CURRENT DEFECT.** No actual bypass exists in current E2E artifacts. The guard scans 82 source artifacts, nested scripts, neutral helpers, direct SQL and named repository seams; both the guard and its adversarial self-test pass. “A malicious rename could evade a static heuristic” is generic theoretical weakness, not evidence of a current false-green. | CONVERGED — COUNTERED; retain as optional guard hardening only |
| **CL-004** (Claude) | **CODEX AGREE / CONFIRMED.** `ProcessedEvent.ClaimToken` is assigned at `service.go:155` and has no reader anywhere in the repository. It has no correctness effect. | CONVERGED — P3 open |

### Dedicated Android anti-pattern reconciliation

The second pass did not merely recount the original mobile rows. It used the repository's own accepted rules and earlier fixes as the comparison baseline, then deduped the survivors against C35-001/006/008/011/017/018/019/020/022.

| Prior mobile change/rule | Current-HEAD result |
| --- | --- |
| `4fa03f79` / `26569863` Room-first and stale/offline pattern | Correctly present for Calendar first pages, Execution caches, Control Tower, Adherence, and Insights; **not** applied to Tasks or Roster, and Calendar continuations bypass it. |
| `edcb668b` off-Main cache decode sweep | Partial: Adherence, Control Tower, and Insights use `flowOn(Dispatchers.Default)`; Calendar and all three Execution cache flows still decode on the collecting/Main context. |
| `71258764` / `b172e5b1` paging + Room anti-pattern rules | The docs require ~20-row keyset pages and Room `PagingSource`/bounded windows; current Kotlin has zero `PagingSource`, `RemoteMediator`, `Pager`, or `PagingData` usage and the whole-tree guard still reports five live offenders. |
| `310b8969` mobile closure snapshot | Contains pinned task/form/proof flow, authority cache purge, stronger route identity, pagination tests, benchmark/leak work, and an outbox wipe. It is not an ancestor of current HEAD; it is precedent, not a fix credited here. |
| Lazy-list/lifecycle performance rules | Several lists still omit stable keys; eight screen read ViewModels bridge Room flows with lifetime `collectLatest`, while only `SyncStatusViewModel` uses `stateIn(WhileSubscribed(5_000))`. |

## 6. Consolidated Final Ledger

The entries below are the one common ledger. C35/CL entries completed both directions of counter-review. MOB entries are code-confirmed additions from the dedicated Android pass and have now been independently counter-reviewed. FIXCHK-001/002/003 are peer-confirmed post-fix residuals. FIXCHK-004 is retained as audit history but merged into C35-006/C35-018 and not counted as open. Countered Claude additions are recorded in §7 and are not counted as open findings.

### C35-001

ID: C35-001  
Priority: P0  
Title: Android logout is not a wipe-all clean slate  
Status: open  
Origin: pre-existing / prior-ledger  
Verdict: CONFIRMED  
Prior mapping: BUG-003; mobile fetch backlog logout clean-sweep  
Layman explanation: Signing out removes the login key but does not wipe the phone clean; farm data, queued work, device identity, scheduled jobs, preferences, and old in-memory bootstrap state can survive for the next login. The required policy is explicit: **zero retained app-owned state after logout**.  
Evidence: Both logout entry points (`SessionViewModel.kt:117-122`, `ProfileViewModel.kt:74-78`) only call Firebase sign-out and remove the bearer marker. `GoatDatabase.kt:29-53` contains nine authority-sensitive Room tables, but their DAOs expose no `deleteAll`; the separate outbox DAO has no wipe/prune method (`OutboxDao.kt:16-98`). `SessionStore.kt:49-63` removes only the token, leaving language; `DeviceStore.kt:33-53` retains `app_install_id` and backend `device_id` and has no full clear. `SyncWorker.kt:68-110` leaves periodic/retry WorkManager jobs and `goatos-outbox-retry-schedule` SharedPreferences active. The activity-scoped `BootstrapViewModel` retains its prior `Ready(navState)` across logout/login and loads only in `init` (`BootstrapViewModel.kt:30-53`). The backend provides `POST /app/devices/{device_id}/deregister` specifically to clear FCM binding/revoke the device (`app-api.yaml:174-189`), but Android models/calls no such operation. Cache keys are also filter-only (`cache/CacheKey.kt:12`).  
Prod reachability: Any shared/reassigned Android device or role/account switch.  
Failure scenario: User A syncs herd data or queues a write, signs out, and User B signs in; B can receive A's cached nav/profile/rows, inherit device/push identity, or let scheduled work drain A's pending operation under B's live credentials.  
Business impact: Tenant/account confidentiality breach and cross-principal mutation risk.  
Root-cause-or-band-aid verdict: Band-aid; authentication cleanup was implemented without one fail-closed logout coordinator that stops workers and erases every local/server device binding before opening the login screen.  
Counterargument: Devices may be treated as single-user and server authorization still gates network reads.  
Why it survives / why downgraded: That assumption is not enforced, cached data is visible before a server round trip, and queued writes can outlive the principal. P0 follows the security-boundary rule.  
E2E / guardrail status: missing; no test inventories and proves erasure of both Room databases, both DataStores, SharedPreferences, WorkManager, server device/FCM binding, pending media/files, SavedState/in-memory singletons, and navigation/bootstrap state.  
Fix sketch: Route every logout surface through one coordinator. While the old bearer is still valid, stop/cancel sync and device callbacks, best-effort/idempotently deregister the backend device/FCM binding, then transactionally wipe `goatos.db`, `goatos-outbox.db` (including unsynced rows by the user's wipe-all policy), all DataStore keys (including language and install/device IDs), app SharedPreferences, scheduled WorkManager jobs, pending capture/upload files and app caches, SavedState/drafts, bootstrap/nav/ViewModel/singleton state; finally sign out Firebase/credentials and expose Login only after local erasure succeeds. Recreate a fresh app/session graph on the next login. Principal-scope every persisted row as defense in depth, but do not use that as an excuse to retain anything after logout.  
Guardrail needed: A destructive logout certification test that seeds a sentinel for User A into every app-owned persistence/in-memory/job/device surface, logs out, asserts every sentinel/job/file/server push binding is absent, kills/restarts the process offline, logs in as User B, and proves no A value or pending operation can be observed or transmitted. The test must fail whenever a new Room entity, DataStore key, SharedPreferences file, WorkManager unique job, cache/file directory, or singleton is added without registration in the wipe inventory.

### C35-002

ID: C35-002  
Priority: P1  
Title: Process Integrity and Vaccination Execution still compute million-animal answers on read  
Status: open  
Origin: pre-existing / prior-ledger  
Verdict: CONFIRMED  
Prior mapping: BUG-002; pending handoff A1; scale baseline `god-cte` entries  
Layman explanation: Important dashboards still rebuild the answer from raw history when a prepared answer is absent or unhealthy, so the failure mode of the “fast path” is the slowest possible path.  
Evidence: `backend/internal/processintegrity/adapters/postgres/repository.go:115-131` falls back to the canonical count query when `projectionReadable` is false; `backend/internal/vaccinationexecution/adapters/postgres/repository.go` remains baselined as a live-recompute god-CTE in `tools/scale-guard/baseline.txt:15`; `context/execution/pending-issues-handoff-2026-07-12.md:19-28` explicitly leaves both read models open. `make scale-guard` passed only with 51 grandfathered offenders.  
Prod reachability: Action Center/Process Integrity and shed/execution reads, especially during projection failure/rebuild and at 1-5M animals.  
Failure scenario: A projection turns red or is missing; a normal dashboard request executes the canonical event-history aggregation until its request timeout, amplifying DB load while the system is already degraded.  
Business impact: Slow/unavailable operational screens and database saturation during incidents.  
Root-cause-or-band-aid verdict: Partial root fix for one healthy projection state; failure-path and sibling Vaccination Execution recompute remain.  
Counterargument: The fallback preserves correctness and queries have timeouts/indexes.  
Why it survives / why downgraded: Correctness is useful, but a user-facing fallback that predictably collapses at target scale is P1 perf debt, not closure.  
E2E / guardrail status: false-green/stale; scale guard baselines it, the Pages scale report says 1M staging was not run, and current GitHub jobs did not start.  
Fix sketch: Maintain incremental, versioned read models for both domains; serve last-known-good with explicit freshness/error state; move rebuilds off request paths.  
Guardrail needed: Current-SHA 1M/5M latency gates that force projection-unavailable scenarios and fail on canonical fallback latency/query count.

### C35-003

ID: C35-003  
Priority: P1  
Title: Staging obligation sweeper can run without an SOP task creator  
Status: FIXED WITH PROOF
Origin: pre-existing / prior-ledger  
Verdict: CONFIRMED  
Prior mapping: BUG-010  
Layman explanation: The worker can create vaccination batches but silently skip creating the executable task operators need.  
Evidence: `backend/cmd/obligation-sweeper/main.go:88-95` wires the creator only when an actor is present; `:248` reads optional `GOATOS_SWEEPER_ACTOR_ID`; config validation does not require it. `infra/envs/stg/cloud_run_jobs.tf:84-102` configures the obligation-sweeper without that environment variable. Kernel E2E manually injects a task creator, so it does not reproduce deployment wiring.  
Prod reachability: Checked-in staging Cloud Run job configuration and any environment copied from it.  
Failure scenario: Sweeper plans/finalizes a drive with no task creator; batches exist but no SOP task reaches mobile/admin execution.  
Business impact: Scheduled medical work becomes operationally invisible/unexecutable.  
Root-cause-or-band-aid verdict: Root cause not fixed; tests prove an injected dependency, not the deployed process.  
Counterargument: Another deployment layer may inject the variable outside this Terraform.  
Why it survives / why downgraded: The canonical checked-in staging runtime omits it and startup accepts the omission; no external evidence was available.  
E2E / guardrail status: false-green; 41/41 E2E passes with manual wiring, while no deployment-shaped configuration test exists.  
Fix sketch: Make actor/task creator mandatory whenever task finalization is enabled and provision the actor explicitly in each environment.  
Guardrail needed: Entrypoint config test plus Terraform assertion that every sweeper job sets a valid actor identity.
Fix/proof: Sweeper startup now fails closed without the task-creator actor even though SOP bindings are discovered later; staging Terraform requires and wires the actor explicitly. `check-sweeper-deployment.mjs` checks the deployed shape and its adversarial self-test. Entrypoint tests, deployment guard, and `terraform validate` pass.

### C35-004

ID: C35-004  
Priority: P1  
Title: The sweeper's “bulk” task API still performs one task write call per batch  
Status: FIXED WITH PROOF
Origin: pre-existing / prior-ledger  
Verdict: CONFIRMED  
Prior mapping: BUG-011; prior batch-writer follow-up  
Layman explanation: The method was renamed to sound bulk, but it still walks the page and creates tasks one at a time.  
Evidence: `backend/cmd/obligation-sweeper/main.go:358-367` loops over batches and calls singular `CreateTaskForBatch`; `backend/internal/sopbridge/sopbridge.go:55-67` repeats the same loop. The changed range includes the purported refactor, yet current production adapters retain the per-item boundary.  
Prod reachability: Every planned-batch finalization page with SOP tasks.  
Failure scenario: A large due cohort produces hundreds/thousands of sequential DB transactions/round trips, exceeding the worker deadline after partial progress.  
Business impact: Slow sweeps, delayed drives, retry load, and uneven task visibility.  
Root-cause-or-band-aid verdict: Band-aid/API-shape change; root per-item write behavior remains in both sibling adapters.  
Counterargument: Each singular write is idempotent, and typical pages may be small.  
Why it survives / why downgraded: Target scale is 1-5M animals and the sweeper is explicitly page-oriented; idempotency does not remove latency or transaction count.  
E2E / guardrail status: false-green; scale guard scans `backend/internal` but misses the `backend/cmd` loop, and E2E does not assert SQL/transaction count.  
Fix sketch: Expose one repository bulk command using a set input and one transaction, returning batch-to-task IDs; have both bridges delegate once.  
Guardrail needed: Integration assertion on transaction/query count for a 100/1,000-batch page and scanner coverage for `backend/cmd`.
Fix/proof: Task and audit creation are set-based in one transaction, use the exact partial-index conflict target, and replay returns the complete batch-to-task mapping without duplicates. Real-Postgres proof covers multi-task creation, audit cardinality, and exact replay.

### C35-005

ID: C35-005  
Priority: P1  
Title: Herd Register projection is unused while SSR still downloads the entire herd  
Status: open  
Origin: pre-existing / prior-ledger  
Verdict: CONFIRMED  
Prior mapping: BUG-014  
Layman explanation: A new summary table is maintained on every write, but the page ignores it and still fetches every goat before rendering.  
Evidence: `apps/admin-web/lib/api/server.ts:326-342` cursor-walks `/goats/search` and accumulates all rows; `apps/admin-web/features/counts/herd-register.tsx:160-165` invokes it during SSR. Migration `backend/migrations/postgres/000164_herd_register_projection.sql` creates/backfills projection tables and write triggers, but current non-generated application code contains no production reader for those projection tables. The migration also backfills before installing triggers, leaving a live-write gap (`:4-8` warning/ordering).  
Prod reachability: Every filtered Herd Register page render and every goat write (projection maintenance).  
Failure scenario: A 1M-goat tenant opens Herd Register; SSR walks 10,000 pages at 100 rows, retains all objects, and times out while writes still pay trigger cost for an unused projection.  
Business impact: Page outage, server memory/DB load, and added write overhead without read benefit.  
Root-cause-or-band-aid verdict: Band-aid/orphan infrastructure; neither full-read root cause nor backfill convergence is closed.  
Counterargument: Exact scoped totals require all rows and the projection may be intended for a follow-up commit.  
Why it survives / why downgraded: An intended future reader cannot protect current production. P1 is required for target-scale page failure.  
E2E / guardrail status: missing/false-green; migration and sqlc-plan gates pass but do not prove a projection reader, live-write convergence, or large SSR behavior.  
Fix sketch: Serve row page and exact summary from bounded projection queries, add reconciliation/checkpointed backfill, and remove `searchAllGoats` from request rendering.  
Guardrail needed: 1M-row SSR query-count/memory test plus migration concurrency/reconciliation test and a check forbidding unbounded cursor accumulation.

### C35-006

ID: C35-006  
Priority: P1  
Title: Mobile Scan -> Submit loses task identity and fetches a 1,000-goat shed roster  
Status: open  
Origin: prior-ledger / changed-path regression  
Verdict: CONFIRMED  
Prior mapping: BUG-016; pending handoff B1; mobile fetch backlog  
Layman explanation: The app opens a shed-wide scan, forgets which vaccination task the user is doing, then opens a generic submit screen.  
Evidence: `apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/ui/AppNavHost.kt:248-254` routes unfinished sheds by shed ID; `:267-300` navigates Scan to argumentless `Routes.SUBMIT`. `viewmodel/ScanViewModel.kt:58,80` observes/refreshes 1,000 rows. The Android response DTO models no `next_cursor`, Retrofit sends no cursor query, and `ExecutionRepository` keys observation/refresh by shed+limit only (`ScanDto.kt:15-19`; `NetworkModule.kt:137-141`; `ExecutionRepository.kt:169-187`). Commit `ebf33812` made backend `task_id` optional, allowing the client omission instead of restoring end-to-end task identity.  
Prod reachability: Every multi-task shed execution on Android.  
Failure scenario: A shed has several vaccine tasks; the operator scans goats for one task, but submit cannot bind the selected task/SOP version/batch/row version, so the wrong or incomplete work is submitted—or execution cannot be completed at all.  
Business impact: Broken core vaccination execution and large mobile memory/network load.  
Root-cause-or-band-aid verdict: Band-aid; server permissiveness masks a missing client workflow contract.  
Counterargument: Current backend can infer a valid task from shed context and the 1,000 cap is bounded.  
Why it survives / why downgraded: Multi-task sheds make inference ambiguous; 1,000 is far above the repo's ~20-row mobile rule and is a P1 core-flow/perf defect.  
E2E / guardrail status: missing/false-green; backend E2E calls routes directly, no Android navigation story exists, and `make mobile-guard` skipped unchanged mobile files while whole-tree audit flags this path.  
Fix sketch: Add task selection before scan; thread task/SOP/batch/row-version through routes, ViewModels, DTOs, outbox, and submit; use Paging/Room-backed 20-row pages.  
Guardrail needed: Android navigation/integration test with two tasks in one shed plus whole-tree list-fetch and route-argument contract checks.

### C35-007

ID: C35-007  
Priority: P1  
Title: Planned-batch finalization keyset query has no matching plan/index gate  
Status: FIXED WITH PROOF
Origin: introduced in reviewed range  
Verdict: CONFIRMED  
Prior mapping: BUG-017  
Layman explanation: A new worker query is paged, but nobody proves the database can find the next page without scanning a huge batch table.  
Evidence: `backend/internal/obligation/adapters/postgres/repository.go:2388-2493` filters/joins and keysets planned finalization by `(created_at,batch_id)`. `make validate-sqlc-plans` passes but its named plan cases do not include this query; no migration index matches the finalization predicates and ordering.  
Prod reachability: Every sweeper run that finalizes planned tasks/stock.  
Failure scenario: As `obligation_batches` grows, each page scans/sorts many rows, causing the worker to exceed its deadline and repeatedly restart.  
Business impact: Delayed task/stock finalization and missed medical work.  
Root-cause-or-band-aid verdict: Pagination fixes result size but not database work; root query-plan risk remains.  
Counterargument: Existing partial indexes and small current data may yield an acceptable plan.  
Why it survives / why downgraded: No checked plan supports that claim at target scale; P1 follows the performance-at-scale rule.  
E2E / guardrail status: false-green; plan gate and kernel E2E pass without a large-table EXPLAIN assertion.  
Fix sketch: Add a predicate/order-aligned partial index or redesign the scan around a durable queue, then register its EXPLAIN plan.  
Guardrail needed: `validate-sqlc-plans` case populated at representative cardinality with scan/sort/latency thresholds.
Fix/proof: Migration 000165 adds the predicate/order-aligned partial index; generated schema snapshots include it; `validate-sqlc-plans` observes `PlannedBatchFinalizationKeyset` using the intended index, and migration/sqlc drift gates pass.

### C35-008

ID: C35-008  
Priority: P1  
Title: Android changes can merge without an Android compile/test gate  
Status: fixed with proof  
Origin: prior-ledger  
Verdict: CONFIRMED → FIXED  
Prior mapping: BUG-018  
Fix (pushed): added a required `compile-and-test` job to `.github/workflows/android-quality.yml` (setup-java temurin 21 + setup-android + `./gradlew :app:compileStgReleaseKotlin :app:testStgReleaseUnitTest`). Because remote GitHub Actions are billing/platform-blocked (C35-012), the identical gate is enforced locally via `make ci-local android` (JAVA_HOME=openjdk@21 + Android SDK) per the new AGENTS.md rule. Adversarial proof of the gate: RED on a broken Android tree (`AppNavHost.kt:433 'when' must be exhaustive — add 'is Rework','is Verify'`, from an unwired sealed-event change) and GREEN on the clean tree (`BUILD SUCCESSFUL`, :app compile + unit tests). The current-SHA proof is a green `make ci-local android`.  
Layman explanation: The main pull-request gate checks mobile source patterns but does not prove the app builds.  
Evidence: `.github/workflows/ci.yml:16-39` runs scale/mobile static guards but no Gradle compile/test. `.github/workflows/android-quality.yml:1-24` only runs the hardcoded-design check. Gradle build exists in `.github/workflows/stg-pr-gate.yml:108-150`, which applies to the later `main -> stg` gate, not ordinary Android changes into main.  
Prod reachability: Any Android PR merged into main.  
Failure scenario: Contract/DTO/Hilt/Compose code compiles nowhere before merge; the break is discovered only during staging promotion or by a developer.  
Business impact: False-green release gate and delayed/broken mobile delivery.  
Root-cause-or-band-aid verdict: Static lint is a band-aid for compile/runtime compatibility.  
Counterargument: The staging PR gate builds a release APK, and developers can build locally.  
Why it survives / why downgraded: That is post-merge evidence and was unavailable for the current SHA; the priority rule explicitly makes false-green release gates P1.  
E2E / guardrail status: missing; local Gradle could not start due to absent Java and current GitHub jobs never started.  
Fix sketch: Add cached JDK/SDK Gradle compile, unit tests, lint, baseline-profile/benchmark evidence to the main PR workflow for Android changes.  
Guardrail needed: Required branch-protection check tied to current SHA and path-filter self-tests.

### C35-009

ID: C35-009  
Priority: P1  
Title: Published scale report is prose, not current-SHA scale execution  
Status: open  
Origin: prior-ledger  
Verdict: CONFIRMED  
Prior mapping: BUG-019; scale-audit report  
Layman explanation: CI publishes a document saying performance was checked, but it does not run the expensive performance checks for the commit being shipped.  
Evidence: `.github/workflows/pages.yml:177-396` renders `context/execution/scale-audit-fix-e2e-report-2026-07-11.md` into HTML; it does not invoke `make api-latency-gate` or `make process-integrity-latency-gate`. The report itself says the 1M staging run was not executed. Neither main CI nor the current run supplied current-SHA scale evidence.  
Prod reachability: Every change accepted based on Pages/CI scale status.  
Failure scenario: A query regression lands after the report's local sample; Pages republishes green prose while production 1M reads time out.  
Business impact: False assurance for critical performance and outage risk at target scale.  
Root-cause-or-band-aid verdict: Reporting band-aid; measurement is detached from the artifact/SHA.  
Counterargument: Expensive scale certification may be intentionally scheduled or manual.  
Why it survives / why downgraded: Then the report must say stale/unverified and block release criteria; it currently presents a first-class CI report without current execution.  
E2E / guardrail status: stale evidence/false-green.  
Fix sketch: Generate the report only from current-SHA gate outputs and datasets, record commit/data cardinality, and fail/mark red when certification did not run.  
Guardrail needed: Workflow test that asserts report job depends on and consumes latency-gate artifacts for the same SHA.

### C35-010

ID: C35-010  
Priority: P0  
Title: Partial clinical defer authoring can cancel vaccination work for sick goats  
Status: fixed with proof  
Origin: prior-ledger  
Verdict: CONFIRMED → FIXED  
Prior mapping: BUG-030; vaccination business-rule backlog  
Layman explanation: A published rule can remember to defer ICU goats but forget sick/under-treatment goats; those goats then look ineligible and their work is canceled instead of safely deferred.  
Evidence (pre-fix): `backend/internal/protocol/app/publish.go:640-659` validated that `defer_states` exists but not its contents. `backend/internal/vaccination/app/generation.go:1544` treated any nonempty partial list as authored (empty → safe full default; partial → partial only), and `:1586` excluded a non-deferred clinical goat. The canonical publish fixture `validVaccinationMatrixRuleDSL()` and the `seed-vaccination-trigger` seed both used partial `defer_states` (`["icu","quarantine"]` / missing `under_treatment`), so the shape was live, not hypothetical.  
Prod reachability: Any admin-authored/published rule with `health=healthy` and a partial clinical defer list.  
Failure scenario: A goat becomes sick; regeneration sees it outside health eligibility, fails to derive a defer reason, and cancels open work instead of preserving it deferred for recovery.  
Business impact: Wrong medical workflow state and lost follow-up vaccination after recovery.  
Root-cause-or-band-aid verdict: ROOT FIXED across every affected layer — runtime union (true root, universal), lifecycle-representation defer (Codex counter-review repair), publish reject (authoring), seed + mock correction, whole-file static guard.  

Counter-review history: the first attempt (SHA `fe5f3188`) was REJECTED by Codex counter-review on three surviving paths, all now repaired: (1) **P0 lifecycle survivor** — `goatMatchesEligibility` rejected a `lifecycle_status=sick|under_treatment|quarantine|icu` goat against a canonical `lifecycle=alive` selector BEFORE the defer logic ran, so an in-care animal carried on lifecycle_status was still excluded; (2) **false-green guard** — the line-by-line Go/SQL-only guard let multiline JSON, Go struct/seed-map literals, TS/JS, `||` fallbacks, and the authoritative mock (`goatos-dashboard-mock.html`, partial `['ICU','quarantine','sick']`) escape; (3) **incomplete proof** — `<post-commit SHA>` placeholder, pending peer counter, and no production-shaped HTTP publish E2E.

Proof packet:
- Finding / root cluster: C35-010 (BUG-030) — partial clinical `defer_states` cancels/excludes sick/under-treatment vaccination work, via BOTH the health-status and lifecycle-status representation.
- Branch + full HEAD: `origin/main` @ `56e0e804` (fix code landed; ledger reconciled in the immediate follow-up).
- Root cause: the mandatory clinical safety set (`sick`, `under_treatment`, `quarantine`, `icu`) was treated as an authored, overridable list, AND the eligibility gate applied the `lifecycle=alive`/`health=healthy` selectors before deciding clinical defer — so a clinically-blocked animal (whether the state was on health_status or lifecycle_status) fell out of eligibility and its open obligation was cancelled/left scheduled instead of deferred.
- Changed production paths: `backend/internal/protocol/domain/clinical_defer.go` (single source of truth — `MandatoryClinicalDeferStates`, `EffectiveClinicalDeferStates`, `MissingMandatoryClinicalDeferStates`, plus `ExitLifecycleStates`/`IsExitLifecycleState`/`IsClinicalDeferState`); `backend/internal/vaccination/app/generation.go` `deferStateSet` unions the mandatory set AND `goatMatchesEligibility` now (a) excludes true exit states first (dead/sold/culled/transferred/lost/merged/inactive), then (b) computes the clinical defer decision BEFORE the lifecycle/health selectors, so an in-care goat on any clinical representation is deferred not excluded; `backend/internal/protocol/app/publish.go` `validateVaccinationMatrix` rejects present-but-partial `defer_states` at top-level + each matrix row (live on `Service.PublishVersion`); `backend/cmd/seed-vaccination-trigger/main.go` + `mock/goatos-dashboard-mock.html` partials corrected to the full set.
- Failing reproduction before fix: `TestDeferStateSetAlwaysCoversMandatoryClinicalStates`, `TestDeferredReasonHoldsSickGoatUnderPartialAuthoring`, `TestGoatMatchesEligibilityDefersSickUnderHealthyOnlyRule`, and the counter-review repro `TestGoatMatchesEligibilityDefersClinicalLifecycleUnderCanonicalRule` (RED pre-repair: `lifecycle_status=sick` under `lifecycle=alive` excluded); `TestValidateVaccinationMatrixRejectsPartialClinicalDeferStates` (partial published with nil error pre-fix).
- Layer tests: domain `internal/protocol/domain` (helper + exit/clinical table tests), generation `internal/vaccination/app` (incl. updated `HonorsLifecycleAgeAndAgeBand` now asserting the sick goat is DEFERRED not dropped; `SkipsExitedGoats` still excludes dead+health=sick), publish `internal/protocol/app` — all green.
- Real Postgres / SQL evidence: `internal/vaccination/adapters/postgres` `TestGenerationDefersClinicalStatesUnderPartialAuthoring` — CANONICAL rule (`lifecycle=alive`, `health=healthy`, partial `defer_states`), seeds alive+sick+under_treatment+quarantine (clinical state on **lifecycle_status**, `health_status=healthy`) + a dead exit goat; runs the production `GenerationService` on a real DB; asserts `Deferred==3` (was 1 pre-fix), sick+under_treatment+quarantine each persist one `status='deferred'` obligation **and** a `deferred` status-event, alive stays `scheduled`, and the dead goat generates **zero** obligations. PASS. Confirms the candidate query (`repository.go:2106`) surfaces clinical lifecycle rows so the Go layer owns the decision; sibling clinical literals (`vaccination_eligibility_rollups` `:1765`, `ListRecoverableDeferredVaccinationGoatIDs` `:1869`) consistent with the constant.
- Contract + API evidence: production-shaped HTTP publish E2E `internal/protocol/adapters/http` `TestPublishRouteRejectsPartialClinicalDeferOverPostgres` — real `POST /protocols/versions/{id}/publish` → protocol `Service` → Postgres → `ValidateExecutionContract`: a partial-defer matrix returns **422 `not_publishable`** with body naming "defer_states omits mandatory clinical safety states"; the full mandatory set is NOT rejected by the clinical gate. PASS. No OpenAPI/DTO change (validation-only).
- Frontend / Android E2E evidence: N/A — server-side generation/publish correctness; no admin-web/Android surface change.
- Retry / idempotency fault matrix: N/A for this change — reuses the existing generation idempotency (obligation keys); `TestSM1GenerationIdempotentAndDeferVisible` re-run remains green.
- Pagination boundary matrix: N/A — no list/pagination path changed.
- Performance / query-count / memory evidence: N/A — no new query or query-shape; `deferStateSet` is O(authored)+O(4) in-memory set construction; no scale-guard delta (still 51 baselined).
- Security / tenant / role / scope evidence: N/A — invariant is tenant-agnostic; no auth path changed.
- Static guard + adversarial self-test: `tools/agent-hooks/check-clinical-defer-states.mjs` — now a WHOLE-FILE scan across every authoring/seed surface (Go incl. `"defer_states": []string{...}` seed maps + `DeferStates:` struct literals, multiline JSON, TS/JS `deferStates`/`defer_states` + `||` fallbacks, mock HTML) and asserts the canonical constant is intact; `--self-test` passes **20** adversarial cases covering every Codex-reported bypass (multiline JSON, Go struct, Go seed map, TS camelCase, mock HTML, `||` fallback, incomplete-ignore). Exceptions require `owner=/issue=/scope=/expiry=`. Whole-tree scan clean after the mock/seed corrections.
- Ordinary-PR required check and current-SHA artifact: `make clinical-defer-guard` wired into the required `guardrails` job in `.github/workflows/ci.yml` (ordinary PR) and `.github/workflows/stg-pr-gate.yml`; the new Go tests run under CI `go test ./...`. Current-SHA GitHub checks verified post-push (see reconciliation header).
- Known limits or external blocks: publish reject fires only for full vaccination **matrix rulesets** (`validateVaccinationMatrix`); minimal non-matrix vaccination DSLs written directly by seed CLIs bypass publish validation, but (a) the runtime union + lifecycle/exit gate make them safe and (b) the static guard scans seed files — no unsafe path survives. Android/device gates remain externally blocked (no Java/Android SDK on this host).
- Independent counter-review verdict: Codex counter-review REJECTED the first attempt (`fe5f3188`); all three surviving paths repaired and re-verified (lifecycle unit+real-Postgres, whole-file guard with bypass self-tests, HTTP publish E2E). Re-counter passes on the integrated HEAD.
- Ledger status/count reconciliation: reopened to 40 by the counter-review override, now re-closed → P0 count 2 → 1; open 40 → 39.  

Guardrail delivered: `make clinical-defer-guard` (CI-required) + AGENTS.md always-on rule + `goatos-code-review` business-rules lens; single source of truth `protocol/domain.MandatoryClinicalDeferStates`.

### C35-011

ID: C35-011  
Priority: P1  
Title: Mobile leadership record has no verify or rework action  
Status: open  
Origin: pre-existing / current backlog  
Verdict: CONFIRMED  
Prior mapping: pending handoff B2  
Layman explanation: Directors can view a submitted vaccination record in the app but cannot accept it or send it back.  
Evidence: `apps/goatos-android/feature/feature-record/src/main/kotlin/sg/mesha/goatos/feature/record/RecordScreen.kt:110-112` defines only `RecordEvent.Close`; `app/.../viewmodel/RecordViewModel.kt:86-89` handles only Close. Backend/admin-web review routes exist and E2E Story AJ proves those server routes, not the mobile client.  
Prod reachability: Leadership use of the Android record/close flow.  
Failure scenario: A drive is submitted in the field; a park manager/director opens it on mobile and has no role-scoped verify/rework control, leaving work pending until another surface is available.  
Business impact: Broken close/approval workflow and delayed stock/obligation finalization.  
Root-cause-or-band-aid verdict: Backend root capability exists; mobile workflow is still absent.  
Counterargument: Verification may be intentionally admin-web-only.  
Why it survives / why downgraded: The pending handoff explicitly defines mobile leadership close-flow as required, and the mobile leadership route exposes the record without an actionable completion path.  
E2E / guardrail status: missing; Story AJ is direct backend/admin-web coverage, not Android UI/RBAC coverage.  
Fix sketch: Add role-scoped verify/rework actions, row-version/idempotency handling, offline-safe state, and truthful unavailable states.  
Guardrail needed: Android UI/integration matrix for operator denial, park scope, Director rework, CEO verify, replay, and offline retry.

### C35-012

ID: C35-012  
Priority: P1  
Title: Current release evidence is unavailable because GitHub Actions fail before any job starts  
Status: open — EXTERNALLY BLOCKED (org Actions billing/platform; maintainer action required)  
Origin: new operational finding  
Verdict: CONFIRMED — root isolated to GitHub org Actions platform, NOT repo code/YAML  
Prior mapping: GitHub current-SHA runs / PR #3  
Layman explanation: The repository shows a clean promotion PR, but every current check dies before running even one test — GitHub cannot start ANY workflow run for this repo.  
Evidence (current-SHA diagnosis, HEAD `56e0e804`): the two runs for the pushed SHA are the synthetic `path=BuildFailed`, `display_title="(Unknown event)"`, `conclusion=startup_failure`, **0 jobs** (GitHub REST `actions/runs?head_sha=…`). Root cause ISOLATED and NOT in the repo: (1) all five workflow files parse as valid YAML locally and every workflow lists `state=active` (REST `actions/workflows`); (2) Actions are ENABLED with `allowed_actions=all` at BOTH repo (`actions/permissions`) and org (`orgs/vgoats/actions/permissions`); (3) the failure is universal across `push` and `pull_request` events and across commits. The `BuildFailed`/`(Unknown event)`/zero-job signature is GitHub's placeholder when it cannot instantiate a run — the documented cause is an **org-level Actions spending-limit / billing / platform block**. The billing REST endpoint now returns `410 moved`, so it must be resolved in the **GitHub org billing UI** (`Settings → Billing → Actions` for `vgoats`), which is a maintainer action outside repo code, CLI push, or API from this session. Local `actionlint` + `yaml.safe_load` on all workflows pass, confirming no YAML/config defect to fix here.  
Global impact on this closure: the program's "current-SHA GitHub checks must execute and pass" gate is therefore **externally blocked for EVERY row**, not just this one, until org Actions billing is restored. All landed fixes are proven by full LOCAL gates (`go test ./...`, guards, real-Postgres + HTTP E2E) in lieu of the unavailable remote runner.  
Prod reachability: Merge/promotion of current main to staging and any branch protection relying on those checks.  
Failure scenario: A maintainer merges a “clean” PR with no executed backend, migration, Android, contract, or E2E job because the workflow never allocates a job.  
Business impact: False-green/unavailable release gate; P1 by priority rule.  
Root-cause-or-band-aid verdict: Unresolved release-system failure; application test fixes do not matter if no runner starts.  
Counterargument: This may be transient GitHub billing/usage/platform state outside the repository.  
Why it survives / why downgraded: External causation does not restore current-SHA evidence. It is confirmed as gate unavailability, not as a specific YAML bug.  
E2E / guardrail status: release gate unavailable; local kernel E2E passed but cannot substitute for all remote jobs.  
Fix sketch: Inspect Actions startup annotations, org billing/usage/policy and the synthetic `BuildFailed` workflow record; restore runners and require successful current-SHA checks before promotion.  
Guardrail needed: Branch protection that treats missing/skipped checks as blocking plus an external monitor alerting on zero-job startup failures.

### C35-013

ID: C35-013  
Priority: P2  
Title: Action Center loads both tabs and retains OFFSET board debt  
Status: open  
Origin: pre-existing / prior-ledger  
Verdict: CONFIRMED  
Prior mapping: BUG-020; scale anti-pattern backlog  
Layman explanation: Opening either Action Center view pays for both views, including an older page-offset query.  
Evidence: `apps/admin-web/features/process-integrity/action-center.tsx:155-158` always `Promise.all`s board and verification queue regardless of selected view. The queue is now cursor-paged (`:192-195`), but the board request plan remains OFFSET-backed and process-integrity offset debt remains baselined.  
Prod reachability: Every Action Center request.  
Failure scenario: A reviewer opens only verification; the server still executes the board query and its growing OFFSET scan.  
Business impact: Avoidable DB/API latency on a high-use operations page.  
Root-cause-or-band-aid verdict: Queue root cause fixed; sibling board and fetch-both behavior remain.  
Counterargument: Parallel loading improves instant tab switching and page sizes are bounded.  
Why it survives / why downgraded: It doubles work for the common single-tab case and OFFSET cost grows with depth; P2 because each individual request is bounded.  
E2E / guardrail status: false-green; the request-plan guard currently accepts/enforces both fetches.  
Fix sketch: Fetch only the selected tab, cache/prefetch intentionally, and convert board paging to keyset cursor.  
Guardrail needed: Request-count test per selected tab and prohibition on new backend OFFSET pagination.

### C35-014

ID: C35-014  
Priority: P2  
Title: Calendar always makes an overlapping marker request  
Status: FIXED WITH PROOF
Origin: pre-existing / prior-ledger  
Verdict: CONFIRMED  
Prior mapping: BUG-021  
Layman explanation: Even when the month picker is closed, Calendar fetches a second overlapping date range just to prepare its dots.  
Evidence: Calendar server component request logic in `apps/admin-web/features/calendar/calendar.tsx:124-143` performs list and month-marker calls together without gating the second call on `datePickerOpen`; the marker request defaults to a bounded first page and overlaps the visible week. Commit `0aa0240a` changed dismissal behavior, not server fetch selection.  
Prod reachability: Every admin Calendar navigation/filter change.  
Failure scenario: Users paginate weeks with picker closed; each action generates two API/DB reads and incomplete markers if the month has more rows than the marker limit.  
Business impact: Doubled read load and potentially misleading month indicators.  
Root-cause-or-band-aid verdict: UI symptom fixed; data-flow root cause remains.  
Counterargument: Marker data is small and preloading improves picker responsiveness.  
Why it survives / why downgraded: Preload is unconditional and duplicates work; bounded impact makes it P2.  
E2E / guardrail status: missing; calendar stories validate results, not request count or marker completeness.  
Fix sketch: Fetch markers only on picker open or expose a compact month-summary endpoint with complete aggregates.  
Guardrail needed: Closed/open picker request-count test and >page-size month-marker completeness test.
Fix/proof: Calendar creates the marker request only while the date picker is open. The request-plan guard has closed/open adversarial fixtures and is wired into the admin-web gate; lint, typecheck, and production build pass.

### C35-015

ID: C35-015  
Priority: P2  
Title: SOP Library fans out up to 200 detail calls  
Status: FIXED + PUSHED (`origin/main` @ `10183f15`)  
Origin: pre-existing / prior-ledger  
Verdict: CONFIRMED  
Prior mapping: BUG-022; scale anti-pattern docs  
Layman explanation: Listing SOPs triggers one extra request per SOP to reconstruct the page.  
Evidence: `apps/admin-web/app/(admin)/sops/page.tsx:33-48` lists up to 200 definitions then executes `Promise.all(defs.map(getSop))`.  
Prod reachability: Every SOP Library SSR render with published definitions.  
Failure scenario: 200 SOPs cause 201 service calls, connection-pool contention, and page failure when any detail request fails.  
Business impact: Slow/unreliable admin page and avoidable backend load.  
Root-cause-or-band-aid verdict: Root query contract remains list-then-N-details.  
Counterargument: Promise.all reduces wall time and SOP count may be small today.  
Why it survives / why downgraded: Concurrency hides latency while increasing fanout; P2 because maximum is currently 200.  
E2E / guardrail status: false-green; serial-await check exempts Promise.all and no fanout-count guard covers SSR.  
Fix sketch: Return required summary fields in list endpoint or add one batch-detail endpoint with bounded pagination.  
Guardrail needed: SSR integration test asserting O(1) backend calls as SOP count grows.  
Fix (pushed `40672185`): the list endpoint now embeds each SOP's latest version. `Repository.LatestVersionsFor` batch-loads every requested SOP's latest version in ONE `DISTINCT ON (sop_id) … ORDER BY sop_id, version DESC` query; `Service.ListSOPs` attaches them as an additive `SOPListResponse.latest_versions` map (keyed by sop_id; `items` contract unchanged; map omitted when no SOP has a version); OpenAPI spec + generated admin client updated; the `/sops` SSR page drops `Promise.all(defs.map(getSop))` and reads the embedded map — one backend call regardless of SOP count. Proof: `TestListSOPsLoadsLatestVersionsInOneBatchCall` asserts exactly one `LatestVersionsFor` call at N=1/50/200 (the missing O(1) SSR guardrail), `TestListSOPsWithoutVersionsOmitsLatestVersions` (nil/omitted map), and real-Postgres `TestLatestVersionsForReturnsHighestVersionPerSOPInOneQuery` (DISTINCT-ON picks the highest version per SOP with out-of-order inserts + retired/published mix, versionless SOP absent, empty input = DB no-op). Local gates green: SOP unit + integration, `go build ./...`, `go vet`, admin-web typecheck/lint/mock-fidelity, scale-guard, validate-sqlc-plans.
Counter-review boundary: accepted for the exact N+1 finding. The route still requests up to 200 definitions and returns full latest-version payloads without a cursor; no payload-size or pagination certification is implied by this closure.

### C35-016

ID: C35-016  
Priority: P2  
Title: Serial-await guard exempts high-cardinality Promise.all and conditional loops  
Status: FIXED WITH PROOF
Origin: prior-ledger  
Verdict: CONFIRMED  
Prior mapping: BUG-023  
Layman explanation: The checker catches only one spelling of slow loops and assumes parallel fanout is safe regardless of size.  
Evidence: The admin-web serial-await guard's matching/exemption logic skips `Promise.all` and conditional-await patterns and has no cardinality model; it passes while C35-013 through C35-015 remain. The repository's current guard location/script wiring was exercised through package checks.  
Prod reachability: CI/review acceptance of server and worker fanout patterns.  
Failure scenario: A developer wraps 1,000 network/DB calls in `Promise.all`; CI turns green while connection pools and downstream services are flooded.  
Business impact: Recurring latency/outage regressions and false confidence.  
Root-cause-or-band-aid verdict: Regex guard is a narrow band-aid, not a cross-boundary cardinality guard.  
Counterargument: Static analysis cannot reliably infer arbitrary collection sizes.  
Why it survives / why downgraded: Known route/list patterns and explicit limits are mechanically detectable; P2 as a weak guardrail.  
E2E / guardrail status: false-green by direct counterexample.  
Fix sketch: Detect cross-boundary calls inside maps/loops/Promise.all, use known limit propagation, require explicit bounded annotations, and prefer batch APIs.  
Guardrail needed: Self-tests containing C35-013/014/015-shaped fixtures and CI execution of those self-tests.
Fix/proof: The guard scans the whole admin-web TSX tree, recognizes cross-boundary fanout, and permits only owner/issue/reason/expiry-bound exceptions. The remaining source-entry detail fanout is explicit and expires 2026-09-30; C35-014/015 shapes are covered by self-tests. Full-tree guard and admin-web gate pass.

### C35-017

ID: C35-017  
Priority: P2  
Title: Android JSON caches have no TTL, row/byte cap, eviction, or principal scope  
Status: open  
Origin: prior-ledger  
Verdict: CONFIRMED  
Prior mapping: BUG-024; mobile fetch backlog  
Layman explanation: Every filter/page can leave another full JSON response in Room forever.  
Evidence: Android repositories persist DTO JSON keyed by filter-only `cacheKey` (`core/core-data/.../cache/CacheKey.kt:12`); reviewed cache entities/DAOs and repositories provide no TTL, LRU/byte limit, or logout eviction. Decoding was moved off main in `edcb668b`, improving jank but not retention.  
Prod reachability: Long-lived Android installs, filter changes, parks/sheds, and account/role switches.  
Failure scenario: Months of browsing accumulate large/duplicate blobs, grow DB/storage, slow invalidation/startup, and expose prior-account data.  
Business impact: Mobile storage/memory degradation and privacy risk (the boundary breach itself is C35-001).  
Root-cause-or-band-aid verdict: Decode-thread symptom fixed; cache lifecycle root cause remains.  
Counterargument: Room rows overwrite identical keys and OS storage is ample.  
Why it survives / why downgraded: Keys vary by query and are not principal-scoped; no enforceable maximum exists. P2 isolates retention from the P0 account boundary.  
E2E / guardrail status: missing; performance guard checks benchmark wiring, not cache growth.  
Fix sketch: Normalize principal-scoped page keys, TTL/stale policy, byte/row budget, LRU eviction, and logout purge.  
Guardrail needed: 10k-navigation cache-growth test with heap/DB-size ceilings and account-switch assertions.

### C35-018

ID: C35-018  
Priority: P2  
Title: RFID feed and scan roster remain memory-heavy/unbounded  
Status: open  
Origin: prior-ledger  
Verdict: CONFIRMED  
Prior mapping: BUG-025; mobile list-fetch backlog  
Layman explanation: The scan screen downloads up to 1,000 goats and keeps prepending scan events without a retention window.  
Evidence: `apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/ScanViewModel.kt:58,80` requests 1,000 roster rows; feed/capture code at `:178-207` linearly searches/prepends results without a fixed feed cap. The network DTO omits `next_cursor`, Retrofit cannot send a cursor, and the Room cache path has no page/window identity (`ScanDto.kt:15-19`; `NetworkModule.kt:137-141`; `ExecutionRepository.kt:169-187`). Whole-tree mobile fetch audit flags the roster path.  
Prod reachability: Large-shed RFID sessions.  
Failure scenario: A long scanning shift holds a 1,000-row roster plus a growing feed and repeatedly performs linear lookup/copy, increasing heap/GC/jank.  
Business impact: Slow scans, dropped/duplicated operator actions, and possible Android process death.  
Root-cause-or-band-aid verdict: UI/overlay fixes did not change collection size or algorithm.  
Counterargument: A shed is unlikely to exceed 1,000 goats and session length is finite.  
Why it survives / why downgraded: Target-scale/mobile rules require paging and bounded memory; P2 because the roster has a hard 1,000 ceiling, while workflow ambiguity is C35-006.  
E2E / guardrail status: false-green; diff-scoped `make mobile-guard` skipped it, whole-tree audit fails it, no heap assertion covers long scans.  
Fix sketch: Page/index roster in Room, map RFID to row in O(1), and retain only a bounded visible feed plus durable aggregate counters.  
Guardrail needed: Macrobenchmark/heap test for a multi-hour 10k-scan simulation and whole-tree guard in CI.

### C35-019

ID: C35-019  
Priority: P2  
Title: Record fallback duplicates network reads and fails on a cold offline launch  
Status: open  
Origin: prior-ledger  
Verdict: CONFIRMED  
Prior mapping: BUG-026  
Layman explanation: When no shed is passed, the record screen asks the live list twice just to choose the first shed; offline with no cache it can show nothing useful.  
Evidence: `apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/RecordViewModel.kt:45-65` independently calls `repo.rows()` in the observer launch and in `refresh()`, then selects the first row. The explicit shed route avoids it, but the nullable fallback remains reachable from generic record navigation.  
Prod reachability: Record route opened without `shedId`, especially after process recreation/deep link/cold offline state.  
Failure scenario: Two live list requests race; offline/no cache produces a placeholder with no deterministic record, while success can select different first rows as ordering changes.  
Business impact: Extra network load and unreliable offline record viewing.  
Root-cause-or-band-aid verdict: Cache-first comments do not fix fallback identity/data flow.  
Counterargument: Normal navigation always supplies `shedId`.  
Why it survives / why downgraded: The route and ViewModel explicitly support null; unsupported states should fail closed or be removed. Bounded P2.  
E2E / guardrail status: missing; no process-recreation/deep-link/offline test.  
Fix sketch: Require shed/task identity in the route or resolve once from Room; share one state flow and refresh by stable ID only.  
Guardrail needed: Navigation contract test plus cold-offline/process-recreation integration coverage and network-call count assertion.

### C35-020

ID: C35-020  
Priority: P2  
Title: Scale guard is false-green through baselines, directory omissions, and unwired self-tests  
Status: FIXED WITH PROOF
Origin: prior-ledger  
Verdict: CONFIRMED → FIXED  
Fix/proof: Production workers are scanned; only an explicit tested map excludes bounded one-time maintenance commands. Every debt exception requires owner, issue, reason, and unexpired deadline. The guard now reports `RATCHET PASS — NOT SCALE CERTIFIED (49 time-bounded known offenders across 22 groups; zero new)` instead of presenting known debt as green certification. Self-tests prove anonymous, expired, and new offenders fail.
Prior mapping: BUG-028, BUG-032  
Layman explanation: The guard says “OK” while known serious patterns are grandfathered, command workers are outside its scan, and the guard's own tests are not explicitly run in CI.  
Evidence: `make scale-guard` reports 51 baselined offenders across 23 groups. `tools/scale-guard/scaleguard.go:109-120` roots scanning under backend internals and does not cover `backend/cmd/obligation-sweeper`, missing C35-004. `.github/workflows/ci.yml:33` runs `make scale-guard`, but does not run `cd tools/scale-guard && go test`; manual self-tests pass.  
Prod reachability: Every PR relying on the scale gate.  
Failure scenario: A duplicate/per-item worker loop or regression inside a baselined group merges because counts do not exceed the allowance and the relevant tree is unscanned.  
Business impact: Recurring scale regressions with a green badge.  
Root-cause-or-band-aid verdict: Baseline enables adoption but cannot be treated as scale certification.  
Counterargument: Baselines intentionally block only new debt and Go tests may be covered by a broader module test.  
Why it survives / why downgraded: The user-facing output is green despite critical known debt and a confirmed unscanned worker; P2 weak/false-green guard.  
E2E / guardrail status: false-green by C35-004 and the 51-debt result.  
Fix sketch: Scan all production Go roots, ratchet priority debt to zero on deadlines, run self-tests explicitly, and distinguish “pass with debt” from scale-certified.  
Guardrail needed: Fixture for command-worker loops, baseline age/priority budget, coverage manifest, and explicit guard test job.

### C35-021

ID: C35-021  
Priority: P3  
Title: Architecture graph is structurally stale for this review range  
Status: open  
Origin: prior-ledger / tooling  
Verdict: CONFIRMED  
Prior mapping: BUG-029  
Layman explanation: The map reviewers use predates most of the roads changed in these 35 commits.  
Evidence: Understand graph metadata was based at `03a0520e`; deterministic preflight against `d2a7fbcf` classified 116 relevant changed files, 107 structural, and required `FULL_UPDATE`. CRG impact also reported thousands of changed/impacted nodes. Per tool instructions, no incremental patch was trusted.  
Prod reachability: Review/tooling only; it affects defect detection, not runtime.  
Failure scenario: A reviewer trusts stale call/impact paths and misses a sibling adapter or newly introduced route.  
Business impact: Lower review reliability and repeated regressions such as duplicate bridges.  
Root-cause-or-band-aid verdict: Incremental navigation is insufficient after a structural range.  
Counterargument: Direct filesystem proof was used, so this audit is not invalidated.  
Why it survives / why downgraded: Correct; it has no runtime reachability and neither auditor used the stale graph as proof. It is review-tool maintenance, therefore P3.  
E2E / guardrail status: stale evidence explicitly quarantined.  
Fix sketch: Run a full graph rebuild at current HEAD and re-run change/impact detection before the next architecture-wide review.  
Guardrail needed: Session/review gate that reports graph baseline SHA and blocks proof claims when structural refresh is required.

### C35-022

ID: C35-022  
Priority: P2  
Title: Corrupt Android cache blobs become false loading or empty state forever  
Status: open  
Origin: prior-ledger  
Verdict: CONFIRMED  
Prior mapping: BUG-031  
Layman explanation: If one cached JSON row cannot be decoded after an app/schema change, the app quietly treats it as no data and never quarantines the bad row.  
Evidence: `CalendarRepository.kt:116`, `ControlTowerRepository.kt:112`, and `ExecutionRepository.kt:192-204` use `runCatching { decodeFromString(...) }.getOrNull()` with no delete/quarantine/error state. Similar patterns exist in adherence/insight/bootstrap caches.  
Prod reachability: App upgrades, partial writes, disk corruption, or DTO changes on any cached surface.  
Failure scenario: A stale blob repeatedly decodes to null; UI shows loading/empty while refresh is unavailable, and every observation retries the same corrupt data.  
Business impact: Misleading offline state and inability to see cached farm work.  
Root-cause-or-band-aid verdict: Off-main decoding fixed jank, not corruption recovery.  
Counterargument: A successful network refresh overwrites the row.  
Why it survives / why downgraded: Offline-first behavior must handle the exact case where refresh cannot rescue it; P2 correctness edge case.  
E2E / guardrail status: missing; cache tests cover valid decode, not corrupted/schema-old rows.  
Fix sketch: Version envelopes, classify decode errors, quarantine/delete corrupt rows, surface stale/error state, and attempt bounded migration/refetch.  
Guardrail needed: Repository contract tests injecting corrupt and old-schema JSON in offline and online modes.

### C35-023

ID: C35-023  
Priority: P2  
Title: Process Integrity projection pruning is unbudgeted and hides database failure  
Status: FIXED WITH PROOF
Origin: prior-ledger  
Verdict: CONFIRMED  
Prior mapping: BUG-033  
Layman explanation: Cleanup loops until all old rows are gone, and if the database errors it silently stops without recording why.  
Evidence: `backend/internal/processintegrity/adapters/postgres/repository.go:399-434` uses an unbounded `for` over delete batches; `:428-429` returns silently on error. There is no total row/time/page budget or observable result.  
Prod reachability: Every successful projection-version replacement with old rows.  
Failure scenario: Millions of obsolete rows monopolize a worker/request context; cancellation or DB failure leaves storage debt with no alert, increasing future read/write load.  
Business impact: Background DB pressure and invisible projection hygiene failure.  
Root-cause-or-band-aid verdict: Batched deletes limit each statement but do not bound the operation or make failure durable.  
Counterargument: Context timeout eventually bounds it and cleanup is best-effort.  
Why it survives / why downgraded: Timeout is not a progress contract and silent failure prevents recovery; P2 bounded operational debt.  
E2E / guardrail status: missing; no large-prune timeout/retry/telemetry story.  
Fix sketch: Move pruning to a durable job with per-run row/time budget, checkpoint/progress, retry, and error metric/audit.  
Guardrail needed: Million-row prune test with fixed work budget and injected DB-error observability assertion.
Fix/proof: Pruning has explicit work/time bounds and returns cancellation/DB/budget errors. A maintenance failure after a serving projection commit records yellow/error state without incorrectly failing the last-known-good serving projection. Unit and real-Postgres failure/cancellation paths pass.

### C35-024

ID: C35-024  
Priority: P2  
Title: Generic domain-consumer side effects can replay after processed-state finalization failure  
Status: open  
Origin: prior OCK recheck  
Verdict: PLAUSIBLE  
Prior mapping: OCK-053; M5 addendum; remainder of BUG-012 root concern  
Layman explanation: The consumer may finish the real work, fail while stamping “done,” and later run the work again.  
Evidence: `backend/internal/domainconsumer/app/service.go:182-209` publishes handler side effects before `MarkProcessed`; `backend/internal/domainconsumer/adapters/postgres/processed_events.go:46-60` reclaims `failed` rows immediately and stale `processing` rows after 15 minutes. The handoff's M5 explicitly says side effects are not co-transactional and deterministic idempotency for every handler remains tracked.  
Prod reachability: All durable domain-event handlers using this generic consumer/store.  
Failure scenario: Handler commits side effects; processed-store finalization fails; `MarkFailed` succeeds or claim goes stale; redelivery reclaims and invokes a non-idempotent side effect twice.  
Business impact: Potential duplicate downstream work/notifications/state changes.  
Root-cause-or-band-aid verdict: NACK/failure evidence fixed the ACK bug, but atomicity/idempotency root cause is only partially addressed.  
Counterargument: Handlers are expected to be idempotent; current E2E finalization story passes for the vaccination handler.  
Why it survives / why downgraded: No specific non-idempotent current handler was proven in this audit, so this is PLAUSIBLE P2 rather than P0/P1.  
E2E / guardrail status: partial; Story AI covers one path and finalization failure/NACK, not replay of every handler after committed side effects.  
Fix sketch: Give each handler a transactional inbox/outbox boundary or prove/store semantic idempotency at every side effect before marking OCK-053 globally fixed.  
Guardrail needed: Registry-driven replay tests for every handler, including “side effect committed, MarkProcessed failed” fault injection.

### C35-025

ID: C35-025  
Priority: P3  
Title: Operations keyset is stable but human-visible order appears random  
Status: FIXED WITH PROOF
Origin: current backlog  
Verdict: CONFIRMED  
Prior mapping: pending handoff A2  
Layman explanation: Parks and sheds are sorted by hidden UUIDs rather than the names operators see.  
Evidence: `backend/internal/vaccinationexecution/adapters/postgres/repository.go` Operations cohort keyset orders by `park_uuid, shed_uuid, stage`; `context/execution/pending-issues-handoff-2026-07-12.md:30-36` records the exact open behavior.  
Prod reachability: Operations list ordering.  
Failure scenario: Rows are complete and stable but appear in unintuitive order, slowing manual lookup.  
Business impact: Minor operator friction only.  
Root-cause-or-band-aid verdict: Pagination correctness is fixed; presentation ordering remains.  
Counterargument: UUID ordering is deterministic and efficient.  
Why it survives / why downgraded: It is maintainability/UX only, correctly P3.  
E2E / guardrail status: covered for pagination stability; missing human-order assertion.  
Fix sketch: Keyset on normalized park/shed names with UUID tie-breakers.  
Guardrail needed: Pagination test proving alphabetical order, duplicate-name stability, and no skipped/duplicated rows.
Fix/proof: Operations now keyset by case-insensitive visible park/shed names, exact names, UUID tie-breakers, then stage. The opaque cursor carries those values and old ID-only cursors are resolved compatibly. Real-Postgres pagination proof deliberately assigns Zulu a lower UUID and verifies alphabetical pages with no skip/duplicate.

### CL-004

ID: CL-004  
Priority: P3  
Title: Domain consumer allocates an unused claim token  
Status: open  
Origin: introduced in reviewed range  
Verdict: CONFIRMED  
Prior mapping: Claude-added CL-004; commit `67e27b58`  
Layman explanation: Every event creates a random claim identifier that no code ever reads.  
Evidence: `backend/internal/domainconsumer/app/service.go:44` declares `ProcessedEvent.ClaimToken`; `:155` assigns `uuid.NewString()`. Repository-wide `rg ClaimToken` finds only those two lines. `git blame` attributes the assignment to `67e27b58`.  
Prod reachability: Every domain event processed through this service, though the value has no effect.  
Failure scenario: No runtime correctness failure; the dead field suggests claim fencing exists when it does not and adds needless allocation/confusion.  
Business impact: Maintainability only.  
Root-cause-or-band-aid verdict: Leftover from the processed-event finalization fix, not an implemented claim-token fence.  
Counterargument: It may be reserved for future fencing.  
Why it survives / why downgraded: Future intent is not current behavior; P3 is appropriate because no runtime effect exists.  
E2E / guardrail status: Not applicable to behavior; dead-code tooling did not flag the unused struct field.  
Fix sketch: Remove the field/UUID allocation, or persist and compare it atomically if claim ownership fencing is actually required.  
Guardrail needed: Static dead-field/unused-write check for production Go structs where practical.

### MOB-001

ID: MOB-001  
Priority: P1  
Title: Offline submission still requires a fresh task network read  
Status: open  
Origin: dedicated mobile architecture pass / documented P1 backlog  
Verdict: CONFIRMED BY CODEX AND CLAUDE (independently counter-reviewed at HEAD)  
Prior mapping: distinct from C35-006; C35-006 is lost route identity, this is the missing Room task read model  
Layman explanation: The app can queue a write offline, but it cannot open the task it needs to create that write unless the network works first.  
Evidence: `TasksRepository.kt:32-47` is a direct `AppApi` pass-through with no entity, DAO, observable, or refresh/upsert path. `SubmitViewModel.kt:90-116` calls `repo.tasks(limit = 1)` on open; failure clears `task` and `clearSavedSubmission()` and replaces the screen with an error. This directly violates `android-offline-first.md:12-30`; `mobile-fetch-fix-backlog.md:68-71` already classifies Tasks Room/cursor work as P1.  
Prod reachability: Core Scan → Submit operator flow after process death, cold start, or loss of connectivity.  
Failure scenario: An operator finishes scanning in a shed, the process is recreated with poor connectivity, and Submit cannot recover the assigned task even though the outbox is healthy.  
Business impact: Core field work cannot be recorded in the exact offline condition the architecture promises to support.  
Root-cause-or-band-aid verdict: Offline writes exist, but the read prerequisite is still network-only.  
Counterargument: The task is normally fetched while the operator is online and the outbox itself survives restarts.  
Why it survives / why downgraded: Current code does not persist that task, and it deletes saved submission identity on GET failure; normal connectivity is not an offline guarantee. P1 because the core workflow is blocked, not because data is lost server-side.  
E2E / guardrail status: missing; no cold-offline/process-recreation task recovery test.  
Fix sketch: Persist task summary/detail/form per principal and row version in Room; route by stable task identity; UI observes Room and refresh only upserts it.  
Guardrail needed: Process-death + airplane-mode Scan → Submit test proving zero prerequisite GETs and a durable enqueue.

### MOB-002

ID: MOB-002  
Priority: P1  
Title: Required recording forms and proof cannot be captured, so real submissions are rejected  
Status: open  
Origin: dedicated mobile workflow pass / partial side-branch implementation absent at HEAD  
Verdict: CONFIRMED BY CODEX AND CLAUDE (independently counter-reviewed at HEAD)  
Prior mapping: distinct from C35-006 and C35-011  
Layman explanation: The app has form-rendering code, but the production Submit screen never uses it and always sends an empty form with no proof.  
Evidence: `SubmitViewModel.kt:47-52` explicitly says answers/proof are not wired; `:151` creates `SubmitTaskRequestDto` without answers or proof refs. Production `rg` finds `FormRunner(` only at its declaration; `TasksRepository.taskDetail()` is never called by a production consumer. `SubmitScreen.kt:185-209` renders summary/vaccine groups only. The side-branch snapshot `310b8969` contains a pinned task detail, durable scan draft, form answers, and proof refs, but is not an ancestor of HEAD.  
Prod reachability: Any SOP task whose published form has a required answer or required proof.  
Failure scenario: Operator taps Submit; the outbox works correctly, server validates the empty payload, and the task moves to conflict/rejection with no UI path to supply the missing fields.  
Business impact: The central vaccination recording flow cannot complete for realistic SOPs.  
Root-cause-or-band-aid verdict: Honest error messaging exposes the gap but does not implement the workflow.  
Counterargument: A form with no required fields can ACK.  
Why it survives / why downgraded: Production vaccination SOPs requiring evidence are the important case; success for empty forms does not close the core path. P1 rather than P0 because the server rejects instead of accepting bad medical data.  
E2E / guardrail status: false-green; FormRunner screenshots/parser tests do not prove production navigation or payload wiring.  
Fix sketch: Pin task/form identity through L3, render FormRunner, persist answers/draft/proof state, enqueue completed proof refs, and fail closed on identity/version mismatch.  
Guardrail needed: Android E2E for required text/number/select/video fields, process recreation, offline proof queue, conflict, rework, and exact submitted payload.

### MOB-003

ID: MOB-003  
Priority: P1  
Title: L2 vaccination execution silently stops at the first backend page  
Status: open  
Origin: dedicated L0→L3 pagination pass / documented P1 backlog  
Verdict: CONFIRMED BY CODEX AND CLAUDE (independently counter-reviewed at HEAD)  
Prior mapping: separate from C35-006's L3 scan issue  
Layman explanation: The backend says there are more execution rows, but Android throws away that cursor and has no Load More action.  
Evidence: Backend `ExecutionResponse` includes `totalCount` and `nextCursor` (`backend/internal/vaccinationexecution/domain/types.go:93-98`) and the handler defaults to 200 (`handler.go:183-188,217-228`). Android `VaccinationExecutionResponseDto` models only `source` and `rows` (`ExecutionDto.kt:53-56`); `AppApi.listVaccinationExecution`/Retrofit expose no cursor (`AppApi.kt:140-147`, `NetworkModule.kt:67-74`). `ShedsViewModel.kt:50-62` observes/refreshes one response with neither page size nor continuation, and `ShedsEvent` has no load-more intent.  
Prod reachability: Vaccination landing/L2 at more than one execution page across parks/sheds/cohorts/rules.  
Failure scenario: Row 201+ never reaches Room or UI; operators see incomplete due counts/sheds and have no indication data was truncated.  
Business impact: Work can be missed and displayed progress can be wrong at scale.  
Root-cause-or-band-aid verdict: Backend keyset work exists; Android/OpenAPI projection and screen wiring are incomplete.  
Counterargument: A normal park may have fewer than 200 execution rows.  
Why it survives / why downgraded: The product target is 1–5M animals and the accepted rule requires ~20-row pagination at every drill level; an undocumented cardinality assumption cannot suppress returned `nextCursor`.  
E2E / guardrail status: missing; no >200 Android list test or cursor contract test.  
Fix sketch: Model cursor/total, request ~20, persist per-item pages in Room, expose PagingSource/RemoteMediator or an equivalent bounded keyset window, and prefetch near the end.  
Guardrail needed: >2-page L2 integration test asserting every row appears once across recreation/offline and no request exceeds 20.

### MOB-004

ID: MOB-004  
Priority: P1  
Title: Calendar page 2 bypasses Room and disappears offline or after process death  
Status: open  
Origin: dedicated Room SSOT pass / documented pagination rule  
Verdict: CONFIRMED BY CODEX AND CLAUDE (independently counter-reviewed at HEAD)  
Prior mapping: BUG-015 fixed cursor consumption only; this is the still-broken on-device SSOT half  
Layman explanation: The first 20 Calendar rows come from the local database, but every later page lives only in temporary screen memory.  
Evidence: `CalendarViewModel.kt:70-75` stores continuation rows/cursors in plain fields; `:236-278` calls `repo.events()` directly and appends to those fields. `CalendarDayViewModel.kt:43-47,80-95` repeats the same pattern. Only first-page `refreshEvents()` upserts Room. The accepted rule says both network and Room must page identically and the UI must never render direct network results (`mobile-data-fetch-anti-patterns.md:34-58`).  
Prod reachability: Calendar day/history lists longer than 20.  
Failure scenario: User loads page 2 online, backgrounds the app, process is killed, then reopens offline; only page 1 remains. A failed continuation can never be satisfied from Room.  
Business impact: Incomplete history/work lists and a false offline-first promise.  
Root-cause-or-band-aid verdict: Cursor UI was added, but persistence stopped at page 1.  
Counterargument: Continuation pages are optional transient browsing state.  
Why it survives / why downgraded: Room is explicitly the on-device UI source of truth, and the rules forbid direct-network page rendering. The rows are business history/work, not disposable search suggestions.  
E2E / guardrail status: missing; current tests prove cursor use, not Room-backed restoration.  
Fix sketch: Store calendar events per item/page scope and drive all pages from a bounded Room PagingSource/RemoteMediator; never append direct API DTOs into ViewModel state.  
Guardrail needed: Load two pages, kill process, disable network, reopen, and assert the same ordered rows with no duplicate or direct-network dependency.

### MOB-005

ID: MOB-005  
Priority: P1  
Title: Production Scan and Submit states render mock farm data as if it were real  
Status: open  
Origin: dedicated truthful empty/loading-state pass  
Verdict: CONFIRMED BY CODEX AND CLAUDE (independently counter-reviewed at HEAD)  
Prior mapping: new root finding  
Layman explanation: While data is missing or the network fails, the real app can show “Gandhi 1”, 12/40 vaccinated, and a fake date from the screenshot fixture.  
Evidence: `ScreenSamples.kt:126-146` contains operational Scan values (`Gandhi 1`, 12/40, 26 pending, `lastSyncedAt=now`); `ScanViewModel.kt:50-58` initializes production state from it and a null Room emission preserves it (`:95-113`). `ScreenSamples.kt:149-173` contains a fake Submit shed/cohort/date; `SubmitViewModel.loadingState`, `blockedState`, and `errorState` (`:277-321`) clear groups but do not clear those identity fields. Other screens have explicit honest placeholder builders, so this is not an intentional global convention.  
Prod reachability: Scan cold cache/no shed and Submit loading/no task/task GET failure.  
Failure scenario: A field operator sees convincing counts/identity from the gallery fixture and acts on them or reports the wrong shed state.  
Business impact: False operational data on a medical workflow screen.  
Root-cause-or-band-aid verdict: Sample fixtures leaked into production state construction instead of being confined to previews/tests.  
Counterargument: The state is replaced quickly on a successful cache/network response and Submit disables the button in error/loading states.  
Why it survives / why downgraded: Cold/offline failure is exactly when replacement never comes, and visible fake farm identity/counts remain misleading even with a disabled button.  
E2E / guardrail status: false-green; screenshot tests intentionally assert sample states, with no production cold/error truthfulness test.  
Fix sketch: Use zero-data production constructors for every loading/empty/error state; keep samples under debug/test/preview sources only.  
Guardrail needed: Static ban on `sample*State()` from production ViewModels plus cold-cache/no-route/error UI tests asserting no fixture identifiers or counts.

### MOB-006

ID: MOB-006  
Priority: P1  
Title: Successful outbox rows are never pruned and the whole table is held for app lifetime  
Status: open  
Origin: dedicated memory/offline queue pass / documented P1 backlog  
Verdict: CONFIRMED BY CODEX AND CLAUDE (independently counter-reviewed at HEAD)  
Prior mapping: distinct from C35-001 logout isolation and C35-017 read-cache retention  
Layman explanation: Every successful mobile write stays in the phone database forever, and the app rereads/maps all of them whenever one row changes.  
Evidence: `OutboxDao.kt:58-59` is `SELECT * ... ORDER BY` without LIMIT; DAO/store expose no prune/delete except state transitions (`:63-97`, `OutboxStore.kt:14-37`). `DefaultSyncRepository` collects that full table in application scope forever (`SyncRepository.kt:107-117`), and `toSyncStatus()` maps every terminal row into `items` (`OutboxStore.kt:80-91`). The whole-tree mobile guard flags this exact DAO.  
Prod reachability: Every long-lived installation that submits/reschedules/uploads proof.  
Failure scenario: Months of successful work grow Room indefinitely; each new event rematerializes and maps thousands of terminal rows, increasing storage, heap, GC, and startup/overlay cost.  
Business impact: Progressive slowdown/process death on low-end field phones and an ever-growing sensitive audit footprint.  
Root-cause-or-band-aid verdict: Drain batching bounds active dispatch, not terminal retention or UI observation.  
Counterargument: Successful rows are useful local history and individual records are small.  
Why it survives / why downgraded: The UI needs counts plus a bounded recent window, not the entire historical payload table; no cap exists. The repo's own P1 backlog specifies grouped counts, active rows, recent terminals, and prune.  
E2E / guardrail status: false-green; drain tests cover state transitions, not 10k-row retention/heap.  
Fix sketch: Query status aggregates separately, observe only active + bounded recent terminal rows, prune SUCCEEDED rows under a retention policy, and keep failed/conflict work durable.  
Guardrail needed: 10k-success stress test with fixed DB/heap/query-time ceilings and assertions that queued/in-flight/failed rows are never pruned.

### MOB-007

ID: MOB-007  
Priority: P2  
Title: Timetable and coverage are network-only and lose truthful state offline  
Status: open  
Origin: dedicated screen-read inventory / documented P1 implementation backlog  
Verdict: CONFIRMED BY CODEX AND CLAUDE (independently counter-reviewed at HEAD)  
Prior mapping: new root finding  
Layman explanation: The HR timetable survives only in memory, and a coverage network error looks exactly like “you have no coverage.”  
Evidence: `RosterRepository.kt:25-31` is a thin API pass-through with no Room DAO/Flow. `TimetableViewModel.kt:55-69` keeps prior rows only in the current process and cold failure becomes `load_failed`. `CoverageBannerViewModel.kt:35-41` turns every exception into null, the same state as `hasCoverage=false`. This violates the network-only read ban (`android-offline-first.md:36-43`).  
Prod reachability: Timetable screen and Calendar coverage banner on weak/no connectivity or process restart.  
Failure scenario: An operator loses connectivity; the timetable blanks on cold start and an active temporary coverage assignment silently disappears.  
Business impact: Misleading staffing/coverage awareness and a degraded secondary workflow.  
Root-cause-or-band-aid verdict: In-memory refresh preservation is a band-aid; there is no durable SSOT or distinct error/stale state.  
Counterargument: Roster/coverage are read-only informational surfaces and center seat counts are usually small.  
Why it survives / why downgraded: Small cardinality does not justify network-only state, but the bounded/informational impact supports P2 rather than P1.  
E2E / guardrail status: missing; no Room/offline/process-death tests.  
Fix sketch: Principal-scoped Room entities/flows for timetable and coverage, background refresh, explicit stale/offline state, and a certified bound or cursor for seats.  
Guardrail needed: Two-account + process-death offline tests distinguishing “no coverage” from “coverage unknown/stale.”

### MOB-008

ID: MOB-008  
Priority: P1  
Title: Leadership data gaps are silently capped at 50 and have no continuation  
Status: open  
Origin: dedicated list-cardinality pass / documented P1 backlog  
Verdict: CONFIRMED BY CODEX AND CLAUDE (independently counter-reviewed at HEAD)  
Prior mapping: not C35-011; that row covers verify/rework actions  
Layman explanation: Directors can see at most 50 animals with bad identity data even when the server says more exist.  
Evidence: `LeadershipViewModel.kt:256-259` hardcodes `GAPS_LIMIT=50` and `COVERAGE_LIMIT=50`; observe/refresh uses those values (`:76-84,124-158`). `VaccinationGapsResponseDto` models `nextCursor` (`VaccinationGapsDto.kt:28-34`), but `applyGapsResource()` maps only rows and discards it (`LeadershipViewModel.kt:173-185`); no load-more event exists. Whole-tree mobile guard flags both 50-row constants.  
Prod reachability: Leadership Data Gaps overlay with more than 50 excluded animals; coverage additionally overfetches beyond the ~20 mobile page rule.  
Failure scenario: Animal 51+ never appears, and the UI presents the first page as the complete gap population.  
Business impact: Leadership can miss animals excluded from vaccination coverage and make incomplete cleanup decisions.  
Root-cause-or-band-aid verdict: Overlay scrolling was fixed in `d0cd5e38`; data pagination was not.  
Counterargument: The KPI still shows aggregate open-gap count and 50 may be enough for triage.  
Why it survives / why downgraded: The sheet is a per-animal remediation list, not a sample; it neither labels truncation nor offers continuation.  
E2E / guardrail status: false-green; UI scroll tests do not cover >50 or nextCursor.  
Fix sketch: Use 20-row cursor pages persisted per item in Room, expose load-more/prefetch, and keep the aggregate count separate.  
Guardrail needed: 51+ gap test proving all pages, stable keys, process restoration, and no request above 20.

### MOB-009

ID: MOB-009  
Priority: P1  
Title: Calendar and Execution still decode large Room JSON blobs on the Main thread  
Status: open  
Origin: prior anti-pattern fix verification / documented P1 backlog  
Verdict: CONFIRMED BY CODEX AND CLAUDE (independently counter-reviewed at HEAD)  
Prior mapping: strengthens but does not duplicate C35-018's unbounded Scan collections  
Layman explanation: When Room emits cached Calendar, shed, record, or scan data, the phone parses the whole JSON response on the UI thread, where taps and frames must run.  
Evidence: `CalendarRepository.kt:94-96,114-118` and `ExecutionRepository.kt:119-127,149-179,190-205` use `map { decodeFromString(...) }` with no `flowOn`. Their ViewModels collect from `viewModelScope`, so the upstream work runs on the Main collector context. Commit `edcb668b` added `flowOn(Dispatchers.Default)` only to Adherence, Control Tower, and Insights; the repository's P1 backlog explicitly lists Calendar/Execution. Scan additionally decodes the current 1,000-row blob.  
Prod reachability: Every cached Calendar/Execution/Record/Scan emission, especially large sheds and app re-entry.  
Failure scenario: Room invalidation or refresh causes a large decode/map while the operator scans or scrolls, blocking frames and touch feedback on a low-end device.  
Business impact: Jank, missed/double taps, ANR risk, and failure of the 120ms scan-feedback/zero-dropped-frame budgets.  
Root-cause-or-band-aid verdict: The earlier performance fix was an incomplete repository sweep.  
Counterargument: Current blobs are bounded by endpoint limits and modern JSON parsing may be fast enough.  
Why it survives / why downgraded: Current Scan explicitly requests 1,000, L2 defaults to 200, and the accepted budget targets 2–3GB weak devices; heavy transforms on Main are prohibited regardless of average desktop timing.  
E2E / guardrail status: missing; no StrictMode/trace or frame-time assertion for cache emission.  
Fix sketch: Decode/map on `Dispatchers.Default`, then replace blob caches for large lists with typed/per-item Room paging so the work itself is bounded.  
Guardrail needed: Static repository-flow check plus Macrobenchmark/FrameTiming test that triggers Room updates on a low-end profile.

### MOB-010

ID: MOB-010  
Priority: P2  
Title: Screen read flows keep collecting while their navigation entries are backgrounded  
Status: open  
Origin: dedicated lifecycle/battery pass / documented cross-cutting backlog  
Verdict: CONFIRMED BY CODEX AND CLAUDE (independently counter-reviewed at HEAD)  
Prior mapping: new cross-cutting finding  
Layman explanation: Leaving a screen on the back stack stops drawing it, but its ViewModel continues observing Room and rebuilding state.  
Evidence: Alerts, Calendar, CalendarDay, Leadership, Overdue, Record, Scan, and Sheds ViewModels each launch `collectLatest` bridges in `viewModelScope`; only `SyncStatusViewModel` uses `stateIn(SharingStarted.WhileSubscribed(5_000))`. The accepted app-wide pattern says screen read state must be WhileSubscribed and never a forever collector (`mobile-fetch-fix-backlog.md:13-17,85-86`; `performance-and-memory.md:90`). Hot hardware flows are separately allowed and are not counted here.  
Prod reachability: Normal drill navigation where Calendar/Sheds/Scan/Leadership entries remain in the back stack.  
Failure scenario: Backgrounded VMs keep reacting to cache/outbox updates, decoding/mapping lists and consuming CPU/battery until popped.  
Business impact: Avoidable battery drain, heap retention, and recomposition/state work on low-end phones.  
Root-cause-or-band-aid verdict: UI uses lifecycle-aware collection, but the ViewModel-side bridge remains eager.  
Counterargument: `viewModelScope` cancels when the nav entry is cleared, and keeping state warm improves Back navigation.  
Why it survives / why downgraded: Back-stack entries can live for the full session; the explicit 5-second replay window provides warm Back behavior without permanent upstream work. P2 because this is cumulative resource waste, not immediate data corruption.  
E2E / guardrail status: missing; no subscription-count/battery/lifecycle assertion.  
Fix sketch: Expose repository flows as derived immutable state with `stateIn(WhileSubscribed(5_000))`; isolate explicit refresh commands and exempt only genuinely hot device streams.  
Guardrail needed: Navigation test asserting Room subscriptions stop shortly after screen STOP and restart with cached state on return.

### MOB-011

ID: MOB-011  
Priority: P2  
Title: Several dynamic Lazy lists still have no stable item keys  
Status: open  
Origin: dedicated Compose performance pass  
Verdict: CONFIRMED BY CODEX AND CLAUDE (independently counter-reviewed at HEAD)  
Prior mapping: Scan instances strengthen C35-018; other screens are new coverage  
Layman explanation: When rows are inserted, removed, or reordered, Compose identifies them by position instead of identity, causing extra work and potentially moving remembered row state to the wrong item.  
Evidence: `ScanScreen.kt:265` renders a prepended feed with `items(state.feed)` and `:752` uses unkeyed `itemsIndexed(filtered)`; Alerts uses index items (`AlertsScreen.kt:186`), RFID uses index items (`RfidScreen.kt:154`), Timetable uses index items (`TimetableScreen.kt:146`), and Submit groups are unkeyed (`SubmitScreen.kt:206`). Accepted rules require stable key + content type on every Lazy list (`mobile-fetch-fix-backlog.md:23-24`; `performance-and-memory.md:32-33,46-49`).  
Prod reachability: Scan feed/filters, alert refresh/reorder, reader discovery, roster changes, and form/group updates.  
Failure scenario: A new scan feed row is inserted at index 0; Compose invalidates/reuses slots by position across the list, increasing recomposition and risking row-local state association errors.  
Business impact: Avoidable jank on hot scan paths and subtle wrong-row UI state if interactive rows gain remembered state.  
Root-cause-or-band-aid verdict: Lazy rendering bounds composition count, but identity/content-type optimization is incomplete.  
Counterargument: Many current row composables are stateless and the lists are small.  
Why it survives / why downgraded: Scan feed/roster are dynamic hot lists and explicitly covered by the performance rule; P2 reflects performance/correctness risk rather than a currently reproduced wrong action.  
E2E / guardrail status: missing; no Compose metrics/recomposition or state-retention test for insert/reorder.  
Fix sketch: Supply backend/device stable IDs and content types for every dynamic Lazy item; avoid index identity.  
Guardrail needed: Static lint for unkeyed dynamic items plus Compose test that inserts/reorders rows and verifies identity/state retention and recomposition budget.

### NEW-E2E-001

ID: NEW-E2E-001  
Priority: P1  
Title: Control-tower batch-drive kernel E2E fails deterministically (alert row = 0) on the current tip  
Status: open  
Origin: found while gating C35-015 on `make ci-local` (surfaced by the go-test-./… gate)  
Verdict: CONFIRMED (deterministic, pre-existing)  
Prior mapping: none — new  
Layman explanation: A production-path kernel test that proves a batch vaccination drive raises exactly one control-tower alert now finds zero.  
Evidence: `backend/tests/e2e/story_c_batch_drive_verify_test.go:236` — "Control tower alert clears / still exactly one control-tower row for this shed/rule/batch: FAILED (rows=0)". Reproduces in isolation on clean `4bc951d6` (`go test ./tests/e2e/ -run TestKernelStoryC_BatchDriveVerifyControlTower`, 5.27s). Proven independent of C35-015: `git diff 4bc951d6 HEAD --name-only` touches only SOP + sqlc-snapshot + admin-client files — zero processintegrity/control-tower/e2e/migration paths, so the E2E and its migration-built test DB are byte-identical to `4bc951d6`.  
Prod reachability: Control-tower alert projection for batch drives — a leadership exception surface.  
Failure scenario: Either the control-tower alert row is no longer produced/persisted for a batch drive, or the assertion is time/date-sensitive (test ran 21:11 IST; India-business-calendar due/overdue windows are `now`-derived). Needs triage to tell a real projection regression from a flaky time-boundary assertion.  
Business impact: If real, batch-drive exceptions may not surface in Control Tower; if flaky, it red-locks the whole `ci-local` `go test ./...` gate for every change.  
Root-cause-or-band-aid verdict: Unfixed — untouched by this pass, recorded honestly rather than silently absorbed.  
E2E / guardrail status: RED on the current tip; blocks the ci-local go-test gate until triaged.  
Fix sketch: Reproduce with a pinned clock; if time-sensitive, stabilize the assertion window; if the alert row is genuinely missing, trace the control-tower projector for batch-drive alerts.  
Guardrail needed: Deterministic-clock harness for the kernel E2E so `now`-derived assertions are reproducible.

### FIXCHK-001

ID: FIXCHK-001  
Priority: P1  
Title: App vaccination routes still do not accept park-scoped grants  
Status: open  
Origin: post-fix fixed/not-fixed verification pass  
Verdict: CONFIRMED BY CLAUDE AND CODEX  
Prior mapping: reopens the app-route authorization half of the claimed park-scope fix  
Layman explanation: The app vaccination endpoints were supposed to work safely for park-scoped users, but the auth middleware still only lets scoped grants count for Calendar routes.  
Evidence: `backend/internal/platform/httpmiddleware/auth.go:284-286` returns true only for `strings.HasPrefix(route.Pattern, "/calendar/")`; the app vaccination routes in `backend/internal/permissions/routes.go:157-159` are `/app/vaccination/...` routes gated only by `AppBootstrap`; the repo-side filter in `backend/internal/vaccinationexecution/adapters/postgres/repository.go:26-29` returns unrestricted when no grants or a tenant-wide grant reaches the context. A park-scoped app grant therefore does not authorize these routes, while a broad bootstrap/tenant-wide path can still bypass the intended park clamp.  
Prod reachability: Field app scan roster, task option values, and reschedule endpoints under park-scoped operator grants.  
Failure scenario: A park-scoped operator either gets a false 403 for legitimate app vaccination work, or the path is opened through broad app/bootstrap authority without the scoped route contract being tested.  
Business impact: Park-level isolation is still not proven on vaccination execution app routes.  
Root-cause-or-band-aid verdict: Repo query filtering was added, but route authorization still treats scoped grants as calendar-only.  
Counterargument: The repository filter can clamp authorized parks if scoped grants reach the request context.  
Why it survives / why downgraded: The middleware is the earlier gate and the route pattern is not `/calendar/`, so the scoped grant does not become an effective app-route role in the first place.  
E2E / guardrail status: false-green; affected tests pass without proving a real app route authorized by a park-scoped grant and then clamped in SQL.  
Fix sketch: Extend scoped-grant eligibility to the intended `/app/vaccination/...` routes, or split app bootstrap from park-scoped vaccination execution permissions explicitly.  
Guardrail needed: Full router/auth tests for park-scoped app users: allowed same-park roster/options/reschedule, denied cross-park, denied no-scope, and no tenant-wide fallback.

### FIXCHK-002

ID: FIXCHK-002  
Priority: P2  
Title: FEFO option disabled-state still compares expiry against UTC day  
Status: open  
Origin: post-fix fixed/not-fixed verification pass  
Verdict: CONFIRMED BY CLAUDE AND CODEX  
Prior mapping: reopens the remaining FEFO business-day consistency gap  
Layman explanation: The SQL ranks vaccine lots using the India business date, but the option-disable reason still decides expiry using UTC midnight.  
Evidence: `backend/internal/vaccinationexecution/adapters/postgres/repository.go:1231-1259` derives `bizToday` from `biztime.DefaultLocation()` and passes it into the SQL FEFO ranking; the later option loop still marks `lot_expired` with `expiry.Time.Before(time.Now().UTC().Truncate(24*time.Hour))` at `repository.go:1274-1282`.  
Prod reachability: Vaccination task option values using `inventory.vaccine_lots.fefo` around IST/UTC day boundaries.  
Failure scenario: Around India midnight, SQL can rank a lot as usable for the business day while the response disables it as `lot_expired`, or vice versa depending on the boundary.  
Business impact: Field users can be blocked from the correct FEFO lot or shown inconsistent stock-option state.  
Root-cause-or-band-aid verdict: SQL ordering was fixed, but DTO availability/reason calculation kept the old UTC clock.  
Counterargument: The expiry date is date-only and most operators will not hit the UTC/IST boundary.  
Why it survives / why downgraded: The same endpoint now contains two date semantics for one business rule; rare boundary bugs are still medical inventory correctness defects.  
E2E / guardrail status: missing; no frozen-clock IST boundary test asserts SQL rank and option disabled-state agree.  
Fix sketch: Compare expiry against the same `bizToday` date value used by SQL and keep all FEFO option state date-only in `Asia/Kolkata`.  
Guardrail needed: Table-driven test across UTC 18:29/18:30 and IST midnight for active, today-expiring, yesterday-expired, and tomorrow-expiring lots.

### FIXCHK-003

ID: FIXCHK-003  
Priority: P3  
Title: Scan roster cursor test asserts the wrong response key  
Status: FIXED WITH PROOF
Origin: post-fix fixed/not-fixed verification pass  
Verdict: CONFIRMED BY CLAUDE AND CODEX  
Prior mapping: reopens the test-quality half of the cursor contract fix  
Layman explanation: The handler now writes `next_cursor`, but the test still checks `nextCursor`, so it does not prove the app-facing cursor contract.  
Evidence: `backend/internal/vaccinationexecution/adapters/http/handler.go:394-400` writes `response["next_cursor"]`; `backend/internal/vaccinationexecution/adapters/http/handler_test.go:298-304` decodes into a map and checks `body["nextCursor"] == ""`. That assertion is aimed at a key the handler no longer emits.  
Prod reachability: Any future regression in the scan roster cursor response shape can slip past this test.  
Failure scenario: The handler stops returning `next_cursor`, or returns only the old camelCase key, and the test remains a weak/incorrect signal instead of catching contract drift.  
Business impact: False confidence in the mobile pagination contract; a backend/client mismatch can ship.  
Root-cause-or-band-aid verdict: Production response was changed, but the assertion was not updated to the snake_case contract.  
Counterargument: Current production code emits the right key, so this is only a test bug.  
Why it survives / why downgraded: It is P3 because the runtime key is currently correct, but this was specifically claimed as cursor validation proof and the proof is weak.  
E2E / guardrail status: false-green; the passing unit test does not assert `next_cursor` non-empty.  
Fix sketch: Assert `body["next_cursor"]` is non-empty, assert `nextCursor` is absent, and preferably decode into the generated/contract DTO rather than a loose map.  
Guardrail needed: Contract test that fails on camelCase-only or missing `next_cursor`.
Fix/proof: The handler test now requires a non-empty `next_cursor`, rejects legacy `nextCursor`, decodes the cursor, and compares the decoded value with the expected domain cursor.

### FIXCHK-004

ID: FIXCHK-004  
Priority: P1  
Title: Android scan roster still ignores server cursor pagination  
Status: merged into C35-006/C35-018; not separately counted  
Origin: post-fix fixed/not-fixed verification pass  
Verdict: CONFIRMED BEHAVIOR; DUPLICATE ROOT  
Prior mapping: strengthens C35-006/C35-018 and MOB-003 with the exact Android transport gap  
Layman explanation: The backend can return a scan roster cursor, but the Android client neither reads it nor sends it back for the next page.  
Evidence: `apps/goatos-android/core/core-network/src/main/kotlin/sg/mesha/goatos/core/network/dto/ScanDto.kt:15-19` models only `source` and `rows`, no `next_cursor`; `apps/goatos-android/core/core-network/src/main/kotlin/sg/mesha/goatos/core/network/NetworkModule.kt:137-141` sends only `limit`, no cursor query; `apps/goatos-android/core/core-data/src/main/kotlin/sg/mesha/goatos/core/data/ExecutionRepository.kt:176-186` observes/refreshes cache only by shed+limit and calls `scanRoster(shedId, limit)` without a cursor.  
Prod reachability: Field scan roster on any shed larger than a single backend page, and any future server-side page-size reduction below the current overfetch.  
Failure scenario: The first page is cached/rendered as the whole roster; animal rows after the cursor are absent, or the app keeps relying on oversized `limit=1000` requests instead of bounded paging.  
Business impact: Incomplete medical scan lists and continued mobile memory/performance risk.  
Root-cause-or-band-aid verdict: Backend cursor emission landed, but Android transport/state/cache did not adopt the cursor contract.  
Counterargument: Current Android asks for a large limit, so most sheds may fit in page one.  
Why it survives / why downgraded: The accepted mobile rule is bounded ~20-row keyset pages through Room; a large one-shot fetch is exactly the anti-pattern already tracked in C35/MOB.  
E2E / guardrail status: false-green; current mobile guard catches oversize constants but there is no Android test proving cursor read/send/cache/load-more.  
Fix sketch: Add `next_cursor` to `ScanRosterResponseDto`, add a `cursor` Retrofit query, key Room pages by cursor/window, and expose load-more/prefetch from the scan ViewModel.  
Guardrail needed: Android integration test with a >2-page roster asserting every goat appears once across process death/offline and no scan roster request exceeds the mobile page budget.

## 7. Dropped / Countered / Out-of-Scope Findings

- Dropped as fixed with current proof: BUG-012 (ACK/finalization behavior), BUG-013 (verification >200), BUG-015 (Calendar page consumption), and pending C1 `RoleManager` compile failure.
- Countered and not re-promoted: BUG-001, BUG-004 through BUG-009, BUG-034; OCK-006, OCK-008, OCK-025, OCK-041, OCK-076, OCK-077. Their active seed/fail-closed/source-scope evidence was not contradicted.
- Out of current checkout: BUG-027 inventory-only row; side-branch/stash A3/D work; mobile FCM SDK work gated on the production Firebase project.
- External/product gates, not local defects: OCK-044, OCK-045, OCK-050. They remain launch prerequisites; this classification does not excuse C35-003's checked-in staging omission or C35-012's unavailable gate.
- Not promoted: herd projection migration's backfill-before-trigger write gap is evidence strengthening C35-005, not a second root bug while no production reader exists.
- Not promoted: Android local Gradle failure due to missing Java. It is a verification limitation, not an application defect.
- Not promoted as confirmed: a specific duplicate side effect under OCK-053. The generic risk is C35-024 PLAUSIBLE until a non-idempotent handler/fault reproduction is supplied.
- **CL-001 countered:** `rule_id` is tenant-unique and functionally determines `dose_code`, protocol version, and protocol name; grouping by those redundant attributes cannot create two rows with the same `(park,shed,rule,batch)` cursor key. NULL-batch obligations for one rule are aggregated into one row. The claimed page-boundary skip is unreachable under current schema/query invariants.
- **CL-002 countered:** the anchor is already an IST `YYYY-MM-DD` from `todayIso`; `Date.UTC` is used only as pure date arithmetic, and the backend reinterprets the resulting keys in `Asia/Kolkata`. No UTC instant decides a business date.
- **CL-003 countered as speculative:** no bypass exists in current E2E artifacts; both the guard and adversarial self-test pass. Intentional alias/dynamic evasion remains optional defense-in-depth work, not an open product or false-green finding.
- **FIXCHK-004 merged, not dropped:** the Android cursor transport gap is real and its evidence is now part of C35-006/C35-018. Keeping a second P1 would count one Room/paging root defect twice.

## 8. Deduped Guardrail Backlog

| Guardrail | Findings closed if effective | Required behavior |
| --- | --- | --- |
| Android destructive-logout certification | C35-001 | Wipe-all inventory: both Room DBs, all DataStore/SharedPreferences keys, WorkManager, files/media/cache, SavedState/drafts, app-scope/singleton/bootstrap/nav state, Firebase credentials, and server device/FCM binding. Seed every surface, logout, restart offline, switch accounts, prove zero survivors or transmissions. New persistence/job types must register in the inventory. |
| App scoped-grant route certification | FIXCHK-001 | Exercise real `/app/vaccination/...` routes through middleware with park-scoped, tenant-wide, no-scope, same-park, and cross-park principals; prove scoped grants authorize only intended app routes and SQL clamps to authorized parks. |
| Room-SSOT + paging architecture test | C35-006, C35-017, C35-019, MOB-001, MOB-003, MOB-004, MOB-007, MOB-008 | Every screen read is network->Room->Flow->UI; ~20-row matching keyset pages; process-death/offline restoration; no direct-network continuation, whole response blob, silent truncation, missing cursor transport, or network-only repo. |
| Current-SHA Android required check | C35-006, C35-008, C35-011, C35-018, C35-019, MOB-001…MOB-011 | Compile, unit/UI/navigation tests, lint, Room migrations, baseline profile, macrobenchmark, Compose metrics, lifecycle/leak/heap assertions, and cursor-pagination contract coverage. Missing/skipped is failure. |
| Whole-tree mobile list guard | C35-006, C35-017, C35-018, MOB-003, MOB-006, MOB-008, MOB-011 | CI must run `--all`; allow only explicit, expiring exceptions. Cover oversized network/Room pages, `SELECT *` observed lists, missing cursor consumption/load-more, absent stable keys/content types, and paging contract drift. |
| Mobile workflow truthfulness E2E | C35-006, C35-011, MOB-001, MOB-002, MOB-005 | Stable task/shed/batch/version route identity; no sample fixture data; required form/proof capture; offline draft/enqueue; process death; role-scoped verify/rework; exact payload and truthful conflict/error/empty states. |
| Low-end-device performance/lifecycle certification | C35-018, MOB-006, MOB-009, MOB-010, MOB-011 | Off-Main parse/map, WhileSubscribed screen flows, O(1) scan matching, bounded feed/outbox, stable-key Lazy lists, flat heap/battery, scan feedback ≤120ms, zero dropped frames. |
| Business-day inventory semantics | FIXCHK-002 | Freeze the clock around IST/UTC boundaries and require FEFO SQL rank, response availability, disabled reasons, and displayed expiry state to use one `Asia/Kolkata` business-date contract. |
| API response-shape contract assertions | FIXCHK-003 | Backend tests must assert exact generated/contract field names, including snake_case cursor keys, and fail when only stale camelCase keys are present. |
| Deployment-shaped sweeper test | C35-003, C35-004, C35-007 | Parse real Terraform/env, require actor, execute 1,000-batch finalization, assert bounded queries/transactions and EXPLAIN plan. |
| 1M/5M current-SHA read certification | C35-002, C35-005, C35-007, C35-009, C35-013 | Real cardinality, projection-unavailable case, query count, p95/p99, memory, DB plan; report generated from same SHA artifact. |
| Cross-boundary fanout static guard | C35-004, C35-013, C35-014, C35-015, C35-016, C35-020 | Scan `backend/cmd` and SSR; detect loop/map/Promise.all calls with cardinality; run self-tests explicitly. |
| Clinical authoring property/E2E matrix | C35-010 | Explicit policy for sick/under-treatment/quarantine/ICU, cap=0, recovery/reopen, partial/missing inputs. |
| Domain-handler replay certification | C35-024 | For every registered handler, inject finalization failure after side-effect commit and prove semantic idempotency/transactional inbox. |
| Projection lifecycle job test | C35-002, C35-023 | Durable bounded rebuild/prune, last-known-good serving, error/progress telemetry, retry. |
| Graph freshness gate | C35-021 | Compare graph SHA to HEAD and require full rebuild for structural-change threshold before architecture proof. |
| Release-system availability monitor | C35-008, C35-009, C35-012 | Alert on startup failure/zero jobs; branch protection blocks missing checks; retain current-SHA evidence. |

## 9. Final Summary Table

The common ledger contains the original 40 counted findings plus NEW-E2E-001, discovered during closure gating: **41 tracked, 12 fixed with proof, 29 open**. FIXCHK-004 remains merged into C35-006/C35-018 and is not double-counted. Claude additions CL-001…003 remain countered; CL-004 survives open. Every retained row is peer-reconciled.

| Priority | Fixed with proof | Confirmed open | Plausible open | Open total |
| --- | ---: | ---: | ---: | ---: |
| P0 | 1 | 1 | 0 | 1 |
| P1 | 4 | 16 | 0 | 16 |
| P2 | 5 | 9 | 1 (C35-024) | 10 |
| P3 | 2 | 2 | 0 | 2 |
| **Total** | **12** | **28** | **1** | **29** |

**Fixed counted rows:** C35-003, C35-004, C35-007, C35-008, C35-010, C35-014, C35-015, C35-016, C35-020, C35-023, C35-025, and FIXCHK-003. **Still partial/open:** C35-002, C35-005, C35-013. NEW-E2E-001 is open and red-locks the broad Go gate until separately repaired. C35-012 remains an open remote-release-availability finding; it does not invalidate green local execution of the same checked-in job runner.

**Three guardrails carry the most leverage:** destructive logout certification (C35-001); current-SHA Room/paging/workflow/performance Android certification (C35-006/008/011/017/018/019 plus MOB-001…011); and current-SHA scale/latency + cross-boundary fanout gates (C35-002/004/005/007/009/013/014/015/016/020). FIXCHK-001 also needs a dedicated app scoped-grant route matrix.

Final state: **ONE COMMON LEDGER. 41 tracked; 12 fixed; 29 open (1 P0, 16 P1, 10 P2, 2 P3).** The original 26 C35/CL rows, 11 MOB rows, 3 counted FIXCHK rows, and NEW-E2E-001 are reconciled here; FIXCHK-004 is merged and not counted twice. The audit STOP RULE applied only to the original audit; the closure program now authorizes these evidence-bound fixes.
