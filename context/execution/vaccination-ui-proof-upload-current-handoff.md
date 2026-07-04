# Vaccination UI + Proof Upload Current Handoff

Date: 2026-06-26

Purpose: hand off the current `/vaccination` UI closure work after the record /
verify drawer and execution table review. This is not an E2E or Google/prod
handoff. It is the remaining admin-web vaccination UI/control closure required
before browser E2E can be meaningful.

Canonical short prompt for the next closure session:

- `context/execution/vaccination-pending-closure-prompt.md`

## Active Session / Ownership Warning

As of the latest maintainer update on 2026-06-26 15:23 IST, Claude is actively
debugging the `/vaccination` render/click bug and owns the running `:3300` admin
web server plus the admin-web vaccination files. A new session must not edit the
same files concurrently. Start only after Claude stops or the maintainer
explicitly hands off ownership.

On start, refresh state instead of replaying this document blindly:

- `git status --short`
- `git diff --stat`
- active Claude/Codex processes
- `lsof -nP -iTCP:3300 -sTCP:LISTEN`
- latest Claude final output, if available

If Claude already fixed the render bug, verify and continue from the refreshed
state. Do not undo or overwrite Claude's in-flight edits.

## Start Here

Read first:

- `AGENTS.md`
- `apps/admin-web/AGENTS.md`
- `context/frontend/current-admin-web-scope.md`
- `context/frontend/herd-register-ui-fidelity-ledger.md`
- `context/execution/vaccination-pre-e2e-readiness-audit.md`
- `context/frontend/vaccination-process-integrity-frontend-handoff.md`
- `context/execution/sop-vaccination-backend-handoff.md`
- `docs/preventive-care-vaccination/PRD.md`
- `docs/preventive-care-vaccination/TRD.md`
- `docs/preventive-care-vaccination/V1-FOUNDATION-SPEC.md`
- `contracts/openapi/app-api.yaml`
- `mock/goatos-dashboard-mock.html`

Before editing, refresh `git status`, active sessions/processes, `:3300`,
`:8080`, and `:55432`. Do not edit concurrently owned admin-web files.

If committing this handoff or its follow-up changes, stage explicit paths only.
Do not use `git add -A` or `git add .`; this tree has many unrelated in-flight
changes and generated/local artifacts.

## Source Anchors Checked

Use these as the product/business boundary for the next session:

- Mesha wiki/source graph confirms Preventive Care (PC) vaccination requires cold-chain integrity
  through vaccine storage/transport (`Preventive Care Director handbook source`, p2). It also
  contains a diagnosis video-upload UI pattern, but that is not itself a
  vaccination admin-web camera requirement.
- `docs/preventive-care-vaccination/PRD.md` says field workers execute vaccination SOPs by
  scan -> administer -> record dose -> upload shed + vial video -> verify
  quantity, and verifier reviews proof submissions.
- `docs/preventive-care-vaccination/TRD.md` makes the shed drive an `obligation_batches`
  work unit, links it to one `sop_task`, and defines `vaccination_completions`
  with lot, dose, route/site, cold-chain, adverse reaction, and idempotency.
- `docs/preventive-care-vaccination/V1-FOUNDATION-SPEC.md` maps SOP proof to shed video,
  vial video, dose/qty/lot; no fake vaccine values or fake schedules should be
  invented to make the UI look dense.
- `contracts/openapi/app-api.yaml` already exposes proof upload
  (`createProofUpload`, returned `upload_url`, `completeProofUpload`),
  SOP submission (`submitAppTask`), Action Center rows with
  `obligation_id`/optional `batch_id`/`sop_task_id`/`sop_task_row_version`/
  `completion_id`, and SOP task verify/rework endpoints.
- `mock/goatos-dashboard-mock.html` is the admin-web UI/UX source of truth for
  layout, density, table controls, drawers, hover/active/disabled states, and
  action anatomy.

## Current Truth

The latest pass improved the mock-shaped right drawer, but it did not close the
functional vaccination UI work.

### P0 Blocker: Hidden App Tree / Dead Clicks

The current browser failure is more severe than a single dead link.

Claude restarted `:3300`, opened a clean tab, and still observed:

- fresh server + fresh tab still has shell/root content at `display:none`
- body contains multiple app/root trees
- one visible-looking route/loading skeleton is `.screen.card.pad` at
  `opacity:0`
- the real app tree with sidebar + matrix exists but is hidden
- links/cells measure as zero-size or detached, so real pointer clicks cannot
  reliably land

Treat this as a real shell/render/hydration/loading-state bug until fixed. Do
not accept programmatic click or route-state proof while the visible UI is being
painted from a hidden/detached tree. The next session must first identify and fix
why the admin-web shell/root is hidden after a clean restart.

Likely search areas before changing code:

- `apps/admin-web/components/mesha-shell.tsx`
- `apps/admin-web/app/mesha-theme.css`
- route/loading wrappers under `apps/admin-web/app`
- any recent change that introduced `.layout`, `.screen.card.pad`, `opacity:0`,
  `display:none`, or duplicated shell trees

Only after a clean tab shows one visible app tree and real pointer clicks land
should the session continue to table fidelity and proof upload.

Still pending after the P0 render bug is fixed:

1. Real user click proof
   - Do not rely on programmatic clicks or `history.pushState` instrumentation
     as proof.
   - Real pointer clicks on status-matrix cells, per-cohort rows, and
     shed/execution rows must open the correct right drawer reliably.

2. `/vaccination` execution table mock fidelity
   - The execution table must match the mock table system: toolbar, search,
     filters, row count, sort affordance, pagination/cursor footer where the
     backend caps rows, compact row density, aligned tags, clear hover/active
     states, and mock-consistent spacing/borders.
   - The current execution list still reads like a custom queue, not the mock
     table anatomy.
   - Do not confuse sample-data density with UI closure. Missing PPR/FMD/
     Enterotox/Deworm/CCPP columns, rich sample cohorts, and historical dose
     dates are roster/source-data gaps unless backed by the source-derived
     baseline or later Preventive Care (PC) / vet source config. Toolbar/filter/pager/density/action-label gaps are UI work and must
     be fixed.

3. Action labels and drawer context
   - A row whose next action is owner assignment must open owner-assignment
     controls or route to the owner-assignment surface.
   - A row whose next action is proof/record/verify must open the vaccination
     proof/record/verify drawer.
   - Do not show "Assign owner chain" while rendering a vaccine proof form unless
     owner assignment is truly the active action and the controls match it.

4. Web proof media is file upload only
   - Admin-web vaccination proof media is in scope as file upload.
   - Do not build browser camera capture for admin-web.
   - Native camera capture belongs to the future mobile/field app.
   - Web proof upload must use the existing proof upload flow where IDs exist:
     `createProofUpload` -> `PUT` bytes to the returned `upload_url` (local dev
     maps this to `uploadProofLocal`) -> `completeProofUpload`, followed by
     `submitAppTask` where a real `task_id` exists.
   - Use proof slots that match vaccination SOP/business meaning: shed/task
     context, vial/lot/cold-chain, and administration. Web label should say file
     upload, not camera capture.

5. Disabled controls are only acceptable for true rollups or proven missing IDs
   - Cohort/protocol rollup cells can keep disabled-with-reason controls if they
     truly have no single `completion_id`, `task_id`, or `obligation_id`.
   - Actionable rows must be wired, not punted.
   - If a shed/execution row lacks `task_id`, `obligation_id`, or
     `completion_id`, either enrich the read model or route to the exact
     Action Center / verification-queue row that has those IDs.
   - Do not leave a fake-looking enabled `Record + verify` button or dead
     `Upload / capture proof` visual box.
   - Prefer `ActionCenterObligation` as the actionable contract because it
     already carries `obligation_id` and can carry `batch_id`, `sop_task_id`, and
     `completion_id`. The `/vaccination/operations` matrix is a rollup contract;
     do not fake IDs from it.

6. Verification actions
   - Verification queue / Action Center rows with `completion_id` must also carry
     `sop_task_id` and `sop_task_row_version`.
   - Accept/reject/rework actions must call SOP task verify/rework routes with
     the task row version; direct vaccination completion review routes are not
     mounted.

## Implementation Order For New Session

1. Fix the hidden app tree / render-state P0 first. Stop if a clean tab still
   has multiple hidden app trees, `display:none` shell content, or detached
   zero-size links.
2. Re-prove real pointer clicks on matrix cells, cohort rows, and shed/execution
   rows. Capture screenshots; do not accept programmatic route changes as proof.
3. Make `/vaccination` execution table match the mock table anatomy: toolbar,
   search/filter control, row count, sort affordance, cursor/pager footer where
   applicable, compact density, aligned tags, and correct hover/active states.
4. Correct action routing:
   - owner-missing rows open owner-assignment controls or route to the matching
     Action Center row;
   - proof/record/verify rows open the vaccination proof/record/verify drawer;
   - completed/read-only rows open history/detail, not a fake write form.
5. Wire web proof file upload only where a real `sop_task_id`/proof scope exists.
   If `/vaccination/execution` lacks the needed IDs, route to or enrich from
   Action Center rather than inventing client-side IDs.
6. Wire accept/reject/rework only where a real `completion_id` exists. Rollups
   stay disabled-with-reason.
7. Update `context/frontend/herd-register-ui-fidelity-ledger.md` with the final
   state, screenshot paths, and any remaining intentional divergences.

## Do Not Do

- Do not seed fake protocols, vaccine names, cohorts, dates, totals, or proof
  rows to mimic the mock sample data.
- Do not build browser camera capture for admin-web.
- Do not introduce nested command routes like `/vaccination/action-center`,
  `/vaccination/protocol-adherence`, or `/vaccination/config`.
- Do not mutate Cloud SQL, Google/prod resources, or source-backed Preventive Care (PC) / vet
  vaccine schedules in this UI session.
- Do not call browser/Playwright E2E closed from screenshots or mock-fidelity
  checks; E2E remains a later gate.

## Completion Bar

This slice is complete only when:

- Every visible `/vaccination` execution-table control works, navigates, holds
  honest local UI state, or is visibly disabled with a specific reason.
- Web proof upload is a real file input/upload flow where the row has a real
  `task_id`/proof scope.
- Record/verify actions are backend-backed where `completion_id` exists.
- Rollup-only disabled states are clearly limited to rollups and documented as
  such.
- The execution table visually matches `mock/goatos-dashboard-mock.html`
  element-by-element.
- Real pointer-click screenshots show the table, clicked row/cell, drawer, file
  upload state, and verify/action state.

## Required Gates

Run, or document why impossible:

```bash
npm --prefix apps/admin-web run typecheck
npm --prefix apps/admin-web run lint
npm --prefix apps/admin-web run check:mock-fidelity
```

If backend/read-model contracts change, also run the relevant Go tests and
regenerate generated clients through the repo's normal command.

Browser/Playwright E2E and Google/prod provisioning remain deferred. Do not
claim those are closed from this handoff.
