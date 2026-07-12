# Repository Cleanup — 2026-07-12

## Scope

This records the evidence-backed consolidation of GoatOS local worktrees,
branches, stashes, and GitHub pull requests into `main`. It exists so cleanup
does not silently destroy unpublished product work or merge unsafe scratch code.

Verified authority before writes:

- Repository: `https://github.com/vgoats/goatos.git`
- GitHub repository: `vgoats/goatos`
- Active GitHub account: `ravimesha`
- Default branch: `main`
- Required push path: `git mesha-push main` with `MESHA_GITHUB_PAT`

## Starting Inventory

- Registered worktrees: 19
- Dirty worktrees: 3
  - primary audit checkout: 9 paths
  - `m4-android-finish`: 12 paths
  - `worktree-agent-a286fb8019b74d5d9`: 4 paths
- Local branches: 19
- Origin remote refs: 9, including the `origin` symbolic ref
- Git stashes: 9
- Open pull requests: 1 — PR #3, `main -> stg`, titled “Deploy main to staging”
- `origin/main` initially matched audit HEAD `d2a7fbcf`; the separate local
  `main` worktree was 19 commits behind it.

## Published Current-Checkout Work

- Kept the consolidated 40-row audit ledger, closure program, shared
  Claude/Codex routing rules, and current handoff.
- Reverted `backend/tests/e2e/report/index.html`: it was accidental generated
  drift from a partial 3-story run replacing the valid 41-story report.
- Ported the one valid unique Android scratch fix: wrapped Firebase/Credential
  errors are classified through their cause chain and raw provider exception
  text is never shown to operators. Added its unit regression test.

## Dirty Worktree Dispositions

### `m4-android-finish`

Not publishable as a whole. The dirty set mixed:

- an explicit `TEMP DIAGNOSTIC (revert before push)` HTTP body logger;
- deletion of the current login-time permission gate, contradicting current
  app code and permission contracts;
- login UI and staging URL changes already present in newer `main`;
- the valid wrapped-auth error fix, which was ported independently with a test.

After the valid fix was ported, the remaining dirty changes were restored.

### `worktree-agent-a286fb8019b74d5d9`

The dirty HRMS changes were an older partial implementation. Current `main`
already contains the contract-driven tab/KPI labels and escalation-state logic
in a more complete version. The scratch version also added a broad literal-guard
exception and carried unused variables/stale copy. It was restored, not merged.

## Branch Dispositions

Retained branches:

- `main`: canonical product branch.
- `stg`: staging deployment branch and base of PR #3.
- `gh-pages`: currently required because GitHub Pages reports
  `build_type=legacy` and serves from `gh-pages:/`.

Scratch branches are removable for these reasons:

- `agent/vaccination-seed-history-calendar`: consolidated into `main` after the
  cleanup commits.
- `agent/finish-vaccination-closure` (`310b8969`): a 299-file stale WIP snapshot.
  Its useful findings/patterns are already recorded in the canonical audit
  ledger; merging it would overwrite newer code and reintroduce unreviewed work.
- `agent/kernel-audit-review-fixes` and
  `agent/kernel-audit-scale-review-fixes`: cherry-pick conflicted because current
  `main` contains newer, stronger cap-bucket, scope-claim, trace, and retention
  design. The branch is superseded, not unpublished truth.
- `agent/staging-vaccination-clean-slate`: commit `415e9b31` was already
  cherry-picked as `b0689a97`; the only omitted delta was generated HRMS report
  noise. The follow-up commit is patch-equivalent in `main`.
- `backup/pre-merge-20260711`, `agent/vaccination-closure-slice`,
  `agent/kernel-audit-flag-doc`, `agent/publish-audit-plan-to-main`,
  `codex/vaccination-workflow-gaps`, `feat/hrms-rebuild`, and
  `feat/mobile-recording-form`: no unique commits remain relative to `main`, or
  their patch is already represented there.
- `m4-android-finish` and `worktree-agent-a9ecbea17e46cefa7`: contain the same
  old telemetry/Firebase experiment. Firebase enablement remains explicitly
  gated/not executed in `docs/mobile/firebase-india-setup.md`; the follow-on
  stash labels analytics work “redo cleanly later.” Do not smuggle this old
  experiment into current `main` during repository cleanup.
- `worktree-agent-a7a8a65b338a13cff`: patch-equivalent roster fixes already in
  `main`.
- `codex/vaccination-workflow-gaps` and remaining detached review/temp trees:
  stale inspection points, not unpublished changes.
- Remote closed-PR branches `fix/vaccination-vertical-kernel-bugs` and
  `vaccination-v1-dev`: old closed work; merging them would reintroduce hundreds
  of commits of superseded code and an accidental root `package-lock.json`.

## Stash Dispositions

- `temp-kernel-validation` and `autostash`: overlapping 233/298-file stale WIP
  snapshots around `310b8969`. They contain partial ledger-fix experiments and
  cannot be merged over newer main. The canonical ledger is the execution queue.
- `preserve non-audit android edits`: hardcodes English empty-state strings over
  localized resources; reject.
- `codex-generated-e2e-report-noise`: generated UUID/time/report drift; reject.
- `local artifacts before main merge`: emulator DB/WAL files, screenshots,
  generated report drift, and staging Terraform already represented in current
  `main`; reject local artifacts and duplicates.
- `partial-analytics-wip (redo cleanly later)`: explicitly incomplete telemetry
  work plus root screenshots; reject during cleanup.
- `unattributed-adminui-vaxexec-edits-during-cherrypick` and
  `stray-adminweb-cmdsurface-edits`: unattributed semantic changes that remove or
  relabel `owner_missing`/coverage behavior and conflict with current backend
  contracts; reject.
- `preserve mesha shell worktree change before branch cleanup`: current `main`
  already has the later company-scope implementation; reject the older shell
  patch.

All stashes can therefore be dropped after this disposition is committed.

## Deployment Safety

Pushing `main` does not deploy staging. PR #3 merely updates to the new `main`
head. Staging deploy runs only when PR #3 is merged and `stg` receives a push.
Repository cleanup must not merge PR #3.
