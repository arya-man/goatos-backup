# CI, Local-CI Push Gate, Main Landing and Release Tags

> Moved verbatim from `AGENTS.md` (split 2026-09-24 to keep session start small).
> These rules are as binding as `AGENTS.md` itself. Only file location changed.

- CI availability is never a closure blocker (Claude AND Codex). A GitHub Actions
  billing/spending/platform failure — the synthetic `BuildFailed` /
  `(Unknown event)` / zero-job `startup_failure` runs — must NOT be recorded as
  an external blocker or used to defer a fix. When remote GitHub Actions cannot
  execute, run the SAME affected-component gates LOCALLY via `make ci-local`.
  The default classifier compares the candidate to `origin/main`, always runs
  common repository guards, and adds backend, admin-web, and/or Android jobs only
  when their owned paths or shared contracts changed. Unmapped paths and changes
  to CI workflows, CI scripts, agent hooks, or the Makefile force the full suite;
  `make ci-local MODE=all` is the explicit full-suite command. Treat a green
  `make ci-local` on the exact pushed SHA as the authoritative ordinary
  deterministic CI gate. It does not replace applicable PostgreSQL, migration,
  device, browser, deploy, or live-state certification lanes; those remain
  closure blockers. Record the `make ci-local` SHA + result as current-SHA
  ordinary-CI proof. Restoring org Actions billing stays a separate maintainer
  task, tracked but never blocking closure.

- **Postgres tests are explicit opt-in only**: Default `make ci-local`, every
  `JOB=...`/`MODE=all` invocation, pull-request workflow, push workflow, and
  scheduled workflow must not start Postgres or run Docker-backed DB tests.
  A local database run requires `GOATOS_RUN_POSTGRES_TESTS=1`. Hosted DB gates
  are not a Goat OS staging deploy path while GitHub Actions is unavailable.
  `MODE=all` means all affected component jobs, not Postgres.
  `GOATOS_REQUIRE_DOCKER=1` may make an explicitly requested DB run fail closed,
  but it must never opt a default run into Postgres by itself.

- **Exact-SHA local-CI push gate (main)**: Only a complete green `make ci-local`
  on the exact commit SHA authorizes a push to `main`. The pre-push hook installed by
  `make ai-setup` enforces this via a machine-local SHA-bound receipt
  (`goatos-ci-local-receipt.json` in the worktree git directory). The receipt
  is either mode `all`, or mode `scoped` bound to the exact remote-main base,
  component-rule hash, and complete classifier-selected job list. The hook
  recomputes scoped coverage at push time; a changed base, stale rules, missing
  component, or newly-full diff is rejected. Explicit partial
  `JOB=...` runs intentionally write NO receipt and never authorize a push. Every
  new machine guardrail MUST be registered in `tools/ci/guardrail-manifest.json`
  and wired into both `Makefile:guardrails` and `tools/ci/run-local-ci.sh`. The
  current `guardrail-registration-guard` proves enumeration, declarations, and
  textual reachability only; semantic execution/routing, existence/uniqueness,
  and spoof resistance remain F0 work and must be manually verified until that
  hardening lands. See `docs/runbooks/local-release-evidence.md` →
  "Exact-SHA Local-CI Push Gate (Main)" and `docs/runbooks/local-ci.md` →
  "Guardrail registration and exact-SHA push evidence" for the full flow. Do not
  bypass the hook with `--no-verify`.
- **Mandatory ordinary main landing (Codex and Claude)**: For ordinary work and
  this documentation foundation, when the requested outcome includes pushing
  to `main`, run **`make land-main`** instead of composing
  `git fetch` / `git rebase` / `make ci-local` / `git mesha-push` by hand. The
  target refuses a dirty worktree, fetches fresh `origin/main`, rebases the
  candidate before CI, runs the complete affected-component `make ci-local`,
  fetches main again, and reruns rebase + CI if main moved before pushing the
  exact certified SHA. Agent hooks block direct agent-issued pushes to `main`,
  and the Git pre-push hook independently rejects a candidate that does not
  contain the current remote-main SHA. Do not auto-rebase at session start:
  sessions may open on dirty/shared worktrees with other agents' changes. Commit
  only the scoped work and use a clean isolated worktree for landing. Standalone
  `make ci-local` remains valid for development/hosted CI; `make land-main` is
  the release path that mutates history and pushes. Do not use GitHub connector,
  `gh pr merge`, or the web merge button as a shortcut unless the current PR
  head already has a completed green GitHub `ci` run on the exact SHA after a
  fresh-main rebase.
- **Whole-ledger/task-kernel program landing exception**: the documentation
  foundation may use ordinary `make land-main`, but the approved implementation
  program uses exactly one external integration PR. It must not use milestone
  PRs or ordinary `make land-main`. F0 first adds the repo-owned
  `make land-integration-pr PR=<number>` exact-head fast-forward gate defined in
  `context/execution/defect-prevention-execution-contract.md`; until then no
  implementation batch closes and the program PR cannot land.
- **Mandatory GitHub release tags and Firebase provenance**: Every dev/stg/prod
  release must create an annotated GitHub tag through `make release-tag`, never
  a hand-written `git tag` command. The tag message must keep separate Backend,
  Frontend/Admin Web, Mobile Android, Infra/Deploy, Docs/Seed/Data, and Other
  sections. STG Cloud Deploy creates the tag automatically after verified
  rollout. Firebase App Distribution releases must restore credentials with
  `make restore-stg-android-release-env`, then add the Android version/code and
  Firebase release URL to the tag before handoff. See
  `docs/runbooks/release-tags.md` and `docs/mobile/stg-signed-release.md`.
