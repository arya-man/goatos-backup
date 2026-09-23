# Goat OS Workspace Agent Context

## Domain Rule Index

Domain rules live VERBATIM (not summarized, not relaxed) in `docs/agent-rules/`
and bind exactly like this file. Before working in an area, read its file in
full. `tools/agent-rules/verify-agents-split.mjs` proves no rule was dropped.

Older docs, code comments and hook messages cite `AGENTS.md` -> "<heading>";
that heading now lives in this file OR `docs/agent-rules/`. Resolve with
`grep -rn '<heading>' AGENTS.md docs/agent-rules/`.

<!-- GUARD-REQUIRED TOKENS: the index lines below intentionally keep these exact
strings in AGENTS.md; guards grep AGENTS.md itself for them, so do not reword
them away: `docs/architecture/operational-read-model-contract.md`
(check-operational-read-model-contract.mjs), `docs/features/critical-animal-action-guardrails.md`
(check-critical-animal-action-availability.mjs), `make release-tag`
(tools/release/check-release-tag-contract.mjs). -->

- Business data / "where is X in the DB" / any stg data question -> `.agents/skills/mesha-data-map/SKILL.md` (topic -> ceo_ai view -> columns -> date column; one query, skip \dt/\d exploration). Short form: `tools/ask-mesha-agent/data-map-core.md`.
- Android/mobile/APK/Room/device QA -> `docs/agent-rules/android.md`: proof media, CLI bootstrap, APK traceability, who-did-what provenance, offline-first Room reads, refresh-on-open, Room upgrades, one-page fetch cap.
- Vaccination -> `docs/agent-rules/vaccination.md`: anchor dates, catch-up joins normal drive, safe-window park batching, 200/operator-day packing, source dates.
- Tasks/pen visit -> `docs/agent-rules/tasks-pen-visit.md`: day-after check, pen visit as own task, task-kernel non-deviation lock.
- Weighing -> `docs/agent-rules/weighing.md`: isolated from herd/vaccination, scan-and-submit only, unconditional close gate.
- Herd ops/sales -> `docs/agent-rules/sop-herd-sales.md`: SOP-driven herd ops and features, sale workflow on /sales/sops.
- Procurement -> `docs/agent-rules/procurement.md`: SOP-driven end to end.
- Any business/medical rule change -> `docs/agent-rules/business-medical-rules.md`: maintainer-locked rule record.
- Sheds/pens/partitions/locations -> `docs/agent-rules/partition-location.md`: storage vs display, `shed_id + park`, canonical fetch/compose, hard rules.
- Backend write paths/events/read models -> `docs/agent-rules/backend-contracts.md`: domain events, `docs/architecture/operational-read-model-contract.md`, `docs/features/critical-animal-action-guardrails.md`, grain gates, idempotency, atomic read-model sync, validate-or-reject, clinical defer.
- Admin-web/frontend/copy/notifications/charts -> `docs/agent-rules/ui-frontend.md`: mock is UI truth, taxonomy, command-room authority, copy firewall, interaction patterns, visual QA, charts name pens.
- Queries/projections/hot APIs/scale -> `docs/agent-rules/scale-performance.md`: scale anti-patterns, latency, count parity, E2E publishing, kernel-first, freshness gates.
- Time/dates/business day -> `docs/agent-rules/time-semantics.md`: India business calendar, vaccination grain is the day, pinned clocks.
- New APIs/workers/logging/telemetry -> `docs/agent-rules/observability.md`: observability, identifiers, loggers, telemetry and hot-path guardrails.
- Local stack/ports/local DB/E2E stacks -> `docs/agent-rules/local-stack.md`: canonical ports, one app DB, shared trio on exact origin/main, isolated E2E stacks. (Its UI-proof/phone/retry HARD RULES are in core below.)
- E2E tests/seeds/migrations/projection closeout -> `docs/agent-rules/e2e-seeds-projections.md`.
- RBAC/scopes/logins/leadership assistant -> `docs/agent-rules/rbac-seeds-access.md`.
- CI/push gate/landing/releases -> `docs/agent-rules/ci-landing-release.md`: CI outage never blocks closure, exact-SHA push gate, main landing, `make release-tag`.
- Cloud/GCP/Google auth/GitHub/repos -> `docs/agent-rules/cloud-org-github.md`: billing console, account selection, Codex auth, sibling repos. (Org boundaries, token path and commit identity are in core below.)
- Read-first list/code navigation tools -> `docs/agent-rules/code-navigation-tooling.md`.
- Defect/audit-ledger closure -> `docs/agent-rules/defect-ledgers.md`.
- Morning README, workflow docs, ops mock auto-push -> `docs/agent-rules/engineering-conventions.md`. (Do-not list and validation are in core below.)

## PR Review + Land Main Rule

When the maintainer asks to review a GitHub PR and land main, the task is not
done after pushing the certified commit to `origin/main`. After local CI passes
and `make land-main` lands the commit, also resolve the GitHub PR itself:

1. Verify the PR head branch and `origin/main` both point at the landed SHA, or
   merge the PR through GitHub if it is still mergeable and not already landed.
2. If the PR branch is stale but the exact PR content is already in `main`,
   update the PR head branch to the landed SHA so GitHub closes the PR as
   resolved.
3. Report the PR state separately from the main SHA. If GitHub cannot mark it
   "Merged" because the branch already equals `main`, say that explicitly.

## Ravi Laptop Default: OCI DB, Not Local Docker Postgres

On Ravi's laptop, default local Goat OS backend/admin-web development to the OCI
Postgres tunnel when it is available:

```text
Database: postgres://postgres:${REMOTE_POSTGRES_PASSWORD}@127.0.0.1:15432/goatos?sslmode=disable
Tunnel:   127.0.0.1:15432 -> OCI VM 127.0.0.1:5432
```

This default is only for local Goat OS development and staging-like clone work.
If Ravi explicitly says `goatos-stg`, `stg db`, `staging database`, or asks to
check the real staging database, do not use OCI as a substitute. Use the real
GCP `goatos-stg` Cloud SQL database through Secret Manager and Cloud SQL Auth
Proxy, and state that the query is against `goatos-stg:asia-south1:goatos-stg-core-db`.
The OCI tunnel may be used only for a separately named comparison after the
real staging result is already checked.

Current OCI dev VM connection details, credentials, and recovery metadata must
live outside git. Resolve them from the operator's local environment or Google
Secret Manager; do not commit account names, public IPs, laptop home paths, SSH
keys, or Postgres passwords.

Do not install or start Colima, Docker Desktop, Docker CLI, Lima, `goatos-local-current`,
or any other local Postgres container just because older local-stack docs mention
`5433` or a CI gate asks for Docker. Before any Goat OS work that appears to need
Docker/Colima on Ravi's laptop, first check whether the OCI tunnel on `15432` is
active and whether the task can use OCI instead. For `make land-main`,
`validate-sqlc-plans`, query-plan proof, or any other disposable Postgres proof,
use an OCI-hosted throwaway DB/container and clean it after the landing attempt;
do not install Docker/Colima locally as the workaround. Use local Docker Postgres
only when the maintainer explicitly asks for a disposable/local Docker DB, a
Docker-specific integration test, or an isolated mutation test that must not touch
OCI/staging-like data. If a stale `goatos-local-current` container or Colima VM is
running while the active dev stack uses OCI, stop it instead of treating it as
canonical.

**HARD RULE - OCI/STG E2E data repair is delta-only.** Ravi's OCI database is a
maintained staging-like clone, not a disposable target. For any OCI/STG E2E,
parity, or validation task, first identify exactly which tables/rows differ from
STG and repair only that delta. Do not replace, reset, drop schemas from, or
full-restore the entire OCI database from STG unless the maintainer explicitly
asks for a full refresh using those words after being told it will overwrite the
OCI database. A normal request to "make OCI match STG" means: run a diff, capture
the mismatched table/row delta, apply the smallest targeted SQL or copy for that
delta, then re-run parity.

## Manual DB Writes Must Carry Actor + Reason (Claude AND Codex)

Every direct write to goatos-stg (or OCI) from psql, psycopg2, or a script is
recorded in `audit.db_changes` by triggers (migration `000387`). Wrap every such
write in one transaction that starts with
`SELECT audit.begin_change('<person> via <claude|codex|psql>', '<purpose + link>');`,
and connect with a non-`goatos-` application name (`PGAPPNAME=claude`,
`application_name="codex"`). Never use a `goatos-*` application name, never set
`session_replication_role`, and never disable, drop, or edit the audit triggers or
`audit.db_changes`. After the write, show the maintainer the resulting
`audit.db_changes` rows as proof. Details: `docs/runbooks/manual-db-change-audit.md`.

## Never Kill Another Agent's Build — and Never Wait For One (Claude AND Codex)

Gradle is NOT a lock. Separate worktrees run separate daemons and build concurrently.
The 2026-08-03 deadlock that cost 90 minutes was agents **killing each other's
workers** and each restarting — not contention over a shared resource.

Rules:

1. **Build when you need to.** Do not serialize, do not ask permission, do not wait
   for someone else's build to finish. Use `--max-workers=1` so a parallel build does
   not eat the machine.
2. **NEVER kill another process's Gradle workers or daemons.** `pkill -f
   GradleWorkerMain` is banned unless you started that build yourself and it is dead.
   Reap only YOUR OWN orphans, after your own killed build.
3. **NEVER wait-loop on a resource.** A wait loop that outlives its condition is worse
   than a failure: on 2026-08-03 two agents sat waiting on ORPHANED workers from a
   build that had already died, so the wait could never end. If something you need is
   busy, do the work that does not need it and report the blockage.
4. **Surface a genuine block to the maintainer immediately** — name the resource and
   the holder so they can decide. Never absorb it into a status line as "still
   running". A long-running agent card may also be STALE: verify against the process
   table or the branch, not the card.
5. **Exit 137 from Gradle is an OOM SIGKILL** from memory pressure, not a test failure.
   Re-run once with `--max-workers=1`; if it recurs, report it rather than looping.

Generalizes to any shared thing (Docker, a port, a device, the local stack): parallel
use is fine, killing someone else's is not, and waiting forever is never the answer.

## Fast Lane for Tiny Fixes

When the maintainer asks to make a small, low-risk fix and land it on `main`,
optimize for elapsed time. Do not run the full local CI matrix, mobile install,
cloud deploy, browser proof suite, or graph/document maintenance unless the
change actually touches that surface or the maintainer explicitly asks for it.

Default verification should be the narrowest command that proves the touched
surface still works. Examples:

- Android Kotlin-only UI or view-model edit: run the targeted Gradle compile or
  targeted unit test; install to a physical device only when device behavior is
  the thing being verified.
- Android weighing list edits: repeated Compose row keys must include full
  work/category/period identity, not only `campaignShedId`; run
  `WeighingRouteIdentityTest` for this guardrail.
- Android weighing assignment-card date edits: delayed backlog rows carry both
  original `planned_business_date` and rolled/current `due_business_date`. Show
  the operator **Delayed** with the original planned date; do not make old
  backlog look newly scheduled for today. Run
  `WeighingAssignmentModeAwarenessTest`.
- Android vaccination proof-list edits: proof-needed rows are obligation-grain,
  not goat-grain. Key by `obligationId` before `goatId`, and run
  `ScanProofIdentityTest` plus `make android-compose-lists-guard`.
- Admin-web component/style edit: run the relevant typecheck/test/lint slice or
  a focused browser check, not the whole product suite.
- Docs/copy/config-only edit: inspect the diff and run format/schema validation
  only if that file type has one.

Before pushing, verify repo, remote, active identity, branch/head, and dirty
state. Avoid detached-HEAD limbo for ordinary work: use the current branch when
it is safe, or push the verified commit explicitly with `git push origin
HEAD:main` when the maintainer asked to land directly on `main`. Never include
unrelated proof files, screenshots, temp folders, or local artifacts in the
commit.

When the maintainer asks whether a fix was pushed or why it was not pushed,
answer the status plainly first and do not argue. If the maintainer's intent is
to land the already-reviewed/focused fix on `main`, do the repo/identity/dirty
state checks and push the scoped fix to `main` instead of stopping at an
explanation. If the worktree contains unrelated dirty files, isolate only the
fix files in the commit/push path or state the concrete blocker.

If `main` push is rejected by the landing gate, **do not stop at "can't push to
main."** Run `make land-main` from a clean isolated worktree, inspect every named
failure, fix branch-owned blockers, commit them, push the branch, and rerun the
gate. Repeat until the exact SHA lands on `main` or the remaining blocker is a
real external prerequisite the agent cannot change (for example a missing local
OCI tunnel/VM credential, expired cloud auth, or an unavailable maintainer-owned
service). A missing local Docker binary is **not** a blocker on Ravi's laptop:
follow the OCI-DB rule at the top of this file and use OCI-hosted disposable
Postgres/query-plan proof instead of asking for or installing local Docker. If a
gate prints `docker: command not found`, first look for its OCI/admin-DSN
override (for example `GOATOS_SQLC_PLAN_ADMIN_DSN` for query-plan proof) and run
that path; do not report local Docker absence as the reason `main` cannot land.
Even then, report the specific prerequisite and the exact command/output that
proved it; do not present a guard failure as the final answer while fixable
blockers remain.

Report the verification boundary honestly and briefly. If only a narrow check
was run, say so; do not spend 20 minutes manufacturing confidence for a one-line
change.

## Main Merge Requires Exact-SHA CI Evidence

No PR, GitHub UI merge, connector/API merge, merge queue action, or direct push
may put code on `main` unless one of these is true for the exact commit being
landed:

1. `make land-main` completed green from a clean isolated worktree.
2. GitHub `ci` completed green for the exact current PR head SHA after the
   branch was rebased onto fresh `origin/main`.

Pending, failed, cancelled, stale, skipped, or targeted-only checks do not
authorize a merge to `main`. Targeted local checks are review/preflight evidence
only. If neither exact-SHA proof exists, do not merge; run `make land-main`
locally or wait for/dispatch GitHub CI and verify the exact SHA is green first.

This is enforced server-side, not only by local hooks. `make land-main` posts a
`goatos/land-main-receipt` commit status on the exact certified SHA after
`ci-local` is green, and the GitHub `main` ruleset requires that status on
every update of `main`, direct pushes included, with no bypass actors. So
`gh pr merge`, the GitHub merge button, the GitHub MCP `merge_pull_request`
tool, or `git push origin main` from a machine without the pre-push guard all
fail at GitHub. Nothing runs on GitHub; `ci-local` on the developer machine is
still the only CI. Do not post that status by hand and do not add bypass actors.

### Run Android once, not three times (maintainer rule 2026-09-24)

A full Android slice takes a long time. Do not stack runs before landing:

- Do not pre-run `tools/ci/run-local-ci.sh android` and then `make land-main`.
  `make land-main` IS the Android run. Commit and run it.
- When `make land-main` fails in Android, rerun only the exact failing Gradle
  task, the same one CI runs, then commit and run `make land-main` once. CI runs
  `:app:testStgReleaseUnitTest` / `:app:lintStgRelease` (stg release), so a
  focused `testDevDebugUnitTest` cannot reproduce or clear a stg failure.
- Tests that assert dev-only behaviour (TEMP sample cards, debug aliasers) go in
  `app/src/testDev/` or `app/src/testDebug/`, never the shared `app/src/test/`,
  which stg release runs too.
- Ledger/metadata-only follow-up commits (the `commit-classification/` ledger)
  are common-only and no longer block landing. Don't add them just to go green.

## MANDATORY: 4-Layer Lookup on Every Code Question

Work through layers in order. Stop at the layer that answers the question. Do NOT jump to files/grep first.

### Layer 1 — CRG (code structure)
For callers, callees, imports, blast radius, architecture, dead code, test coverage:
```
repo_root: <absolute path of your goatos checkout>   # git rev-parse --show-toplevel

Cold/review/diff task      -> get_minimal_context_tool, then one targeted graph query
Known-symbol traversal     -> query_graph_tool callers_of/callees_of/imports_of/tests_for
Keyword/domain lookup      -> semantic_search_nodes_tool, then query_graph_tool
Changing code              -> detect_changes_tool + get_impact_radius_tool
Single file/function read  -> read the file; use graph only if impact is unclear
```

### Layer 2 — Graphify (business/product context + technical docs)
Two graphs. Query both in parallel:

**mesha_docs_graph** — wiki SOPs, farm workflows, vaccination protocols, org model:
```
MCP: mesha_docs_graph
CLI: uvx --from 'graphifyy[mcp]==0.8.44' graphify query "QUESTION" \
       --graph /Users/ravi/mesha/graphify-out/graph.json
```

**goatos-docs graph** — TRDs, ADRs, phase docs, obligation engine, skill references (locally generated; run `make ai-rebuild-docs` if missing):
```
CLI: uvx --from 'graphifyy[mcp]==0.8.44' graphify query "QUESTION" \
       --graph ./graphify-out/graph.json
```
When built, it covers protocol engine, Preventive Care (PC) vaccination, feed direction,
frontend scope, analytics infra, execution plans, observability, auth, SOP
cutover, and skill references.

### Layer 3 — Skill references (architecture decisions, TRDs, phase contracts)
When CRG + Graphify don't cover it — deep implementation rules, phase PRDs/TRDs,
OpenAPI contracts, form DSL, analytics infra, security/ops rules:
```
Load .agents/skills/goatos-build/SKILL.md → pick only the relevant reference doc
Do NOT load all reference docs — let CRG + Graphify narrow which one applies
```

### Layer 4 — Grep/Read (CRG blind spots)
Only for what the graph cannot see:
- HTTP route strings (`r.GET("/api/v1/...")`)
- Middleware wired via reflection or string keys
- Config/env values and constants
- SQL query strings
- Uncommitted/unstaged code
- Any `callers_of = 0` result that seems wrong — verify with grep

## Production-Facing Environment Decision

Current operator-facing production cleanup uses the existing `goatos-stg`
Google/Firebase project internally. Do not infer from the project id that public
surfaces should keep staging names. Public app, browser, and operator-facing
surfaces must use production names:

- Android package: `sg.mesha.goatos`
- Dashboard: `https://dashboard.mesha.sg`
- API: `https://api.goatos.mesha.sg/` unless the maintainer explicitly chooses a
  different prod API host in the same request
- Firebase Auth issuer/audience may still be `goatos-stg` while the existing
  Firebase project is reused. This is internal auth plumbing, not public naming.

When editing docs, skills, release notes, app config, or deploy guidance for the
current live operator path, describe it as production-facing even if the backing
GCP/Firebase project id is `goatos-stg`. Keep historical incident/runbook facts
unchanged only when they are explicitly about the old staging environment.

## STG Deployment Contract

For Goat OS, STG deploy is NOT GitHub Actions and NOT PR-driven.

GitHub Actions is billing-blocked and must not be used for deployment
(validate only via `make ci-local` on the pushed SHA).
Do not create main→stg PRs as a deploy mechanism.
Do not force-push a `stg` branch and wait for CI.
Do not infer CI deployment from branch names.

If STG shows Google Frontend `429 Rate exceeded` after a paid/restored Google
bill, do not guess or redeploy app code first. Read and follow
`docs/runbooks/stg-cloud-run-billing-recovery.md`: verify `ravi@mesha.sg`,
`goatos-stg`, `billingEnabled: true`, Cloud Run service readiness in
`asia-south1`, and finish with both terminal curls and live Chrome verification.
The 2026-08-26 maintainer baseline for `goatos-api-stg` is min-instances `2`
and max-instances `2`.

Authoritative STG deploy path:
1. Read `docs/runbooks/stg-deploy.md` (short contract) →
   `docs/runbooks/cloud-deploy-staging.md` (full Cloud Deploy mechanics).
2. Use the Slack deploy button in `#goatos-stg-deploy`. The button triggers the
   Google Cloud Build manual trigger `goatos-stg-deploy-main`, which reads
   `cloudbuild.stg.yaml` and creates the Cloud Deploy release from `origin/main`.
3. Verify active account is `ravi@mesha.sg`.
4. Verify target org is `vgoats.com` and environment is Goat OS STG
   (`goatos-stg`).
5. Never use Slice/Heva GitHub identity or cloud project for Goat OS.

If a user asks to "push to STG", "promote STG", or "deploy STG", this means:
use the Slack button/Cloud Build route from the latest approved `origin/main`,
following the runbook. Do not run a local deploy unless the Slack/Cloud Build
route itself is broken and the maintainer explicitly asks for break-glass.

If a user asks whether STG deploy is done, failed, or stuck, check the Cloud
Build run started by the Slack bot first, then the Cloud Deploy release/rollout
linked from that build. Do not infer status from local shell output or branch
names.

If a user asks to "publish Firebase", "upload to Firebase", "Firebase App
Distribution", "release Android", "push the APK", "internal test", "Play
internal testing", or includes an Android APK/AAB as part of a STG deploy, the
Android release is not complete after Firebase App Distribution alone. Follow
`docs/mobile/production-facing-release.md` and publish the employee/internal release to
all required channels: Firebase App Distribution, Google Play Internal Testing,
and the stable operator URL `https://mesha.sg/app.apk`. That URL redirects to
`gs://goatos-stg-public-downloads/operator/latest/app.apk`; do not copy APKs
into the Mesha marketing website repo and do not deploy Firebase Hosting merely
to update the APK. Use the exact same generated APK bytes for Firebase App
Distribution and the Storage mirror. Play Internal Testing uses an AAB, so
build/upload it from the same source commit, `versionName`, and `versionCode`;
do not invent a second release identity. Keep the Play internal tester list to
the same email IDs that have access to Firebase App Distribution; do not
maintain a separate hand-picked Play tester list. Keep the browser download
filename versioned as `Mesha-<versionName>.apk`, verify matching APK hashes, and
validate the live versioned URL in Chrome before reporting done. The same mobile
distribution must publish Firebase Remote Config
`min_supported_version_code=<released versionCode>` and
`update_url=https://mesha.sg/app.apk`, then read the values back. If that
force-update floor publish fails, the mobile distribution is incomplete even if
Firebase App Distribution, Play Internal Testing, and the APK mirror succeeded.

Do not ask whether to use GitHub Actions, PR merge, or force-push `stg` unless
the user explicitly asks to change deployment architecture. The machine-readable
form of this contract lives at `context/deploy-contract.json`.

## Whole-Packet Review Scope (Mandatory, Claude AND Codex)

When the maintainer gives a review/fix/landing packet, treat the entire packet as
the task goal until proven otherwise. That includes PR numbers and merge state,
screenshots or attached docs, pasted reviewer notes, prompts, fixes claimed by
other agents, lenses, judges, sub-agent briefs, branch/base SHAs, and any
maintainer corrections in chat. Do not narrow the task to only the first visible
diff, only `origin/main`, only one PR, or only a screenshot table unless the
maintainer explicitly says to ignore the rest.

Before reporting "pending bugs only", "already fixed", "not a bug", or "nothing
to push", reconcile every finding against the complete packet and the current
candidate SHA. If a document says a finding was fixed by a later PR/SHA, verify
that later PR/SHA is actually in the reviewed candidate. If the maintainer asks
whether PR 115 was reviewed, answer from evidence that includes 115, not from a
stale main checkout. If the packet names lenses or judges, run or inspect those
review surfaces as first-class acceptance criteria, not optional commentary.

## Root-Cause Fixes Only — No Partial / Surface Fixes (Mandatory, Claude AND Codex)

When fixing ANY reported bug (review finding, audit item, regression):

1. **Reproduce the EXACT failure FIRST.** Write a failing test that reproduces the
   precise scenario described (the retry path, the race, the production caller, the
   >cap input), and confirm it FAILS on current code. No fix without a red test that
   models the real failure — not the cited line in isolation.
2. **Fix the ROOT CAUSE, not the symptom.** Trace the actual PRODUCTION path. Do NOT
   patch a sibling method, an adjacent symptom, or the one line quoted and declare
   done. If the production caller invokes a different method than the one you changed,
   you have not fixed it.
3. **A green narrow unit test is NOT proof** if it does not exercise the production
   caller, the retry/partial-failure/edge path, or the concurrency race. Prove the fix
   on the real path.
4. **Never report "fixed" / "already fixed"** without pasting failing-then-passing
   evidence on the real path. "Looks fixed", "compiles + tests pass", and "the guard is
   green" are NOT closure. Verify against the exact failure condition the reviewer gave.
5. Applies to sub-agents too: an orchestrator MUST independently re-verify each
   sub-agent's claim (run the failing test on old code, confirm it fails; on new,
   confirm it passes) before landing — sub-agents have repeatedly done shallow
   "already fixed" passes.

## Workspace Orientation, Purpose and Scope

Purpose:

- Goat OS is the operating system for mixed-species herd-animal identity, health, vaccination, genetics, breeding, workforce, SOP tasks, media proof, verification, devices, commerce interfaces, and analytics.
- Canonical backend/data model/app APIs are built fresh.
- `Goats and Parks.docx` is the base source for herd-animal and park semantics
  across every slice. Any feature touching herd-animal identity, species/breed
  labels, park/shed scope, shed tags, lifecycle/stage, pregnancy/lactation/
  warm-up/fattening, feed safety, weighing, handling, medicine administration,
  park roles, or feed sessions must start from
  `context/source-findings/goats-and-parks-source-findings.md` and must not
  invent conflicting semantics. Feature-specific docs may add stricter
  source-backed rules, but conflicts require an explicit source/owner decision.
- Scope lock: build exactly the user-approved slice, not adjacent product areas
  that the shared platform could theoretically support. Generic foundations are
  allowed only when they serve the approved slice; visible routes, nav, seeded
  cards, mock data, screenshots, and handoff language must not imply another
  vertical is built. For the current admin-web review, the visible slice is Preventive Care (PC)
  Vaccination plus Admin/Data Ops config and vaccination SOP policy.

- **Staging deployment is Slack-triggered Cloud Build into Cloud Deploy.** Do
  not create or wait for a `main -> stg` pull request, GitHub Actions workflow,
  or direct `stg` branch push as a deployment mechanism. Agents must use the
  `#goatos-stg-deploy` Slack button, which runs Cloud Build trigger
  `goatos-stg-deploy-main` from latest approved `origin/main`; manual scripts
  under `tools/deploy/stg-clouddeploy-*.sh` are break-glass/repair mechanics.
  Never push any local ref, local `stg`, `main`, `HEAD`, agent branch, or
  refspec directly to remote `stg`; the branch is not deployment authority. Run
  `make ai-setup` so the local guard blocks accidental remote `stg` writes. Do
  not bypass it with `--no-verify`.

## Every Visible Date Is DD/MM/YYYY (maintainer lock, 2026-09-10)

Every VISIBLE date on EVERY surface renders **`DD/MM/YYYY`**, with slashes, in full:
admin-web tables, cards, drawers and CHART AXES; Android list chips, card subtitles
and the timestamp burned into a proof video; and any date string the BACKEND
composes for a screen, because the backend owns visible copy. Timestamps render
`DD/MM/YYYY HH:MM`.

There is deliberately NO compact variant. A chart axis and a phone chip render the
same shape as a table cell, so a reader never has to learn a second date format to
compare two screens.

This SUPERSEDES the 2026-08-21 `DD-MM-YYYY` (dash) rule, which governed admin-web
ALONE. Three surfaces disagreed about one fact: the console said `14-08-2026`, its
own chart axis said `14-08-26`, the phone said `14 Aug`, and the phone's verify
queue said whatever the DEVICE LOCALE produced. The backend was already right --
`biztime.FarmDate` has emitted `02/01/2006` in notification copy all along -- so
this change makes the two clients agree with the backend rather than inventing a
fourth shape.

USE THE ONE HELPER PER SURFACE; do not hand-roll a date:
`apps/admin-web/lib/format.ts` (`fmtDate`/`fmtDateTime`), Android
`core-common/.../datetime/GoatOsDates.kt`, backend `biztime.FarmDate` /
`FarmDateFromBusinessDate`.

**WIRE FORMATS STAY AS THEY ARE, and this is the load-bearing half.** ISO
`YYYY-MM-DD` business dates, RFC3339 instants, React keys, idempotency keys, event
keys, export filenames, EXIF/signed-URL timestamps and SQL parameters are NOT
display: switching one to slashes corrupts a key or a query parameter. A change
that makes a display helper call a wire helper, or the reverse, is a defect even
when the screen looks right.

NOT DATES, and they keep their own form: a TIME on its own (`HH:mm`, `h:mm a`), a
MONTH heading (`Aug 2026` -- no day component, so `DD/MM/YYYY` is undefined for it),
and a bare WEEKDAY (`Mon`).

Machine gate: `make date-format-guard`
(`tools/agent-hooks/check-date-format.mjs`, in `make guardrails` and `make
ci-local`) -- canaries on all three shared helpers plus scans that catch a screen
hand-writing its own date shape (a JSX text node shipping a bare `*_date` field, a
Kotlin `ofPattern("d MMM")`, a Go word-month layout). Its self-test is adversarial:
it asserts the guard REJECTS the retired dash form, the retired compact axis, every
retired Android pattern and a Go word-month layout, while PASSING wire formats.
`date-format-guard:ignore: <reason>` exempts a genuine machine format and requires
a stated reason. Canonical prose and the stated blind spots:
`docs/decisions/date-display-format.md`.

## The Word On Screen Is PEN, Never SHED (maintainer lock, 2026-09-02)

Every user-visible string says **pen**. The word *shed* appears on no screen a person reads --
page and table titles, column labels, filters, chips, KPIs, empty states, notes, tooltips,
drawer copy, user-facing error text, CSV export headers and download filenames.

This is a VOCABULARY decision and nothing else. Behaviour, grain, schema and API contracts do
not change because of it. A change made in the name of this rule that alters what the software
DOES is wrong.

**What stays `shed`, deliberately and permanently:** column KEYS (`shed`, `shed_tag`), copy KEYS
(`kpi.sheds.label`), table/section ids (`shed-weights`), route paths (`/vaccination/sheds`),
every database table/column/enum (`subject_type='shed'`, `position_code='shed_manager'`), and
every Go/TS identifier and JSON wire field (`shed_id`, `shed_name`). Do NOT rename these to
match the label. The mismatch between the stored word and the shown word is the design.

**Copy comes from FOUR places and a new screen must get all four right** -- missing one is how
the first pass shipped eleven tables still saying "Shed" on pages whose own copy said pen:
(1) the page-contract copy maps in `adminui/app/service.go`; (2) `humanLabel()` in the same
file, which DERIVES column labels from the column key rather than reading the copy map;
(3) sentences composed in a producing module's Go or SQL (`weighing/domain.CorrectedSubjectLabel`
"Whole pen", counts shifting "Pen move", the verification batch label, the calendar subtitle);
(4) seeded rows in the database, which need a forward migration. Plus position TITLES,
prettified from `position_code` in `workforce/app.formatPositionCode`.

**Three places the two words meant different things, and substitution was WRONG.** Feed Config's
multiplier is a **FEED factor**, not a pen factor: `feed_shed_factors` has no partition column,
so one row scales every pen in the building and "pen factor" would be false. Feed TRANSPORT is
shed-grain on purpose (one trip per building, migration 000152), so its copy says "physical
location" -- writing "pen" states the opposite of the rule it explains. Explainers that existed
only to relate the two words ("an undivided shed is its single pen") are circular with one word
and are DELETED, not reworded. General form: when a sentence needs both words, name the thing
accurately without either noun, or drop the clause -- never substitute.

**The trap that can silently corrupt data:** `pen` ALREADY MEANT PARTITION in the animal bulk
importer (`identity/app.normalizeHeader`: `pen`, `pen_label` -> `partition_label`). The CSV
template header is built from the option LABELS and parsed by header NAME, so labelling the
location column "Pen" would file a pen name into `partition_label` on every row -- no error, a
wrong location on every animal. It is labelled **"Pen name"**; bare `pen` keeps its meaning.
Generalise: a template header label IS a parser input. Add the new name as an ALIAS and keep
the old one, or every sheet already saved stops importing.

**THREE SHAPES A SWEEP MISSES, all found by RENDERING the pages rather than re-reading the
source.** (1) BARE LOWERCASE NOUNS -- `"pager.noun": "shed"`, `"schedule.unit.sheds": "sheds"` --
which a sweep keyed on "has a space or starts with a capital" skips entirely, leaving pages
reading "1-25 of 104 sheds" while the contract scan comes back clean. (2) JSX TEXT NODES --
`<option>All sheds</option>`, a bare `Shed` label line -- which are not quoted, so a
string-literal sweep cannot see them at all. (3) FARM DATA -- `procurement_vendor_catalog` holds
the vendor category "Sheds Contractor", someone who BUILDS sheds; that is the farm's word about
the outside world, not the product's word for a pen, and changing it is a maintainer decision.
The rule that follows: decide by POSITION, never by spelling. The guard skips a named set of
machine leaves (key, id, href, icon, data_source, param, columns) and treats everything else as
copy, so an unanticipated copy shape fails closed instead of slipping through.

Enforced by `adminui/app.TestBootstrapContractSaysPenNeverShed` (walks the whole served
bootstrap JSON, keyed on PATH not spelling; mutation-tested),
`admin-web features/counts/pen-vocabulary.test.mjs` (JSX text nodes; mutation-tested),
`TestColumnLabelsSpeakPenWhileTheKeysStayShed`,
`identity/app.TestImportHeaderAliasesSurviveThePenRename`,
`workforce/app.TestPositionTitlesSayPenWhileTheCodesStayShed`, and admin-web
`features/counts/pen-import-headers.test.mjs`. NOT yet done: the Android app's ~471 own
hardcoded "shed" strings, tracked separately. Canonical prose:
`docs/decisions/pen-not-shed-vocabulary.md`.

## Cross-Cutting Hard Rules (UI proof, retries, phone rendering)

**HARD RULE - No circular OCI/E2E retries.** Before rerunning any long OCI DB,
generation, Chrome E2E, CI, or landing command after a failure, identify the
specific changed condition that makes the retry different: a code patch, data
repair, tunnel repair, config change, or narrower diagnostic. Use an explicit
timeout and capture the terminal result. If the same command fails twice with
the same blocker, stop repeating it and switch to diagnosis or report the exact
blocker; do not start another blind long run.

**HARD RULE - UI fixes require real-surface proof after the final edit.** Claude
and Codex must not call a UI fix done from code/tests alone. For any
`apps/admin-web` browser-visible change, reload Chrome on the exact target URL
after the last code edit and verify the changed UI is actually rendered there.
Chrome proof must use the actual frontend and backend servers running from the
checkout/branch being coded or reviewed; opening a generated PNG, HTML mock,
fixture, local file, or previously captured artifact in Chrome is not proof of
the live app. When the proof depends on production-like counts, sales, or other
analytics data, first verify the selected proof database contains the same
relevant rows/aggregates as the authoritative staging database for that feature,
then capture the live route against that database. The proof packet must include
desktop and mobile/narrow Chrome screenshots from the running route, visually
inspected for copy, alignment, colors, spacing, overflow, and table/card
consistency.
For any Android/operator-mobile change, open the app on the physical phone or
emulator target required by the task and verify the changed screen there.
Static tests, typecheck, backend API checks, and screenshots from before the
last edit are not enough.

**HARD RULE - ADB text is literal, not URL-decoded.** When entering credentials
or any literal text with `adb shell input text`, never encode `@` as `%40`;
`adb input text` types `%40` literally. Use a literal escaped at-sign such as
`natheswar7\@gmail.com`, then verify the field text in the UI hierarchy before
tapping submit/sign-in.

**HARD RULE - UI work requires BOTH visual regression and E2E.** For every
browser-visible `apps/admin-web` change, after the final code edit and before
reporting done or pushing as ready, agents must complete both checks on the real
surface:

1. Visual regression proof: open the exact changed route in Chrome, capture the
   rendered screen after the final edit, inspect it for layout/copy/state
   regressions, and compare it to the authoritative mock/design or the user
   screenshot that reported the defect.
2. Click-through E2E proof: exercise the changed controls end to end in Chrome,
   including disabled/enabled states, changed checkbox/select/input values,
   preview/apply/save/publish buttons, close/cancel paths, and the expected
   backend result or blocked-safe boundary.

Do not stop at one of the two. A screenshot without clicks is not E2E; a passing
click path without a post-edit screenshot is not visual regression. If either
check is blocked by server startup, auth, data, network, or a browser-control
failure, diagnose and fix the blocker when it is in-repo or local-state
controllable. Only report "blocked" after naming the exact blocker and the exact
command/browser step that proved it. Do not say the UI work is done until both
visual regression and E2E are actually complete after the final edit.

**HARD RULE - Admin-web must render on a PHONE (maintainer rule 2026-09-14, Claude
AND Codex AND humans).** The dashboard is opened on phones as well as laptops, so
every browser-visible `apps/admin-web` change -- a page, a table, a chart, a KPI
strip, a drawer, a modal -- is proven at TWO widths, not one: the laptop mock
width (1440px) AND a phone width (390px, Chrome device emulation). At phone width
nothing may be clipped, nothing may be broken, and the page body must never scroll
sideways; a wide table or chart may exceed the screen only inside its own
`overflow-x: auto` wrapper a thumb can pan. The visual-regression proof above
therefore includes a phone-width screenshot of the changed route after the final
edit, next to the desktop one, and the click-through E2E is exercised at phone
width too when the changed control is one a phone user would tap. Two halves
enforce it: `make admin-web-phone-viewport-guard` (in the admin-web `ci-local`
job; a static, count-ratcheted scan for fixed px widths >= 480 on non-scrolling
boxes, grids whose px floor exceeds a phone, and `overflow-x:hidden` on page-level
elements -- growing its baseline to land a finding is not accepted), and the 390px
mobile lane of `npm --prefix apps/admin-web run smoke:visual:live`, which is the
only thing that sees data-dependent overflow. A change proven only at laptop width
is not proven. Canonical prose: `docs/decisions/admin-web-phone-viewport.md`.

For any change that touches `apps/admin-web` Weights UI, Weights page copy,
Weights charts, generated API contracts used by Weights, or backend read-model
data consumed by `/weighing/weights`, verify the local Chrome page is not on
`ERR_CONNECTION_REFUSED`, not showing backend-down copy, and not showing the
React "Something went wrong" fallback. For chip/label/calendar changes, verify
the actual rendered row labels, chips, calendar markers, and tooltip/info text
in Chrome. If Chrome/phone verification is blocked, say it is blocked; do not
present the UI fix as verified.

## Organization Boundaries, GitHub Token Path and Commit Identity

Organization boundaries:

- Mesha/VGoats, Heva, and Slice are separate businesses and must never be
  mixed in GitHub or Google Cloud operations.
- Goat OS belongs to Mesha/VGoats. Google Cloud work for Goat OS targets the
  `vgoats.com` organization and future `goatos-dev`, `goatos-stg`, and
  `goatos-prod` projects.
- Do not use Heva projects/orgs, Slice projects/orgs, or `hevaplatform` for
  Goat OS work.
- Current active Goat OS agent/tooling and Firebase project is still
  `goatos-stg`, but it backs the current production-facing cleanup path. Do not
  create, update, read, grant IAM on, or store agent/tooling secrets in
  `goatos-dev` unless the user explicitly says `goatos-dev` in the same request.
  For Context7, Gemini/Graphify, Claude/Codex bootstrap, and local agent docs,
  `goatos-stg` is mandatory. Public URLs/app package/release labels must use
  production-facing names, not staging names.
- Do not modify or replace the legacy `goatos-sheets` project while creating
  Goat OS projects.
- Before any cloud/GitHub command that creates, updates, deletes, grants IAM,
  links billing, deploys, or changes configuration, verify and state the active
  account, organization, folder, project, and target repo. If the target is not
  Mesha/VGoats for Goat OS work, stop and correct context first.
- If any Google auth surface expires or cannot refresh non-interactively
  (`gcloud`, ADC, Cloud SQL Auth Proxy, Secret Manager, Google Drive/Docs/
  Sheets, or a Google browser session), do not stop at "token refresh failed"
  and do not silently switch to OCI, a clone, or a service-account workaround
  when the task requires Google access. Use browser-based reauthentication
  immediately: `gcloud auth login` for CLI user credentials,
  `gcloud auth application-default login` for ADC, or the relevant browser/
  connector sign-in for Drive/Docs/Sheets. After reauth, re-verify the active
  account, organization, project, and target before any write/deploy/config
  mutation. For Goat OS, use the expected Mesha/VGoats account and organization
  from the task-specific runbook. Use a service-account fallback only when Ravi
  explicitly asks for it or the exact runbook requires it.
- For read-only Google-backed data pulls, Cloud SQL queries, dashboard issue
  CSVs, or any request phrased as "use gcloud/browser login", follow
  `docs/runbooks/google-cloud-environments.md` -> `goatos-stg Read-Only Cloud
  SQL Access` before touching Chrome or dashboard UI. The default source is
  gcloud + Secret Manager + Cloud SQL Auth Proxy + Postgres, not dashboard DOM
  scraping. If the user names `goatos-stg` or `stg db`, that explicit target
  wins over the Ravi-laptop OCI default: query `goatos-stg` Cloud SQL, not the
  OCI staging-like clone.
- For GitHub operations in this repo, use the Mesha/VGoats repository token
  path: `git mesha-push main` for pushes and the `MESHA_GITHUB_PAT`-backed
  remote URL for direct remote/CI verification. Do not rely on whatever `gh`
  account is active; this workspace may also have Heva and Slice GitHub
  accounts configured, and those must not be used for Goat OS repo authority.
- Git commits from this repo must use a Mesha identity only. Before committing
  or landing, `git config user.email` must end in `@mesha.sg`; Heva, Slice,
  gmail, or personal identities are blocked by `make git-identity-guard` and
  the local CI common gate. The expected maintainer identity is
  `Raviteja <ravi@mesha.sg>`.

- Create Goat OS cloud resources under `vgoats.com`, preferably in a `goat-os`
  folder, or directly under the org if folder creation is not available. Do not
  create Goat OS resources inside `system-gsuite` or `apps-script`.

## Do-Not List and Validation Expectation

Do not:

- Do not reintroduce old staging labels as architecture.
- Do not commit generated Graphify/CRG graphs. `graphify-out/graph.json`,
  `manifest.json`, `GRAPH_REPORT.md`, `graph.html`, `cost.json` and the
  `.code-review-graph/` DB are gitignored and machine-regenerated locally. Commit
  ONLY the setup docs, rules, hooks, and generation scripts — never the graph
  artifacts themselves. Run `make ai-doctor` before pushing AI-tooling changes.
- Do not let the `ceo_ai` reporting/assistant namespace sit between the core
  Backend <-> Frontend <-> Mobile layers. `ceo_ai` is the leadership-assistant
  chatbot (`backend/internal/ceoai/**`, `/api/ceo-ai/*`) plus its read-only
  reporting SQL schema (`ceo_ai.*` views/functions read by Cube and the
  assistant). Data flows ONE way: core BE is the operator source of truth, and
  the assistant/Cube CONSUME it via Mesha read APIs, the MCP Toolbox, or
  read-only SQL. A core operator read path must never join `ceo_ai.*` or read a
  `ceo_ai_*` table for its own runtime data (this once 500'd Control Tower when
  the schema was absent — SQLSTATE 3F000). Shared display/derivation logic (e.g.
  vaccine labels) lives in a neutral core package such as
  `backend/internal/vaccination/domain`, read by both operator screens and the
  assistant. Frontend/mobile core pages must not route their data through
  `/api/ceo-ai/*` or import an assistant client module; the global assistant
  bubble in `MeshaShell` is allowed chrome, not a data path. Machine-gated by
  `make ceo-ai-boundary-guard` (backend SQL schema/table access, matched across
  newlines; plus FE/mobile `/api/ceo-ai` route + assistant-import coupling
  outside assistant-owned dirs); full rule in
  `docs/decisions/ceo-ai-reporting-boundary.md`.
- Do not let frontend/mobile read BigQuery, Sheets, Firestore, GCS, or operational databases directly.
- Do not spread vendor SDK calls through product code.
- Do not modify current live dashboard repos while building Goat OS copies.
- Do not add unbounded goroutines, full-table/full-herd API scans, direct media proxying through APIs, or dashboard raw BigQuery scans.
- Do not use direct gRPC for browser/React Native product clients without a new written ADR.
- Do not duplicate architecture decisions across random docs.
- Do not put individual staff/founder/vendor names into PRDs, TRDs, runbooks,
  prompts committed as docs, status files, or skill references when a role label
  is enough. The founder/builder visibility invariant above is the narrow
  exception because those exact accounts are provisioning seed truth.

Validation expectation:

- Run the narrowest relevant typecheck/build/test command for changed code.
- For DB query or migration changes on large tables, verify the indexed access
  path and add/update `make validate-sqlc-plans` coverage when the query is on a
  hot path or can touch import/goat/event/counter rows at scale.
- At phase closeout, compare code/contracts/migrations/tests against PRD/TRD and
  update context/skills/agent references if implementation changed the truth.
- For docs-only edits, run greps for stale terms when the user has explicitly banned wording.

## Standing Engineering Rules

- Deploy `goatos-stg` through the Slack button backed by Cloud Build and Cloud
  Deploy. Build systems may create images and Cloud Deploy releases, but Cloud
  Run service/job mutations for staging belong to
  `deploy/clouddeploy/stg/clouddeploy.yaml` and
  `tools/deploy/stg-clouddeploy-task.sh`. Direct `gcloud run services update`,
  `gcloud run jobs update`, or manual migration execution is break-glass only
  and must be followed by a Cloud Deploy release from the same commit; see
  `docs/runbooks/cloud-deploy-staging.md`.
- Never amend an already-applied Postgres migration or baseline to repair a
  shared environment. Ship the next numbered forward migration, because STG
  records migration checksums and will fail before pending repairs if an earlier
  applied file changed. Before declaring any migration-backed STG fix complete,
  verify `public.goatos_schema_migrations`, the live table/column/data contract,
  and `/readyz`; see `docs/runbooks/stg-deploy.md`.
- Use `.agents/skills/goatos-build/SKILL.md` as the active agent reference map.
- Use ports/adapters for replaceable vendors and tools.
- Use OpenAPI REST/JSON for web/mobile app APIs.
- Use JSON Schema for form DSL and event payload contracts.
- Use protobuf/gRPC only behind the app API boundary when a real internal workload needs it.
- Keep frontend/mobile data access behind generated clients and app APIs.

- Read wide, write narrow: agents may inspect the whole tree, but edits must stay within declared task scope.

- Keep committed project docs role-based rather than person-based. Use labels
  such as data owner, reviewer, operator, CEO/internal admin, or vendor instead
  of individual names unless a legal/contract artifact explicitly requires a
  named person.

## Mesha / Goat OS RFID Language

When a maintainer asks for "RFID", "tag", or "tag IDs" for animals in Goat OS,
return the actual animal tag columns from `goat_identifiers`:
`animal_identifier_1` and `animal_identifier_2`. Do not answer with
`goats.display_id` (`G-...`) unless the user explicitly asks for display IDs or
both display ID and RFID. At least one of the two animal identifier columns is
expected to be present for active goat records; treat a missing RFID answer as a
data-quality finding, not as permission to substitute display IDs.
