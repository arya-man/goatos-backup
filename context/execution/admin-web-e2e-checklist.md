# Admin-Web Full E2E Checklist

Date: 2026-06-27

Purpose: turn the requested "test every real thing" pass into a runnable,
non-destructive E2E gate for the current Goat OS admin-web slice.

This checklist extends:

```text
context/execution/vaccination-pre-e2e-readiness-audit.md
context/execution/procurement-vaccination-e2e-plan.md
docs/runbooks/vaccination-local-business-chain.md
```

## Counterpoints

The intent is right: one clean local stack, real seed data, real APIs, real
frontend clicks, stop on failure, collect defects, rerun only failed checks, and
prove CT / AC / PA / Calendar / Workflows read the same truth.

Do not turn that into "click every future product route" or broad CRUD before
the active slice needs it. Current approved admin-web scope is PHC Vaccination,
Calendar vaccination due-work, Procurement Source Entry bridge, Counts Herd
Register, Config, SOP Library, Audit Log, Goat Passport, and top-level command
lenses. Unbuilt verticals and future Counts modules must stay absent, not
visible as fake disabled placeholders.

Also, do not test disabled or future controls as if they are built. For
disabled/unbuilt affordances, the E2E suite may assert only that they are absent
from the active path or visibly non-mutating with an honest reason. It must not
click through future write flows, seed data for them, or fail the slice because a
future flow has no backend yet.

Do not block today's vaccination E2E on fully backend-driven page bodies. The
admin-web shell now consumes backend-owned nav, top-bar, route-label, role-lens,
and page metadata from `GET /admin-web/bootstrap`; parks and nav badges are also
backend-backed. Route body titles, table headers, filters, chips, drawer labels,
and action copy are still the migration gap below. Until those page bodies are
migrated, E2E must assert that visible controls are real, local-chrome only,
navigational, contract-backed, or honestly disabled.

## Current Bug Ledger

Fixed in this pass:

- Vaccination execution rendered the returned shed-event set as one long page.
  It now pages filtered execution rows before grouping by park/shed.
- Vaccination status matrix and cohort detail used a disabled "no cursor"
  footer. They now expose real 5/10/25/50 frontend pagination over the bounded
  backend response.
- Shared visible-row search/filter only knew table rows. It now also covers
  vaccination execution rows, Workflow catalog rows, and Action Center task
  cards.
- Supplier warmup context was hard-limited to four procurement loads. It now
  fetches a larger bounded set and pages the visible loads.
- Protocol Adherence had no search/pager on the vaccination gap ledger. It now
  has visible-row search, filter drawer, and mock-style pager.
- Workflows catalog had no search/pager and could force a very tall left rail.
  It now has visible-row search/filter plus the same pager controls.
- Action Center board and SOP verification queue rendered all fetched rows.
  Both now have search/filter affordances and mock-style pagers.
- Config protocol rules and SOP Library are backend-sourced active admin lists,
  but had no pager. Both now have local search plus 5/10/25/50 pagination.

Still a documented platform/API gap, not hidden as done:

- Some shell navigation, page titles, and page chrome remain frontend-owned.
  Full backend-driven IA/page contracts are tracked in the Contract-Driven UI
  Platform Gap section below.
- Several list endpoints still return a bounded first page without backend
  cursor metadata. The UI now prevents giant dumps, but true server-side
  pagination/search/sort remains a backend contract follow-up per endpoint.

## Clean Slate Rule

Clean slate means deterministic business data, not deleting auth/session state.

Allowed:

- Start local Postgres/API/admin-web from current source.
- Apply migrations to head.
- Seed tenant, grants, locations, sheds, stage lookup, protocol config, SOP,
  inventory, vaccine lot, procurement load, and goats with deterministic E2E
  prefixes.
- Remove or archive only rows created by the E2E run when a scoped cleanup tool
  exists.

Not allowed:

- Delete browser login/session/cookies unless the login flow itself is under
  test.
- Drop the whole local database as a hidden precondition.
- Hand-edit frontend fixtures to make UI rows appear.
- Directly mutate business state from the browser outside generated APIs.

Each run must print a run id, seed ids, and a cleanup scope. Suggested run id:

```text
E2E-YYYYMMDD-HHMMSS
```

## Local Stack Gate

Before browser E2E, prove the local production-equivalent chain:

```text
Postgres migrated to head
  -> API :8080 from current source
  -> admin-web :3300 from current source
  -> outbox relay with GOATOS_OUTBOX_PUBLISHER=eventbus
  -> vaccination generation handler registered
  -> obligation sweeper runnable
  -> proof/SOP/verification APIs usable
  -> CT / AC / PA / Calendar / Workflows / Vaccination / Passport read Postgres truth
```

Use:

```bash
bash tools/dev/vaccination-chain-proof.sh
```

This proves the clean Herd Register path. The full gate is not green until the
negative procurement matrix below is captured in the same style.

## Time Windows

Use seconds, not vague waiting.

| Transition | Poll interval | Timeout | Pass condition |
| --- | ---: | ---: | --- |
| `goat.created` outbox row appears | 1s | 10s | one pending row for the goat |
| relay delivers to eventbus | 1s | 15s | outbox published and obligation exists |
| sweeper creates batch/SOP task | 1s | 15s | obligation has `batch_id`, batch has `sop_task_id` |
| proof upload completes | 1s | 10s | proof status is completed |
| SOP submission fanout records completion | 1s | 15s | `vaccination_completions` row exists |
| verification action updates obligation | 1s | 10s | completion accepted/rejected/rework and obligation state updated |
| CT / AC / PA / Calendar / Workflows refresh | 2s | 30s | every read model reflects the same terminal state |

On timeout, stop the suite, collect API response, DB row ids/statuses, server
logs, browser screenshot, and the failing route URL. Do not continue and bury
the root cause.

## Seed Matrix

### Locations And Sheds

Seed at least one park and two sheds:

```text
park: CBE
shed A: CBE-K1-A
shed B: CBE-K1-B
```

The UI must prove filtering, shed drilldown, and read-model grouping across both
sheds. Do not create generic shed CRUD only for E2E; use seed/config until a
real operator workflow requires CRUD.

### Herd Register Goats

Create through `/counts/herd` or `POST /admin/goats`:

| Goat | Shed | Shape | Expected result |
| --- | --- | --- | --- |
| `E2E-HERD-CLEAN-A` | A | K1 day-21, eligible, no trusted prior evidence | vaccination obligation generated |
| `E2E-HERD-CLEAN-B` | B | K1 day-21, eligible | separate shed grouping generated |
| `E2E-HERD-HF-TRUSTED` | A | matching trusted HF vaccination evidence | matching obligation suppressed |
| `E2E-HERD-NOT-DUE` | B | outside rule window | no active due work |

### Procurement Load Goats

Create through `/procurement/source-entry` APIs/UI:

| Goat | Procurement path | Expected downstream |
| --- | --- | --- |
| `E2E-PROC-CLEAN` | source health pass -> pre-dispatch accepted -> loaded -> arrived -> accepted intake | enters PHC vaccination work |
| `E2E-PROC-REJECT` | rejected before truck | procurement history only, never PHC work |
| `E2E-PROC-OWNER-MISSING` | owner/ownership unresolved | procurement CT/AC gap only, never PHC work |
| `E2E-PROC-EXTRA-UNKNOWN` | extra unknown at arrival | arrival/identity review only, never PHC work |
| `E2E-PROC-DEFERRED` | accepted intake with health defer signal | defer visible; no active dose obligation until eligible |

## Backend/API Cases

Run API/integration checks before browser clicks.

1. Config publish:
   - Save vaccination protocol draft.
   - Publish only when source/review, SOP, proof, stage lookup, and role gates
     pass.
   - Assert missing gates block in deterministic priority.

2. Herd Register trigger:
   - Create eligible goats split across two sheds.
   - Relay `goat.created`.
   - Sweep obligations.
   - Assert AC / PA / Calendar / Workflows / Vaccination / shed drilldowns group
     by shed and status.

3. Herd Register import:
   - Use the same template shape the UI exposes.
   - Preview mixed valid/invalid rows.
   - Commit valid rows.
   - Assert row errors, idempotent replay, audit rows, and generated
     `goat.created` events.

4. Procurement source-entry:
   - Create load.
   - Add source goats.
   - Record source health.
   - Record pre-dispatch decisions.
   - Dispatch only eligible goats.
   - Review arrival with expected, missing, extra, and rejected cases.
   - Accept intake for the clean goat only.
   - Assert only accepted-intake clean goats generate vaccination obligations.

5. SOP/proof/verification:
   - Upload required proofs through backend proof APIs.
   - Submit SOP answers with proof refs.
   - Accept one completion.
   - Reject one completion.
   - Request rework for one completion.
   - Assert AC / PA / Calendar / Workflows / Passport show the right state after
     each action.

6. Idempotency and replay:
   - Replay create/import/accept-intake/proof/SOP/verification with same key and
     same payload.
   - Assert original result returns with no duplicate side effects.
   - Replay same key with different payload.
   - Assert deterministic conflict behavior.
   - Re-run relay and sweeper.
   - Assert obligations/completions remain de-duplicated.

## Frontend Click Matrix

Every visible control must be one of:

```text
real generated backend API
real server action using generated API
real route/navigation
local UI chrome only
disabled with visible honest reason
```

The active click suite must exercise only built controls. Disabled/future
controls are not action-test targets; they are scope-honesty checks only.

### List/Table Pagination Flow

For every built list/table/card-list surface, assert:

- First page renders no more than the selected row/page size.
- Page-size controls cover 5, 10, 25, and 50 where the local contract is used.
- Next, Previous, disabled-first, and disabled-last states work.
- Filter/search resets or clamps to a valid page and never leaves a blank page
  while matching rows exist.
- Row/card click from page 2 opens the correct drawer/detail and close returns
  to the same page/filter/scope.
- Empty search state is clear and does not mutate data.
- Top-bar park/date scope survives every pager/filter/search navigation.
- Browser smoke checks desktop and narrow widths for footer overlap, clipped
  labels, and inaccessible scroll regions.

Current active surfaces:

- Counts Herd Register.
- Vaccination status matrix.
- Vaccination cohort detail.
- Vaccination supplier warmup context.
- Vaccination execution shed-event board.
- Action Center work board.
- Action Center SOP verification queue.
- Protocol Adherence vaccination ledger.
- Workflows live workflow catalog.
- Procurement Source Entry board.
- Config protocol rules.
- SOP Library cards.
- Audit Log top and bottom pagination.

### Text Length / Hover Full Text

Every built page must handle long backend strings without pushing the layout,
hiding later columns, or forcing users to guess the value.

- Long table/list/card text must use ellipsis (`...`) or a two-line clamp.
- The full value must be available on hover/focus through a native `title`,
  accessible label, or equivalent tooltip.
- Truncated elements should use the shared `ClipText` / `data-truncate`
  contract where practical.
- E2E smoke must fail if a marked truncated element has no full hover text or
  malformed truncation styling.
- Protocol Adherence specifically must truncate long `Expected`, `Actual`,
  `Owner`, and `Next action` cells while keeping the full text on hover.
- The same rule applies to Action Center task cards, Workflow rows, Vaccination
  execution rows, Procurement rows, Config rules, SOP cards, Audit rows, and
  Goat Passport identifier/history rows as those surfaces grow.

### Shell And Top Bar

- Desktop hamburger collapse/expand.
- Mobile hamburger open/close, scrim click, Escape close.
- Sidebar group open/close, active leaf, badges, disabled leaf state.
- Control Tower, Action Center, Calendar, Protocol Adherence, Workflows.
- PHC / Vaccination.
- Procurement / Source Entry.
- Counts / Herd Register.
- Admin / Data Ops / Config, Audit Log, SOP Library.
- Park scope menu: all parks, each seeded park, backend-safe ids in URL.
- Date/as-of menu: point-in-time active, unsupported ranges disabled.
- Theme toggle.
- Notifications disabled with reason.
- Role preview menu switches local preview state only.
- Sign out stays available; do not delete login/session state in clean-slate
  setup.

### Control Tower

- Summary reads `/control-tower/vaccination`.
- Shows process gaps only, not raw herd census as a KPI.
- Links to AC, PA, Workflows, Vaccination, Config, SOP Library.
- Completed clean-goat obligations should disappear from gap alerts.
- Rejected/unresolved procurement goats must not appear as vaccination gaps.

### Action Center

- Domain/work-state/severity chips.
- Search/filter controls: server-backed where contract exists; otherwise local
  drawer controls must be labeled local/visible-page only.
- Board pager: page size, next, previous, search, filter reset, row click,
  drawer close, and scope preservation.
- SOP queue pager: page size, next, previous, search, verify/reject/rework, and
  return URL preservation.
- Cards open workflow/passport/shed links.
- Verify, reject, request rework actions.
- Pagination/empty/error/loading/retry states.

### Calendar

- Day/week/month/list tab switches.
- Due events show vaccination work from generated obligations.
- Nudge/snooze/done only when backend action exists; otherwise disabled.
- Event drawer links to workflow, goat/passport where available, shed context,
  and Action Center.
- After verification accept/reject/rework, poll for up to 30s and assert the
  event state changes or disappears according to the read model.

### Protocol Adherence

- Expected/actual/gap/severity/owner/next-action/evidence table.
- Severity chips and filters preserve top-bar scope.
- Search and pager cover visible rows, empty search, page-size switch, and
  drawer close preserving scope/page.
- Row links open workflow or evidence detail.
- Deferred/explained rows remain visible.
- Server pagination/sort/search when available; otherwise disabled with reason.

### Workflows

- Live workflow catalog search/filter/pagination.
- List rows navigate to `/workflows/{row_id}`.
- Drilldown shows config -> obligation -> batch/drive -> SOP -> proof ->
  verification -> completion -> next due.
- Back links, Passport links, shed execution links, AC and PA links.
- Completed/rejected/rework permutations render the correct chain node state.

### WF / PA / SOP State Reaction

This is mandatory for the vaccination slice, not optional smoke:

- Publish source-backed vaccination config:
  - Workflows shows `Config` done and `Obligation` pending/current until goat
    eligibility materializes.
  - Protocol Adherence does not show a false gap before any eligible goat
    exists.
- Create eligible herd goat:
  - Outbox relay and sweeper generate obligation and shed-drive SOP task within
    the timeouts above.
  - Workflows changes to Config done, Obligation done, Drive current/next.
  - Protocol Adherence shows expected vaccination, actual missing, owner/next
    action, and severity.
  - Action Center shows the corresponding card under the computed work state.
  - Calendar shows the due event in the selected date window.
- Submit SOP/proof:
  - Workflows advances SOP/proof nodes.
  - Protocol Adherence evidence count/status changes.
  - Action Center moves from proof-pending to verification-pending.
  - Calendar event state changes from due/proof to verification/review where
    the read model exposes it.
- Verify accepted:
  - Workflows advances Verify and Close/completion.
  - Protocol Adherence gap closes or changes to on-track/completed.
  - Action Center removes the open gap/card or moves it to completed.
  - Calendar due event disappears or becomes completed according to the read
    model.
  - Goat Passport shows the completed dose.
- Reject/request rework:
  - Workflows marks Verify blocked/rework.
  - Protocol Adherence keeps the row visible with rejection/rework evidence.
  - Action Center shows rejected/rework state and next action.
  - Calendar shows rework/review due work if the backend read model emits it.

### Vaccination SW Flow Contract

The expected product flow is:

```text
Config -> Due List -> Shed Drive -> SOP Execution -> Proof -> Verification -> Completion
```

E2E must prove the core chain:

- Config means an approved, source-backed vaccination rule with vaccine, goat
  group/stage/age, due/booster schedule, linked SOP, proof policy, and approver.
- Vaccination does not create rules directly; Admin / Data Ops / Config owns
  rule authoring and publish.
- Published config plus goat eligibility creates due vaccination work.
- New goat creation automatically checks vaccination eligibility.
- Due goats are grouped shed-wise into a shed drive, not one task per goat.
- Shed owner/operator executes the SOP, uploads proof, verifier accepts/rejects,
  completion writes vaccination history, and booster date is created only after
  accepted verification.
- Calendar, Action Center, Protocol Adherence, Workflows, Vaccination execution,
  Goat Passport, and alerts/readiness all read from the same backend truth.

Do not claim these edge cases are green until the suite has explicit backend and
frontend assertions:

- Draft/not-approved rule creates no work.
- Trusted holding-farm/history evidence suppresses duplicate work.
- Sick/ICU/quarantine/deferred goats remain visible with reason.
- Shed shift moves open pending vaccination work to the new shed.
- Dead/sold/exited goats cancel pending work without changing completed history.
- Overdue/missed, missing proof, rejected proof, and rework remain visible.
- Missing/expired stock is shown as blocked when the runtime enforces it.
- Rule changes preserve old completed work and new work follows the new approved
  rule.
- Reminder/nudge/escalation state is visible and external delivery is verified
  once notification workers are in scope.

Current code audit status: the core chain is real and covered by the smoke
proof; some edge cases are partial/operationally gated. See
`context/execution/vaccination-edge-case-code-coverage.md` before making
CEO-facing claims about shift/exited events, hard stock blocking, stage-change
triggers, manual campaigns, or automatic escalation ladders.

### Vaccination

- SOP quick-view opens real SOP data.
- Import sheet / New drive are not active E2E targets until real write contracts
  exist; current vaccination drives are generated from published protocol config.
- Status matrix search/filter/pagination, including page-size switches and
  second-page row click.
- Per-cohort row clicks and record/verify drawer read real state.
- Execution section search/filters/pagination and shed row clicks.
- Shed detail shows owner chain, blockers, deferred reasons, SOP/proof/
  verification state, and next action.

### Procurement Source Entry

- Board list search/filter/pagination.
- New load.
- Load detail.
- Add source goat.
- Source health.
- Pre-dispatch accept/reject/defer/block.
- Dispatch.
- Arrival gate expected/missing/extra review.
- Accept intake.
- Media capture disabled with honest reason until included in this slice.
- Procurement command-lens checks use top-level routes with
  `?domain=procurement`, never nested procurement command routes.

### Counts Herd Register

- Search rows.
- Filters modal: apply, clear all, close, Escape/backdrop.
- Page size.
- Cursor next/previous.
- First page, last page/end state, empty result, invalid cursor, invalid sort.
- Register goat drawer: required fields, validation, submit, success, error,
  duplicate identifier conflict, double-submit protection.
- Import sheet drawer: template, paste/upload, preview, row errors, commit,
  replay, success, error.
- Goat Passport row/entity links.

### Config

- Category switch changes schema-driven fields.
- Stage lookup comes from backend; read failure blocks save/publish.
- SOP list comes from backend; read failure blocks save/publish.
- Protocol-rules search/pagination covers source-backed returned rows.
- Save draft.
- Dirty-after-save blocks publish.
- Publish.
- Impact preview.
- Source/review gate messaging.

### SOP Library

- List real vaccination SOPs.
- Search/filter/page real returned SOPs.
- New SOP builder.
- Save draft, dry run, publish.
- Proof policy subject scope.
- Non-vaccination domains hidden or disabled.

### Audit Log

- Summary cards.
- Operation family chips.
- Role/`Viewing as` lens.
- Search.
- Operator filter.
- Top and bottom pagination.
- Row detail drawer.
- Clear all.
- Export disabled until real export exists.

## Contract-Driven UI Platform Gap

Target rule for future expansion:

- Shell navigation, top-level page chrome, page titles, table columns,
  supported filters, sort keys, page sizes, row actions, badges, disabled
  reasons, empty-state copy, and route visibility should come from a
  tenant/RBAC-aware backend contract.
- Frontend may own rendering, icons by token, responsive layout, open/closed UI
  state, unsaved form draft state, theme preview, and local menu state.
- Frontend must not own business truth, built/unbuilt module truth, route
  entitlement, server sort/filter semantics, or write-action availability.

Current status:

- `MeshaShell` now consumes backend-owned shell/nav/top-bar/route-label data
  from `GET /admin-web/bootstrap`.
- Parks in the top bar are backend-backed.
- Action Center / PHC badges are fetched through `/api/nav-counts`.
- Role preview lenses are now backend contract text, but still preview-only UI
  state; they do not bypass RBAC.
- Calendar page presentation is partly frontend-local contract code.
- Data tables and workflow rows generally use generated backend clients, but
  not every visible filter/action has a backend contract yet.

Do not claim "fully backend-driven UI" until the existing
`GET /admin-web/bootstrap` page contracts are consumed by route bodies, not only
the shell. Recommended build path:

1. Keep `GET /admin-web/bootstrap` generated and RBAC-protected. It returns
   navigation, scope controls, role lenses, route labels, page/table/drawer
   contracts, and disabled reasons.
2. Consume per-page contracts:
   `page_title`, `subtitle`, `tabs`, `filters`, `sort_keys`, `columns`,
   `page_size_options`, `row_actions`, `bulk_actions`, `empty_state`, and
   `error_contract`.
3. Convert the shell to render from contract while preserving mock anatomy.
4. Add contract drift tests and Playwright assertions that every rendered
   server-declared action is either executable or disabled with the declared
   reason.

Until then, the E2E pass validates honest wiring and data truth, not dynamic IA.

## Parallel Execution Policy

On the current local laptop shape observed on 2026-06-27:

```text
logical CPUs: 12
memory: 24 GB
```

Recommended local limits:

- One Postgres/API/admin-web stack.
- One data-plane mutating suite at a time.
- Playwright browser workers: 2 for click/read suites; 1 for mutation suites.
- Backend Go package tests may run in normal package parallelism, but avoid
  running them while Next is building.
- Multiple agents may split work by domain, but they must not all start their
  own full local stacks or mutate the same seed tenant simultaneously.

Safe split:

```text
agent A: backend/API seed and assertions
agent B: frontend click ledger/read-only Playwright
agent C: docs/contract gap ledger
```

Unsafe split:

```text
three agents each running make dev-local + next build + browser E2E + DB mutation
```

If multiple agents run, give each one a unique run id and seed prefix. Mutating
tests must use separate tenants or strict idempotency keys.

## Failure And Rerun Policy

1. Stop on first hard failure in a mutating chain.
2. Capture:
   - run id
   - route/API call
   - idempotency key
   - request payload fingerprint
   - response body/status
   - DB ids/statuses for affected rows
   - backend logs
   - browser URL and screenshot
3. Classify:
   - data-plane bug
   - generated client/contract drift
   - frontend control not wired
   - frontend visual/layout/a11y bug
   - test seed/setup bug
   - documented out-of-scope control missing disabled reason
4. Fix only the failing class.
5. Rerun the failed case first.
6. Rerun the full suite only after failed cases pass.

## Required Gates

Backend:

```bash
go test ./internal/procurement/... ./internal/processintegrity/... ./internal/vaccination/... ./internal/sop/... ./internal/proof/... ./internal/permissions/...
go test -race ./internal/procurement/adapters/postgres -run TestProcurementIdempotencyReserveSerializesConcurrentSameKey -count=1
make validate-migrations
make validate-sqlc-plans
npm --prefix tools/contract-validation run validate
```

Frontend:

```bash
npm --prefix apps/admin-web run check:mock-fidelity
npm --prefix apps/admin-web run typecheck
npm --prefix apps/admin-web run lint
npm --prefix apps/admin-web run build
npm --prefix apps/admin-web run smoke:visual:live
```

Business-chain:

```bash
bash tools/dev/vaccination-chain-proof.sh
```

Current runnable smoke wrapper:

```bash
make admin-web-e2e-smoke
```

This wrapper runs the clean Herd Register data-plane proof plus live admin-web
visual/click smoke. It is not the full four-goat procurement negative matrix
yet; keep that matrix as a remaining E2E implementation item until captured.

Full E2E is green only when the backend/API matrix, browser click matrix, visual
smoke, and contract-gap ledger all pass or have an explicit disabled-with-reason
entry.
