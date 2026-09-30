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

- **Admin-web push gate (every branch) + skip ledger (FIXJ-CI, 2026-09-30)**: the
  pre-push hook (`tools/agent-hooks/pre-push.hook`, installed by `make ai-setup`) runs
  `tools/ci/admin-web-push-gate.sh` on every push to any branch that changes an admin-web
  input: design:guard, typecheck, npm test, next build, visual gate. Missing guard files fail
  the push. Lane-skipping flags (`GOATOS_SKIP_ADMIN_WEB_VISUAL_GATE`, `GOATOS_FAST_LOCAL_CI`,
  `GOATOS_CI_ONLY_STEP`, `GOATOS_ADMIN_WEB_BASE_URL` for the visual gate) need
  `GOATOS_SKIP_REASON="..."` and are recorded in the skip ledger (`tools/ci/goatos-skip-ledger.sh`).
  Details: `apps/admin-web/AGENTS.md` "Local CI is strict".

- **The shared pre-push hook is a shim; each branch runs its own hook (FIXJ8, J1B P0-2,
  2026-09-30)**: every worktree of a clone shares `<git-common-dir>/hooks`. The installer used
  to COPY the installing branch's hook and guards there, so whichever branch last ran
  `make ai-setup` decided what every branch's push ran: review/pr-307's old stg/main-only hook
  silently replaced PR #294's admin-web gate, and 2d43dee4a reached the PR with no visual-gate
  pass and no skip-ledger row. Now `tools/agent-hooks/install-stg-push-guard.sh`
  (`make push-hooks-install`, `make ai-setup`, every `make land-main`) installs
  `tools/agent-hooks/pre-push.shim` as `hooks/pre-push`. The shim holds no gate logic. On each
  push it reads the pushing worktree's committed `tools/agent-hooks/pre-push.hook`, plus the
  files `tools/agent-hooks/pre-push.bundle` lists, from HEAD, stages them and runs them. A
  branch with no checked-in hook gets the legacy stg/main guard: its own
  `tools/ci/check-{stg-promotion,local-ci-evidence}.mjs`, else the snapshots beside the shim.
  Nothing found means the push is refused. Installing from any branch never downgrades another.
  An older branch's installer treats the shim as a foreign hook and chains it as
  `pre-push.before-goatos-stg-guard`, so the shim still runs. Change the gate by committing to
  `pre-push.hook` / `pre-push.bundle`; no reinstall is needed. The branch hook judges its own
  branch, and main stays protected by the exact-SHA ci-local receipt, the required
  `goatos/land-main-receipt` status and the receipt check below.
  `tools/ci/check-push-hook-freshness.sh` (self-test `check-push-hook-freshness.test.sh`)
  fails unless:
  - the installed pre-push is byte-identical to the shim;
  - the shim resolves this worktree's hook (`GOATOS_PUSH_SHIM_WHICH=1 .git/hooks/pre-push`);
  - REAL test pushes into a throwaway bare repo behave: main without a receipt is refused, an
    admin-web feature push is refused by the admin-web lanes, and a branch with no hook gets
    the legacy guard.
- **Push-gate receipts reach the PR and gate landing (FIXJ8)**:
  - Every push the gate sees writes a receipt to `<git-common-dir>/goatos-push-gate/receipts`
    (`tools/ci/admin-web-push-receipt.mjs`). It records the SHA, ref, input digest, each lane
    as pass / reused-pass / skip (with its written reason) / not-applicable, the skip-ledger
    rows for the SHA, and the commits the push covered (SHA + `git patch-id --stable`). A
    skipped lane without a >= 12 char reason is refused.
  - Once the SHA is on GitHub, the receipt is posted on the PR as the `goatos/push-gate`
    commit status: green when every lane ran, red with `SKIP <lane>: <reason>` when one was
    skipped.
  - `make land-main` checks every commit in origin/main..candidate before ci-local. A commit
    no receipt covers (by SHA, or by patch-id after a rebase) REFUSES the landing. Fix: run
    `tools/ci/admin-web-push-gate.sh --certify origin/main`, which runs every lane on HEAD and
    records a receipt for the range.
  - The receipts are attached to the land-main receipt (`goatos-land-main-gate-receipts.json`
    in the git dir), and the `goatos/land-main-receipt` status carries the commit and skip
    counts.

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

- **Deploy-drift rule (perf budget, 2026-09-24; catalog `.agents/skills/scale-anti-patterns/SKILL.md` ("STG latency catalog", P1-P25) P24).** A perf fix
  is not done when it lands; it is done when the exact main SHA is deployed to
  stg (launcher or Slack deploy button) and re-measured there. Before reporting
  anything "still slow on stg", check the deployed revision SHA equals
  `origin/main`. Bad: stg ran `a67be34c0781` while main was 116 commits ahead.
