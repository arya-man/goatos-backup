## PR type

- [ ] Ordinary PR. Program-only fields below are `N/A`; follow the ordinary
  scope/proof/review contract and repository landing rules.
- [ ] Sole approved whole-ledger/task-kernel integration PR. Every applicable
  program field and program checklist item below is mandatory; ordinary-only
  fields are `N/A`, and worker or milestone PRs are forbidden.

## Scope and dependency

- Root batch / stable IDs / feature contract:
- Base SHA:
- Integration branch/head SHA:
- Internal batch commit map and dependency order:
- Proof-index link and current ledger tally:
- Latest main-sync merge and conflict resolutions:
- Owned files and shared-file coordinator:

## Failure and root cause

- Exact failing scenario:
- Production reader/writer/caller:
- Root cause and sibling paths audited:
- Existing authority sources / hard blockers (no routine maintainer action requested):

## Prevention packet

Complete the mandatory contract in
`context/execution/defect-prevention-execution-contract.md`. Use `N/A` only with
a concrete reason.

- [ ] Canonical invariant/decision/schema updated
- [ ] Failing-before production-path regression recorded
- [ ] Persistent constraint/transaction/version/idempotency protection added
- [ ] Structural guard added or stronger-control rationale recorded
- [ ] Guard adversarial self-test covers realistic evasions
- [ ] Guard registered and wired into the ordinary affected `make ci-local` job
- [ ] Cross-surface contracts/generated clients/Room consumers proved
- [ ] Recovery, reconciliation, metrics, alerts, and DLQ outcomes covered
- [ ] Relevant `AGENTS.md`, skills, anti-patterns, runbooks, and phase docs synced
- [ ] Operational work follows the shared event -> owned task -> clock -> hierarchy -> contact waterfall -> proof -> sign-off -> rollup chain; no private task/scheduler/escalation path was introduced, retained as canonical, or exempted

## Proof

- Red command/result on base:
- Green command/result on candidate:
- Guard self-test and real-check result:
- Affected `make ci-local` result and exact SHA:
- Migration/upgrade/lock/restart proof, if applicable:
- Browser/device/production-path evidence, if applicable:
- Deployed revision/schema/repair evidence, if applicable:
- Known limitations or closure-pending controls:

## Review and landing

- [ ] Independent counter-review reconciled every raised finding
- Independent judge model/reasoning and reviewed SHA:
- [ ] Rollback class, kill switch/forward-repair path, and final ledger reconciliation are recorded

Ordinary PR only:

- [ ] Ordinary affected CI and selected certification evidence are recorded;
  landing follows the repository's ordinary PR/main contract.

Sole program PR only:

- [ ] Every internal milestone's incremental commits and the cumulative PR diff were reviewed
- [ ] Fresh `origin/main` was merged into the single integration branch and affected prior gates were rerun
- [ ] Expected PR base/head and the exact-head local-CI receipt are recorded
- [ ] `make land-integration-pr PR=<number>` fenced the fast-forward to that head SHA
- [ ] Fresh `origin/main` equals the tested PR head, the PR reports merged, and post-landing exact-SHA verification passed
