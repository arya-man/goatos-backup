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

- Run full local CI, push branch, create PR, and verify PR/head state.

## Tests and evidence

- `make postgres-bind-contract-guard`: PASS; 358 legacy dynamic findings held by a per-file shrink-only baseline.
- `make guardrail-registration-guard`: PASS; new guard registered and reachable.
- Focused Go tests for `sqlbind`, AST scanner, and Weighing pruning: PASS.
- Throwaway PostgreSQL 16 execution: PASS for `dimensions`, `origin`, `shed_type`, `weight_bands`, and `weekly_gain`; every test executed rather than skipped.

## Known failures and constraints

- PostgreSQL integration tests are opt-in in normal local CI, so static bind-contract enforcement must be always-on.
- The current tree contains thousands of pgx call expressions; enforcement must distinguish resolvable calls from dynamic builders and avoid silently grandfathering newly changed unsafe code.
- The scanner intentionally baselines 358 currently unprovable dynamic sites. It blocks provable mismatches globally, debt growth per file/category, and any changed backend Go file that still contains an unsafe unresolved call; deeper builder provenance remains a phased migration rather than a false zero-debt claim.

## Judge status

- Three specialist audits completed and incorporated. Independent judges found and drove fixes for changed-line enforcement, pgx option handling, named-map key validation, mutable wrappers, import spoofing and shadowing, cross-scope identifier collisions, unchecked `Bind` errors, extra bound-call arguments, non-pgx selector false positives, and a stale Weighing source guard.
- Final judge verdicts: runtime/lexer APPROVE; guard/CI bypass resistance APPROVE; scope/skills completeness APPROVE.

## Source and deployment state

- Branch: `fix/sql-bind-contract-guard`.
- Base SHA: `e565e0d291b89ec4ae31d9003f028cca5ed9638b`.
- No push, PR, merge, or deployment yet.
