# Vaccination Pending Closure Prompt

Date: 2026-06-26

Purpose: store the full execution contract for closing the remaining
non-Browser-E2E, non-Google/prod vaccination work. Future sessions should not
need a huge pasted prompt; use the short prompt below.

## Short Prompt To Paste

```text
Work in /Users/ravi/mesha/goatos. Read and execute:
context/execution/vaccination-pending-closure-prompt.md

Goal: close all remaining non-Browser-E2E, non-Google/prod pending items for the
vaccination slice, then commit/push safely if verification passes or checkpoint
the clean closed subset if a documented blocker remains.

Start with ownership/process/git refresh. Do not use git add -A or git add .
Defer only Browser/Playwright E2E/full click-matrix approval and Google/prod
provisioning. End final report with:
Browser/Playwright E2E not run.
Google/prod provisioning not touched.
```

## Non-Negotiables

Defer only:

- Browser/Playwright E2E and full click-matrix approval.
- Google/prod provisioning.

Allowed:

- Manual Chrome real-pointer checks.
- Local visual smoke only.
- Existing `npm --prefix apps/admin-web run smoke:visual:live` as smoke, not
  E2E.

Banned:

- Browser/Playwright E2E suites.
- Full click-matrix approval runs.
- Google/cloud/prod/stg mutation.
- Invented production vaccine schedule values, fake production protocols, fake
  cohorts, fake dates, fake totals, or fake proof rows.
- `git add -A`.
- `git add .`.

Use explicit `git add <path>` only. Push with:

```bash
zsh -ic 'git mesha-push main'
```

Compaction guard: after any compaction/resume, reconstruct current state before
replaying deletes, commits, pushes, migrations, seeds, imports, server restarts,
or DB/cloud actions.

Local server authority: local dev services may be restarted only after ownership
check. Restore them on the same ports: `:3300` admin-web, `:8080` API,
`:55432` DB.

## Phase 0 - Refresh And Ownership

Confirm no active competing Claude/Codex session owns this repo, admin-web,
`/vaccination` files, or `:3300`/`:8080`/`:55432`. If another active session
owns them, stop and report.

Run:

- `git status --short`
- `git diff --stat`
- `git diff --name-status`
- `git ls-files --others --exclude-standard`
- `ps`/`lsof` for `:3300`/`:8080`/`:55432`

Read:

- `AGENTS.md`
- `SKILLS.md`
- `context/README.md`
- `apps/admin-web/AGENTS.md`
- `context/frontend/current-admin-web-scope.md`
- `context/execution/vaccination-ui-proof-upload-current-handoff.md`
- `context/execution/vaccination-pre-e2e-readiness-audit.md`
- `context/execution/vaccination-trigger-closure-parallel-handoff.md`
- `context/frontend/vaccination-process-integrity-frontend-handoff.md`
- `context/execution/sop-vaccination-backend-handoff.md`
- `docs/phc-vaccination/PRD.md`
- `docs/phc-vaccination/TRD.md`
- `docs/phc-vaccination/V1-FOUNDATION-SPEC.md`
- `docs/runbooks/vaccination-local-business-chain.md`
- `context/frontend/herd-register-ui-fidelity-ledger.md`
- `contracts/openapi/app-api.yaml`
- `contracts/openapi/admin-api.yaml`
- `mock/goatos-dashboard-mock.html`

Use code-review-graph for dirty-tree review if available. Use Graphify before
business/UX decisions:

- `mesha_docs_graph` for PHC/vaccination/SOP/cold-chain/source facts.
- `mesha_visual_graph` for screenshots/diagrams/UI flow.
- goatos docs graph for committed GoatOS docs.

## Phase 1 - Close Current `/vaccination` UI Handoff

Close or verify everything in
`context/execution/vaccination-ui-proof-upload-current-handoff.md`.

Required outcomes:

- Hidden app-tree/click blocker fixed or verified already fixed.
- Clean Chrome tab has one visible app tree.
- Manual real pointer clicks open the right drawer for status-matrix cells,
  per-cohort rows, and shed/execution rows.
- Execution table matches mock anatomy: toolbar, search, filters, row count,
  sort affordance, cursor/pager footer where capped, compact density, aligned
  tags, hover/active states.
- Matrix and per-cohort tables match mock controls/fidelity.
- Action labels match vaccination workflow and wiki/docs.
- Owner-missing rows open/route to owner-assignment flow.
- Proof/record/verify rows open proof/record/verify drawer.
- Completed/read-only rows open history/detail, not fake write forms.
- Admin-web proof media is file upload only, not camera.
- Proof upload uses `createProofUpload -> PUT upload_url ->
  completeProofUpload`.
- `submitAppTask` only runs where real `task_id` exists.
- Accept/reject only runs where real `completion_id` exists.
- Rollup cells stay disabled only with exact reason.
- Append final state and screenshot paths to
  `context/frontend/herd-register-ui-fidelity-ledger.md`; do not overwrite prior
  screenshot history.

UI/UX fidelity requirement: match `mock/goatos-dashboard-mock.html`
element-by-element for every in-scope `/vaccination` surface touched in this
pass:

- Fonts, type scale, font weight, line-height.
- Colors, tag colors, borders, backgrounds.
- Hover, active, disabled, empty, error, and loading states.
- Spacing, padding, row height, table density.
- Column/cell alignment, toolbar layout, search controls, filters, row counts,
  sort affordance, pagination/cursor footer.
- Drawer anatomy: header, body, footer buttons, field layout, proof upload
  control, labels.
- Button/icon/chip sizing.
- Mobile/narrow behavior for touched responsive components.

Do not treat typecheck/lint/check:mock-fidelity as visual proof. Inspect
screenshots and compare against the mock manually, element by element.

## Phase 1A - Protocol Columns And Local Fixture Data

The vaccination matrix columns are not just visual; they come from backend/config
protocol rows. The UI must not hardcode PPR/FMD/Enterotox/Deworm/CCPP columns.

Do not stop just because the current local API returns only one protocol.
Instead:

- Use the source-derived local/dev baseline recorded in
  `context/source-findings/phc-vaccination-roster-stage-proposal.md`: ET/K1/day
  21 as the schedule-bearing row, K2=42, and PPR/FMD/HS/BQ as SOP/roster labels
  until schedule evidence exists.
- Do not create cosmetic UI-only vaccine columns; protocol/config rows must drive
  whatever the matrix shows.
- Apply only to the local dev DB at `:55432`.
- Do not touch Cloud SQL or prod/stg.
- Do not re-open a generic "business approval" blocker for the local/dev ET
  baseline; later production roster expansion is source-backed versioning.
- The `/vaccination` matrix should show multiple vaccine columns because
  backend/config returns multiple protocols, not because the UI fakes columns.

## Phase 1B - Actionable Backend Contract

Close the actionable row contract gap:

- Expose `obligation_id`, `batch_id`, `sop_task_id`/`task_id`, and
  `completion_id` where they exist in `/vaccination/execution` or the drawer
  source.
- Update OpenAPI, backend read model, and generated admin/app clients as needed.
- Wire web proof file upload only where real `task_id` exists:
  `createProofUpload -> PUT upload_url -> completeProofUpload`.
- Submit SOP task only with real `task_id`.
- Accept/reject only with real `completion_id`.
- Keep rollups disabled only when no single real ID exists, with exact reason.

Pager wording must be honest. If the API is limit-bounded, do not say
`all in-scope shown`. Use `N returned rows - no cursor exposed` or implement real
cursor/`has_more`.

## Phase 2 - Clean And Stabilize Dirty Tree

Categorize dirty files:

1. Intended vaccination closure code/docs.
2. Required generated client/sqlc/schema artifacts.
3. Local runtime junk.
4. Suspicious/unrelated files needing review.

Remove only obvious runtime junk:

- `rm -rf backend/.goatos-local-media/` if it contains only local proof media.
- Temp logs/cache/screenshots only if untracked and not referenced by docs.

Do not delete scripts/runbooks/migrations/generated clients/source docs.

Review and preserve if intentional:

- `tools/dev/vaccination-chain-proof.sh`
- `docs/runbooks/vaccination-local-business-chain.md`
- `backend/migrations/postgres/000085_*` if required by HF evidence.
- `context/execution/vaccination-ui-proof-upload-current-handoff.md`
- `context/execution/vaccination-post-audit-codex-handoff.md`
- `context/frontend/herd-register-ui-fidelity-ledger.md`

Review `backend/internal/legacy_import/` and `backend/internal/reporting/`;
include only if intentional and verified, otherwise stop and ask.

Confirm deleted admin-web UI primitives have no imports remaining.

Fallback checkpoint rule: if later phases stall, break verification, or expose a
blocker, still commit and push the clean Phase 1/2 checkpoint plus any safely
closed work. Commit body must reflect actual closed state only; mark un-run
phases as deferred/blocker, not closed.

## Phase 3 - Close Four-Goat Negative Matrix

Add or extend a data-plane script/test. No browser. No manual DB patching.

Use real existing local DB data only. Prefer existing seeded goats/loads from
local `:55432`. If local DB lacks real rows for required states, document that
as a data blocker. Do not fabricate goats or manually patch business rows.

Matrix assertions:

- Clean accepted goat enters vaccination work.
- Rejected/source-only goat does not enter active PHC vaccination work.
- Owner-missing/unresolved goat is excluded or shown only as blocker/review.
- Extra unknown arrival is excluded or review-only.

Use API/CLI/Go harness/SQL assertions. Record concrete IDs and assertions in:

- `docs/runbooks/vaccination-local-business-chain.md`
- `context/execution/vaccination-pre-e2e-readiness-audit.md`

This must become closed or have an exact blocker.

## Phase 4 - Close PHC/Vet Roster Item If Possible

Use Graphify/wiki/source docs first. Cite `source_file`/page/image.

Use the existing source-derived ET/K1/day-21 baseline and do not over-engineer an
importer to force broader production roster expansion.

Boundaries:

- Local dev only: any seed/migration/config verification targets local docker DB
  at `127.0.0.1:55432`, never Cloud SQL.
- If goose migration is needed, apply only via documented local psql/goose path.
- Use only an existing approved source-backed import/config pathway.
- If no existing pathway exists, document that as a feature blocker.
- Do not build a new ingestion/import surface in this session.
- Do not invent production PPR/Enterotox/CCPP schedules.

Use the source-derived ET/K1/day-21 baseline now:

- Cite source evidence.
- Add/import/seed only through existing approved local source-backed path.
- Preserve source metadata: `source_system`, `source_ref`, `review_status`,
  `approved_by`, `approved_at`.
- Verify Config shows them and publish gate remains honest.

For later PPR/FMD/HS/BQ schedule expansion:

- Keep source labels visible only where backend/config supplies them.
- Add schedule-bearing rows only when timing/dose/booster source evidence exists.
- Leave no vague pending or generic business-approval language.

## Phase 5 - Verification

Run:

- `git diff --check`
- `cd backend && go test ./...`
- `cd backend && go build ./...`
- `cd backend && go vet ./...`
- `npm --prefix packages/api-client run generate` if OpenAPI changed.
- `npm --prefix apps/admin-web run check:mock-fidelity`
- `npm --prefix apps/admin-web run typecheck`
- `npm --prefix apps/admin-web run lint`
- `npm --prefix apps/admin-web run build`

Run visual smoke, but do not call it E2E:

- Ensure `:3300`/`:8080`/`:55432` are healthy.
- `npm --prefix apps/admin-web run smoke:visual:live`
- Inspect screenshots.
- Compare in-scope surfaces to `mock/goatos-dashboard-mock.html`
  element-by-element.
- Append ledger updates if needed.
- Restore servers on same ports if restarted.

Run proof scripts:

- `bash tools/dev/vaccination-chain-proof.sh`
- Run the new/extended four-goat negative-matrix proof if Phase 3 closed.

If procurement idempotency code changed, also run the relevant race test from
the backend module root.

## Phase 6 - Commit And Push

Review:

- `git status --short`
- `git diff --stat`
- `git diff --cached --stat`

Stage explicit reviewed paths only. Never `git add -A`. Never `git add .`.

Commit message:

```text
checkpoint: vaccination pre-E2E readiness
```

Commit body must state actual state per phase:

- `/vaccination` UI handoff closed or exact blocker.
- Data-plane chain closed.
- Four-goat negative matrix closed, not run, or exact blocker.
- Source-derived ET/K1/day-21 baseline and K2=42 recorded, with any later
  production roster expansion called out separately.
- Browser/Playwright E2E not run.
- Google/prod provisioning not touched.

Push:

```bash
zsh -ic 'git mesha-push main'
```

Final report:

- What was cleaned.
- What non-E2E pending items were closed.
- `/vaccination` UI result.
- Four-goat matrix result.
- Source-derived roster/stage result with sources checked/cited.
- Verification results.
- Commit hash.
- Final git status.
- Fresh-session handoff prompt for Browser/Playwright E2E and full
  click-matrix approval.
- Fresh-session handoff prompt for Google/prod provisioning only when explicitly
  requested.

End exactly with:

```text
Browser/Playwright E2E not run.
Google/prod provisioning not touched.
```
