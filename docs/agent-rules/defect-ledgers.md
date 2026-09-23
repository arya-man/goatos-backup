# Defect Prevention and Defect-Ledger Closure

> Moved verbatim from `AGENTS.md` (split 2026-09-24 to keep session start small).
> These rules are as binding as `AGENTS.md` itself. Only file location changed.

## Defect Prevention Closure (Mandatory)

Every bug fix, audit batch, kernel milestone, and feature change follows
`context/execution/defect-prevention-execution-contract.md`. A fix is not closed
until the same current-SHA packet includes the failing-before production-path
regression, the root-cause implementation, the strongest applicable recurrence
control, ordinary affected `make ci-local` wiring, recovery/observability where
needed, docs/skill/anti-pattern sync, and independent counter-review.

When a rule is mechanically detectable, ship its structural guard in the same
batch with adversarial negative fixtures, manifest registration, self-test,
Make target, and standard `run_common` or component-job wiring. A hook or
compatibility-only `JOB=guardrails` path is not enforcement. If a static guard
is weaker than a DB/transaction/contract/runtime control, record that choice and
ship the stronger control; do not write a literal-only false-green grep.

Delegated briefs must name the absolute repo path, fresh base SHA, owned files,
invariants, banned patterns, red/green tests, prevention work, and CI commands.
The coordinator owns shared migrations/contracts, independently verifies every
claim, and keeps closure-pending work open.

## Consolidated Defect-Ledger Closure (Mandatory)

When asked to fix/continue/close the consolidated audit ledger or its bugs, read
both of these before editing:

- `context/repo-audits/last-35-commits-consolidated-bug-ledger.md`
- `context/repo-audits/consolidated-ledger-defect-closure-program.md`

Select one highest-priority unblocked root defect (or an inseparable cluster),
reconstruct the live count from the file, and follow the closure program across
every affected backend, SQL, API, admin-web, Android, architecture, performance,
memory, retry, pagination, security, E2E, observability, and CI/CD layer. Do not
mark a row fixed until its current-SHA proof packet and independent Claude/Codex
counter-review pass. Merge duplicate-root evidence instead of inflating counts.
`CLAUDE.md` and `CODEX.md` remain thin shims to this shared rule.
