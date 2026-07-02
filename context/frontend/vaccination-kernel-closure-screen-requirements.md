# Vaccination Kernel Closure Screen Requirements

Status: frontend execution requirements.

Date: 2026-06-27

Purpose: define the UI/UX standard for the screens needed by
`context/execution/vaccination-kernel-closure-business-backlog.md`.

This is the rule for the 9 closure items: do not build plain text blocks,
generic cards, or one-off forms. These screens must look and behave like the
existing Mesha admin-web command center.

## Required Sources

Read these before implementing any closure screen:

```text
apps/admin-web/AGENTS.md
context/frontend/current-admin-web-scope.md
context/frontend/admin-web-backend-ui-contract.md
context/frontend/vaccination-process-integrity-frontend-handoff.md
context/execution/vaccination-kernel-closure-business-backlog.md
mock/goatos-dashboard-mock.html
apps/admin-web/app/mesha-theme.css
apps/admin-web/components/ui-primitives.tsx
```

## Non-Negotiable Visual Rule

`mock/goatos-dashboard-mock.html` is the admin-web UI/UX source of truth. The
current app already ports that style through `apps/admin-web/app/mesha-theme.css`.
Every new closure screen must reuse that system.

Do:

- Use the existing Mesha theme tokens, classes, density, icon sizing, hover
  states, drawers, modals, filters, table footers, and card anatomy.
- Use backend-owned page contracts for titles, columns, filters, row-click
  behavior, disabled reasons, action labels, and drawer/detail fields.
- Keep top-level command screens top-level: Control Tower, Action Center,
  Calendar, Protocol Adherence, Workflows, Config, SOP Library, Audit.
- Keep `/vaccination` as Preventive Care (PC) Vaccination operations, not a catch-all nested
  command room.

Do not:

- Build a plain white/gray CRUD table that ignores the mock.
- Replace rich drawers with flat key/value `helpgrid` blocks.
- Put all actions in a bare text footer without icons/status/disabled reasons.
- Invent a new palette, button style, tab style, or pagination style.
- Duplicate park/date/scope chips inside the page body. Scope belongs in the
  top bar or behind Filters.
- Make frontend state the source of truth for due, missed, blocked, proof,
  verification, escalation, or DLQ state.

## Existing Visual Vocabulary To Reuse

| UI need | Existing pattern |
| --- | --- |
| Font | `var(--f)` from `mesha-theme.css`: system/Inter stack, body `14px/1.45`. |
| Page header | `.phead`, `.crumb`, `h1` around 21px, subtitle `.sub`. |
| Cards | `.card`, `.card .hd`, `.card .bd`, `.kpi`, `.chartcard`; avoid generic nested-card piles. |
| Buttons | `.btn`, `.btn.sm`, `.btn.p` / `.btn.primary`, `.iconbtn`; use lucide icons where an icon exists. |
| Hover | `.btn:hover{border-color:var(--brand)}`, row hover `background:var(--bg)`, card hover border brand / slight lift, nav hover `var(--sidebar-2)`. |
| Status | `Tag` from `components/ui-primitives.tsx`, `.tag`, tones `t-ok`, `t-warn`, `t-dng`, `t-info`, `t-pur`, `t-teal`, `t-mut`. |
| Chips/filter tabs | `.chip`, `.chipset`, `.subtabs`, `.opf`, `.fchipsbar`, active `.on`, disabled `aria-disabled` with title. |
| Tables | `.card` + `.tbar` + `.tsearch` + `.screen table`; row/cell click uses `.celllink`; text truncation uses `ClipText`. |
| Pagination | `.pager2` footer, range/meta left, row-size chips/select, Previous/Next buttons right. Do not use prose pagination. |
| Work board | `.taskboard`, `.tcol`, `.task`, `.task-ac`, owner row, SLA pill, blocker line. |
| Drawers | `.drawer`, `.scrim`/`.veil`, `.dh`, `.dc`, `.df`; footer wraps; body uses rich status blocks, `.metagrid`, steppers, proof pills. |
| Modals | `.modal.on.card`, overlay, `.hd`, `.bd`, `.fld`, footer, Escape close, focus management. |
| Forms | `.fld`, label uppercase/muted, inputs using theme focus ring `var(--brand-soft)`. |
| Scope controls | top bar park/date selectors; page filters only for page-specific facets. |

## Theme Token Rules

Use only existing theme variables unless a design-system change is approved:

```text
surfaces: --bg, --panel, --panel-2
lines:    --line, --line2
text:     --ink, --muted, --faint
brand:    --brand, --brand-d, --brand-soft
states:   --ok, --warn, --danger, --info, --purple, --teal
state bg: --okx, --warnx, --dangerx, --infox, --purplex, --tealx
shadow:   --shadow
focus:    --ring / --brand-soft
```

Do not introduce Tailwind/shadcn/cyan-slate old-admin colors. Do not hardcode
new reds/greens/purples when `t-*` tones or theme vars already exist.

## Interaction Rules

- Every row/cell that represents a record must open a drawer or detail route.
- Every action must be either live through a backend command or disabled with a
  backend reason.
- Drawers must be query-param addressable where existing surfaces use that
  pattern (`?ac_row=`, `?adh_row=`, `?scope_mode=`).
- Modals must support Escape close, click-away close, focus on open, and no body
  scroll bleed.
- Hover states must be visible, not barely different from rest state.
- Desktop controls stay compact; mobile/narrow media query can increase touch
  targets.
- Long text uses `ClipText` or two-line clamp. Text must not overflow buttons,
  cards, table cells, drawer footers, or chips.

## Backend Contract Rule

Closure screens must extend backend page/UI contracts before frontend wiring.
Frontend may choose layout, density, and summary-vs-detail placement, but the
backend contract owns:

- page title and subtitle
- table/list/card columns
- filter labels, values, counts, defaults, and disabled reasons
- sort keys and page-size options
- row-click query param and drawer object IDs
- drawer title, summary fields, detail fields, status blocks, actions, and
  disabled reasons
- empty/error copy
- available commands and required permissions

If a backend contract is not ready, render the mock-shaped control disabled with
reason. Do not simplify it into a bare text note.

## Screen Requirements By Closure Item

### 1. Publish Generation Status And Retry

Route:

```text
/config
```

Product surface:

- Admin/Data Ops protocol rule publish panel.
- Shows generation status after publish: queued, running, completed, failed,
  retrying.
- Shows affected goat count, obligations created, skipped/deferred/blocked
  counts, last run time, failed cursor/error, and retry action.

UI pattern:

- Use `/config` authority-screen anatomy, not a separate route.
- Publish status should be a compact `.card` section or drawer section next to
  the rule version detail.
- Use `Tag` tones: queued/info, running/warn, completed/ok, failed/dng,
  skipped/mut.
- Retry is `.btn.sm` with refresh/replay icon and backend disabled reason when
  not allowed.
- Generation history rows use table anatomy with `.tbar`, `.tsearch`, row
  click, `.pager2`.

Backend contract needed:

- Rule version detail includes `generation_status`, `generation_runs`,
  `retry_action`, and disabled reason.
- Table contract for generation runs.

Acceptance:

- User can publish and see generation progress without reading logs.
- Failed run opens drawer with error/cursor/retry/audit chain.

### 2. Event Delivery Health

Route:

```text
No product route required for first build.
Optional later: /operations/audit or Control Tower ops-health panel.
```

Product surface:

- This is backend/infra first. The UI only needs a health signal once metrics
  exist.

UI pattern if surfaced:

- Control Tower kernel-health strip using `.alert` / `.alert.warn` and compact
  KPI cards.
- Link to the implemented DLQ Operation Center at `/operations/dlq`.
- Do not create a fake live event dashboard before backend metrics exist.

Backend contract needed:

- Kernel health DTO: relay lag, oldest outbox age, consumer lag, failed count,
  last successful delivery, last failure.

Acceptance:

- If worker delivery is stopped, UI/ops alert shows a clear degraded state.

### 3. Full Goat Shift Repair Visibility

Routes:

```text
/vaccination
/vaccination/execution/sheds/[shedId]
/calendar
/action-center
```

Product surface:

- Show that a shifted goat was removed from old pending work and moved/merged
  into new shed work, or show a repair exception if that is not possible.

UI pattern:

- In `/vaccination`, use existing shed execution sections and `.pexec` rows.
- In shed detail, use `.card` sections for "Drive repair" and "Moved goats".
- Old and new shed rows use `Tag` tones and linked `.celllink` rows.
- Repair drawer uses `.metagrid`: Old shed, New shed, Batch, Stock release,
  Stock reserve, Repair status, Owner, Next action, Audit.
- If old drive already started/completed, show `.alert.warn` and Action Center
  task, not silent movement.
- Calendar/Action Center rows use existing status/blocker line, not a separate
  notification-only view.

Backend contract needed:

- Batch repair status, old/new batch IDs, stock release/reserve summary, repair
  exception state, action availability.

Acceptance:

- A moved goat can be traced from old shed pending drive to new shed drive or
  explicit exception drawer.

### 4. Sick / ICU / Quarantine Defer And Recovery

Routes:

```text
/vaccination
/action-center
/calendar
/goats/{goat_id}
```

Product surface:

- Preventive Care (PC) / Parks sees why vaccination is held, who owns review, and when recovery
  re-check created or reopened due work.

UI pattern:

- Use status tags: `deferred` / `blocked` with `t-warn` or `t-dng` based on
  risk.
- Use Action Center task cards for review/recovery work, not plain notes.
- Goat Passport shows timeline entries for deferred, recovery detected,
  re-evaluated, due created/canceled.
- Drawer must show reason, source state, rule defer policy, review owner,
  recovery event, due-after-recovery, and next action in `.metagrid`.
- Filters include defer reason and state chips through modal/filter contract.

Backend contract needed:

- Defer reason, lifecycle/health source, defer policy ID, recovery status,
  re-evaluation run ID, next action.

Acceptance:

- Sick/ICU/quarantine goats show visible hold reason and recovery path across
  Preventive Care (PC) / Vaccination, Action Center, Calendar, and Goat Passport.

### 5. Hard Stock-Blocked Execution

Routes:

```text
/vaccination
/vaccination/execution/sheds/[shedId]
/action-center
/calendar
```

Product surface:

- Operator cannot start/submit a vaccination drive with missing, expired,
  quarantined, insufficient, wrong-location, or cold-chain-blocked stock.

UI pattern:

- Use `.alert.warn` or danger alert at drive top with exact stock blocker.
- Disable Start SOP / Submit proof buttons with backend reason.
- Show stock block as Action Center work item with Inventory owner.
- Execution rows show stock pill: available/shortfall/expired/quarantined.
- Drawer `.metagrid`: Required dose, available dose, lot, expiry, location,
  cold-chain, reservation, owner, override policy.
- Use existing Inventory-style `Tag` tones, not new colors.

Backend contract needed:

- Stock gate state, reason enum, lot/quantity/expiry/location, allowed actions,
  override permission/disabled reason.

Acceptance:

- Bad stock blocks execution visually and functionally.
- Inventory owner gets a clear action row and the operator sees why blocked.

### 6. DLQ Operation Center

Route:

```text
Recommended: /operations/dlq or an Operations/Data Ops section linked from /operations/audit.
```

Product surface:

- Ops/Data/Engineering can inspect, replay, discard/ack, and audit failed
  kernel events without shell access.

UI pattern:

- Use Audit Log / Activity Trail anatomy, not a raw JSON dump.
- Top KPIs: failed, dead-letter, oldest, replayed today.
- Filter chips: event type, source, status, tenant/scope, age, attempts.
- Search uses `.tsearch`; advanced filters use `.modal.on.card`.
- Table rows are `.celllink` and open drawer.
- Drawer has business summary first, then technical payload secondary.
- Raw JSON is collapsed/secondary in monospace; not the main UI.
- Footer actions wrap: Replay, Discard/Ack, Copy trace, Open source entity.
- Use `.pager2`; for million-goat/event scale prefer cursor pagination and do
  not fake total counts.

Backend contract needed:

- DLQ list/detail/replay/discard APIs with permissions, audit, payload summary,
  trace IDs, and disabled reasons.

Acceptance:

- Operator can triage dead-letter event from list to replay/discard with audit
  and without terminal access.

### 7. Kernel Health Panel

Route:

```text
/ or /operations/audit, with link to DLQ Operation Center.
```

Product surface:

- Shows whether kernel automation is healthy: outbox age, DLQ count, worker lag,
  scheduler failure, Cloud Tasks failure, notification failure, projection lag.

UI pattern:

- Control Tower style KPI strip plus alert banner.
- Green/yellow/red state using existing tones only.
- Each KPI opens a drawer with metric history, last failure, owner, and link to
  DLQ/worker logs/runbook.
- Do not create a dense engineer-only metrics page as the first product
  surface.

Backend contract needed:

- Kernel health summary DTO and metric detail links.

Acceptance:

- A broken worker produces a visible degraded state and links to action.

### 8. Incident Escalation Status

Routes:

```text
/calendar
/action-center
/workflows/{row_id}
```

Product surface:

- When a critical SLA creates an external incident, users can see incident
  status/reference from the same escalation drawer.

UI pattern:

- Add incident block inside existing escalation/detail drawer, not a new vendor
  dashboard.
- Use `Tag`: incident queued/info, open/dng, acknowledged/warn, resolved/ok.
- External incident ID is a compact mono chip with copy action.
- Resolve/ack buttons remain Goat OS actions; vendor status is secondary.

Backend contract needed:

- Escalation detail includes incident provider, external incident ID, external
  status, last sync, create/resolve disabled reasons.

Acceptance:

- Critical escalation drawer shows whether incident was created and whether it
  is resolved.

### 9. Stage-Change And Manual-Campaign UI

Routes:

```text
/goats/{goat_id}
/config
/vaccination
/action-center
```

Product surface:

- Stage-change events can be inspected.
- Manual campaigns can be created/approved/executed with scope, reason, and
  approval.

UI pattern:

- Goat Passport timeline shows stage change with old/new stage, source, actor,
  resulting rule re-check.
- Config exposes manual campaign as approved action only when backend contract
  allows it.
- Manual campaign creation uses `.modal.on.card` with `.fld` inputs, scope
  selectors, rule/version picker, reason, scheduled date, and proof/SOP policy.
- Campaign preview shows affected goat count, stock readiness, defer/blocked
  counts, and duplicate suppression before Create.
- Campaign rows show in `/vaccination` using drive/batch anatomy and in Action
  Center as task cards once due.
- Filters include campaign/manual trigger chips via backend contract.

Backend contract needed:

- Stage-change event detail, campaign preview, campaign create/approve/cancel,
  generated obligations/batches, permission/disabled reasons.

Acceptance:

- Manual campaign can be previewed, approved, and traced to obligations/drives.
- Stage-change rule re-check is visible in goat/process history.

## Screen Inventory

| Closure item | Route/body | New screen or existing screen extension? | Required UI elements |
| --- | --- | --- | --- |
| 1 | `/config` | Existing extension | Rule detail status card, generation runs table, retry drawer/action. |
| 2 | none first | No product screen | Optional health DTO later; visible through item 7. |
| 3 | `/vaccination`, `/vaccination/execution/sheds/[shedId]` | Existing extension | Drive repair rows, moved-goat drawer, repair exception alert. |
| 4 | `/vaccination`, `/action-center`, `/calendar`, `/goats/{goat_id}` | Existing extension | Deferred/recovery tags, Action Center cards, Passport timeline, drawer. |
| 5 | `/vaccination`, shed execution, `/action-center`, `/calendar` | Existing extension | Stock block alert, disabled execution actions, Inventory owner task, drawer. |
| 6 | `/operations/dlq` or Operations/Data Ops section | Implemented ops screen | KPI strip, filters, DLQ table, detail drawer, replay/discard actions. |
| 7 | `/` or `/operations/audit` | Existing/minimal extension | Kernel health KPI strip, alert, metric drawer, DLQ link. |
| 8 | `/calendar`, `/action-center`, `/workflows/{row_id}` | Existing extension | Incident block in escalation drawer, external ID/status. |
| 9 | `/goats/{goat_id}`, `/config`, `/vaccination` | Existing extension plus modal | Stage timeline, manual campaign modal, preview, campaign rows. |

## Filters And Pagination Requirements

- Use backend-owned filters where possible. Local visible-row search is allowed
  only as a temporary enhancement over bounded returned rows and must be labeled
  honestly.
- Filter drawers use existing modal anatomy: `.modal.on.card`, overlay, `.fld`,
  chips, Clear, Apply, Done.
- Filter chips preserve top-bar scope. A domain/work-state chip must not clear
  selected park/shed/date scope unless the backend contract says so.
- Pagination uses `.pager2`. Offset page sizes can use `[5, 10, 25, 50]` where
  bounded data allows it; million-scale lists use cursor pagination with no fake
  total count.
- Table cells with long labels use `ClipText`; no text overflow or clipped
  drawer footer buttons.

## Done Criteria

Each closure screen is not done until:

1. Backend contract owns labels, columns, filters, actions, disabled reasons,
   and drawer fields.
2. JSX uses existing Mesha classes/primitives instead of a new visual system.
3. Row/cell click opens the correct drawer/detail route.
4. Empty, loading, unauthorized, failed, blocked, disabled, and no-data states
   are mock-shaped and honest.
5. Desktop and mobile/narrow widths do not overlap, clip, or lose actions.
6. `npm --prefix apps/admin-web run check:mock-fidelity` passes.
7. `npm --prefix apps/admin-web run lint`, `typecheck`, and `build` pass.
8. Live visual smoke screenshots are inspected when backend/admin-web can run.
9. A route/drawer/modal ledger entry records mock source, app component, backend
   fields, disabled gaps, and screenshot proof.
