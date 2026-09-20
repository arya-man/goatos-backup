# PostgreSQL bind-contract guard — 2026-09-21

## Goal

Prevent missing, extra, gapped, or conditionally-pruned PostgreSQL parameters from reaching review, build, push, or `main`. The protection must derive the SQL/argument contract instead of hardcoding today's placeholder count, cover existing risk as far as safely possible, ratchet future changes, and be documented in agent review/build guidance.

## Scope

- Audit recent bind-parameter incidents and the current Go/pgx call surface.
- Add a reusable SQL placeholder analyzer and repository guard with adversarial self-tests.
- Cover `Query`, `QueryRow`, `Exec`, and `Batch.Queue`, including pgx execution-mode options, repeated placeholders, comments/quoted text, variadic arguments, and dynamic query builders.
- Replace the tactical Weighing hardcoded-count assertion with a derived contract.
- Wire the guard into ordinary backend CI and guardrails; push/landing hooks must require a fresh passing local-CI receipt that includes the guard.
- Add or update a focused repository skill so future agents select safe patterns and required proof.
- Obtain independent audit and judge reviews, address findings, push a branch, and keep the pull request updated.

## Done

- Created clean isolated worktree `/Users/raviteja/mesha/goatos-sql-bind-contract-guard` from `origin/main` at `e565e0d291b89ec4ae31d9003f028cca5ed9638b`.
- Preserved the dirty primary checkout and its unrelated changes.
- Started three independent read-only audits: historical/current risk inventory, guard architecture, and skill/CI/push wiring.
- Confirmed the repository uses pgx `v5.9.2`, which supports `pgx.StrictNamedArgs` for exact named-parameter validation.
- Confirmed prior incidents include Weighing conditional pruning, Feed batch placeholder renumbering, and Obligation unused positional arguments.
- Historical audit confirmed a fourth incident: Leadership Tasks bound tenant plus user to a tenant-wide `$1` query, causing `expected 1 arguments, got 2` on the unfiltered shape.
- Added PostgreSQL-aware `sqlbind` lexical validation and a `BoundQuery` contract that derives placeholder continuity and argument arity from final SQL and actual arguments.
- Added a Go AST scanner for literal/constant `Query`, `QueryRow`, `Exec`, and `Batch.Queue` calls, with explicit handling for pgx control arguments, `StrictNamedArgs`, and validated `BoundQuery` use.
- Added an always-on repository guard with adversarial self-test, per-file shrink-only legacy baseline, changed-file enforcement, and unconditional backend-CI/guardrails/landing wiring.
- Kept enforcement in the explicit guard and local CI; the existing push/landing hooks require a fresh passing local-CI receipt. An editor-wide PostToolUse hook was removed after judge review because running the Go scanner after every edit added avoidable latency.
- Replaced the Weighing hardcoded-`31` primary assertion with runtime validation of the final pruned SQL and actual production argument slice. The legacy unverified count shrank from 359 to 358.
- Updated backend engineering guidance, root/backend agent rules, existing build/review/database skills, and the pull-request proof checklist.

## Pending

- GitHub review and any requested follow-up. Main merge and deployment remain out of scope.

## Tests and evidence

- `make postgres-bind-contract-guard`: PASS; 358 legacy dynamic findings held by a per-file shrink-only baseline.
- `make guardrail-registration-guard`: PASS; new guard registered and reachable.
- Focused Go tests for `sqlbind`, AST scanner, and Weighing pruning: PASS.
- Throwaway PostgreSQL 16 execution: PASS for `dimensions`, `origin`, `shed_type`, `weight_bands`, and `weekly_gain`; every test executed rather than skipped.
- First full `make ci-local` at `3a4e3fa113b37220c0ca3b94ea339e56fe2a7bed`: RED only on missing leadership-assistant classification for the new infrastructure helpers and an exception-guard classification for malformed AST string literals. All product suites, the new bind guard, full Go tests, query plans, admin-web build/tests, and Android compile/unit/lint passed. Both metadata findings were fixed before the final rerun.
- Final full `make ci-local` at implementation SHA `8827233e6e0f93c941c7a3b87f3133cffd49f73e`: GREEN with a fresh all-scope receipt. The optional `ai-doctor` step warned that the local Repowise index was stale; it was not a required product/landing gate.

## Known failures and constraints

- PostgreSQL integration tests are opt-in in normal local CI, so static bind-contract enforcement must be always-on.
- The current tree contains thousands of pgx call expressions; enforcement must distinguish resolvable calls from dynamic builders and avoid silently grandfathering newly changed unsafe code.
- The scanner intentionally baselines 358 currently unprovable dynamic sites. It blocks provable mismatches globally, debt growth per file/category, and any changed backend Go file that still contains an unsafe unresolved call; deeper builder provenance remains a phased migration rather than a false zero-debt claim.

## Judge status

- Three specialist audits completed and incorporated. Independent judges found and drove fixes for changed-line enforcement, pgx option handling, named-map key validation, mutable wrappers, import spoofing and shadowing, cross-scope identifier collisions, unchecked `Bind` errors, extra bound-call arguments, non-pgx selector false positives, and a stale Weighing source guard.
- Final judge verdicts: runtime/lexer APPROVE; guard/CI bypass resistance APPROVE; scope/skills completeness APPROVE.
- Maintainer-requested performance judge: APPROVE. No admin-web/shared frontend package changed; the 57,062-byte Weighing SQL template and DB call count are unchanged. The only request-path work is one linear SQL scan, a 31-entry ordinal map, and two 31-element defensive slice copies before the existing query. The warm whole-repository build-time guard completed in about 1.02 seconds and is not installed as an editor-wide hook.

## Source and deployment state

- Branch: `fix/sql-bind-contract-guard`.
- Base SHA: `e565e0d291b89ec4ae31d9003f028cca5ed9638b`.
- Last fully certified implementation SHA: `8827233e6e0f93c941c7a3b87f3133cffd49f73e`.
- Final pre-PR all-scope CI receipt: GREEN at `98d2aca15969ef49e7d7e7f3aa2f7f527a350869`.
- Pushed branch and opened PR: https://github.com/vgoats/goatos/pull/326. Local, remote, and PR heads matched `98d2aca15969ef49e7d7e7f3aa2f7f527a350869` before this bookkeeping update.
- No merge or deployment performed.

## PR 326 review fixes

- Scope: address named-map mutation/escape false negatives and dollar signs inside unquoted SQL identifiers. Base reviewed SHA: `c8929125b923cf2cd6f4befb8e7ef26c2fc41443`; current fix SHA is the commit containing this entry.
- Done: reject named-map index writes, delete/clear, aliases, helper calls, and pointer escapes as unverified; preserve direct database-call uses. Consume complete unquoted identifiers before scanning placeholders.
- Tests: focused sqlbind, scanner, and Weighing postgres package tests PASS; regression cases cover mutations/escapes, repeated safe reads, dollar-containing and Unicode identifiers, extra arguments, and real placeholders beside identifier suffixes. Bind guard and registration guard PASS; legacy baseline remains 358, registration count 139.
- Before/after: the two review fixtures previously produced scanner exit 0 with no findings; after fixes they produce positional-bind-mismatch and unverified-dynamic-args. No API latency improvement is claimed; SQL templates and database call counts are unchanged.
- Known limitations: PostgreSQL integration tests remain opt-in and were not executed in this fix session; no live browser E2E or latency run, no fresh full local CI receipt. Map analysis intentionally fails closed on uses outside its approved direct-call pattern.
- Judge status: local regression verification completed; no independent judge requested for this follow-up.
- Pending: push this fix commit to the existing PR branch and verify remote head.
- Deployment state: no main merge, main push, or staging deployment authorized or performed.

## Maintainer-requested review loop (2026-09-21)

- Scope: fix PR 326 findings, push only the PR branch, and repeat independent agent review until no actionable findings remain. Starting head: `d20bda473dfb8eda5aa6f15a89927c3100fe3355`. No main promotion or deploy.
- Done: resolve constants by lexical object identity and fail closed for cross-file unresolved constants; reject pgx controls and query rewriters in bound data; reject mutable/address-escaped bound values and uses before a checked Bind succeeds. Fix CR-terminated comments and Unicode dollar-quote lexing.
- Before/after: cross-file constant collision and pgx-control fixtures passed unsafely before; now rejected. Added regressions for tuple reassignment, pointer escape, and use inside the error branch. No API latency improvement claimed.
- Tests: focused scanner/sqlbind tests and repository bind guard PASS (358 unchanged baseline findings). Real PostgreSQL 16 Weighing section matrix PASS: dimensions, origin, shed_type, weight_bands, weekly_gain, none skipped. Raw pgx option reproduction fails with SQLSTATE 08P01; fixed Bind rejects it first. Unicode dollar-tag and CR-comment queries execute successfully against PostgreSQL.
- Judge status: first independent Node guard/baseline/CI review reports no actionable findings. Counter-review found local option aliases, parenthesized pointer/assignment escapes, range overwrites, branching error guards, and E-string continuation; all fixed with regression coverage. Runtime re-review reports no further actionable findings; scanner re-review also reports no actionable findings after re-running all four confirmed bypass fixtures.
- Pending: fresh full local CI, PR push and exact remote readback. Optional ai-doctor cannot use absent graph/Repowise indexes in this isolated checkout; source inspection used instead.
- Known limitations: no live browser E2E or real-route latency certification; no frontend, SQL template, DB fanout, tenant/grant, or sync behavior changed.

### Pushed-head review round

- Pushed and read back implementation `4c5bd5d2b65a6cea4fd6d9634f47b8cca0173ee1` on PR 326; full Go suite PASS.
- Post-push runtime reviewer: no actionable findings. Scanner reviewer reproduced a remaining batch QueryRewriter bypass; pgx rejected the supposedly valid batch. Fixed: Queue rejects known rewriters while preserving execution options as data. Scanner tests and repository guard pass (358 unchanged baseline findings).
- Full CI on that superseded implementation was deliberately interrupted before completion; it is not a green receipt. The batch-fix commit restarts full CI and independent review; final receipt is kept in the worktree Git directory and the PR description records the exact tested SHA.
- Deployment state remains unchanged: no merge, main push, or deployment.

## PR 326 argument provenance follow-up

- Scope: fix scanner approval of custom pgx query rewriters and reassigned execution controls; push only the existing PR branch.
- Starting SHA: `dc53ee3c869fcea3f1244211e2fe81e06fdb3c55`.
- Before: compilable custom-rewriter and reassigned-`any` fixtures both return scanner exit 0, then PostgreSQL 16 rejects execution with SQLSTATE 42P02.
- Done: review reproductions confirmed; existing focused tests, bind/registration guards, and all five real PostgreSQL Weighing section variants passed.
- Pending: regression tests, scanner fix, local CI, PR branch push and readback.
- Known failure: the scanner treats unrecognized leading arguments as ordinary data even when pgx consumes them as controls.
- Performance judge: unchanged runtime SQL, argument order, cache, DB calls, frontend and sync. Existing runtime validation measured 53-81 microseconds per uncached section/all-sections read; this follow-up changes build-time scanning only.
- Browser E2E: not run; no frontend or runtime query changes planned.
- Deployment state: no main push, merge, or deployment.

### Follow-up implementation proof

- Regression-first: all eight new argument-provenance cases failed on the starting head with zero scanner findings.
- Fix: collect package-local custom rewriter receiver types, follow all assignments and declaration types, resolve sibling-file provenance with source-specific imports, and cache facts once per package. Ordinary same-named local types retain lexical identity.
- Focused scanner/sqlbind/Weighing tests: PASS. Cross-file receiver, factory, aliased-import, package variable, and same-name local-type regressions: PASS.
- Original two compilable PostgreSQL reproductions now fail the scanner with `unverified-dynamic-args`; prior runtime failures were SQLSTATE 42P02.
- Runtime changes: none in this follow-up; API latency, allocations, SQL text and DB calls remain as reviewed.
- Pending: final guard, independent performance review, exact-head local CI, and PR push/readback.
- Independent performance review caught exponential duplicate traversal on long ordinary alias chains (24 links: 5.16s versus 0.092s before). Fixed by visiting each object once for the entire per-argument reachability walk; the new 40-link regression passes within the focused 30-second suite timeout.
- Optional AI tooling doctor: source portability checks passed; fresh-worktree CRG/Repowise indexes are absent. Repository CI treats these indexes as optional and does not use them as product proof.
- Independent performance re-review: PASS. Fixed 24-link chain 6.6-7.1ms; 40/100 links 8.9/10ms. Full-tree scanner 0.609-0.666s versus baseline 0.537-0.597s, with identical 358 legacy findings. No remaining performance finding.
- Final bind guard and registration guard: PASS, unchanged baseline and 139 registered guards. Final full local CI and remote publication are recorded in the local follow-up log and PR description after this commit.

## PR 326 container-control review fix

- Scope: reject pgx controls and rewriters reached through struct fields, indexed containers, and subsequent field/element assignments. PR branch only.
- Starting SHA: `6d27ec0912207b4371a7b4bc6e1920a45e21b2d6`; current SHA is the commit containing this entry.
- Before: seven added field/slice/map/control/rewriter fixtures all failed because the scanner emitted no findings. After: all seven are rejected as unverified dynamic arguments.
- Done: traverse container types and values and track assignments through their root object. This is conservative rejection evidence: selecting a data field from a container that also holds controls may require a validated builder. Added allowed cases for ordinary struct/slice/map values and batch execution-mode data.
- Tests: focused scanner, sqlbind, and Weighing adapter packages PASS with `-count=1 -timeout=60s`; bind guard PASS with the unchanged 358-entry legacy finding count. Runtime SQL, API behavior, frontend, sync, query counts and data scope are unchanged; no API speedup claimed.
- Pending: independent performance review, local CI, PR branch push and remote readback. No PostgreSQL integration or browser E2E run for this scanner-only fix.
- Known failures: seven regression-first failures were expected and fixed. No implementation test failures remain.
- Deployment state: no main merge, main push, or deploy.
