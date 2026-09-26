# Components: the frame and the kit

All from `apps/admin-web/components/kit` (barrel `index.ts`). Stories under
`apps/admin-web/stories/**` render every state; `npm run storybook` (port 6007) to browse.

## Frame

**PageShell** — `<PageShell ariaLabel>` wraps the whole page body. `display:grid; gap:24px`
(16 at ≤768), `min-width:0` on every section, children's own margins dropped. Every
`app/(admin)/**/page.tsx` AND its `loading.tsx` render inside it (`page-outside-shell`,
`missing-loading-tsx`). Story: `Kit/PageFrame`.

**PageHeader** — `title` (Barlow 28/32), optional `eyebrow` (module word), `crumbs`
("Module • Page", last crumb muted), `actions` (right-aligned buttons), `tabs` (one
`AnimatedTabs`), `toolbar` (a `FilterBar`). **There is no description prop on purpose.** A
`<p className="muted">` within three lines of a heading is `prose-under-title`.

**Sidebar** — section eyebrows per IA group, active group auto-expanded, one active style (soft
brand pill), active leaf scrolled into view. Owned by `components/nav*`; do not restyle per route.

## Surfaces

**Card / CardHeader** — radius 16, padding 24, hairline border, elevation-1. `CardHeader`
title + optional action, 16px below; sections separated by hairlines. `subtitle` exists for a
one-line unit/scope label ("Head received per month"), never for a paragraph.

**TableCard / DataTable treatment** — Card with padding 0; header band on `--panel-2`, 12/600
uppercase muted labels, sort arrow on the active column; rows 56px (dense 44 via
`DenseToggle`), 16px horizontal cell padding, hover lift without layout jump; identity cell
(avatar/tile + two-line text); `StatusChip` (soft tint + strong text: Paid/Published brand,
Overdue red, Draft muted, Progress amber); `RowMenu` (portaled ⋮, never clipped);
`TableFooter` (dense toggle · rows per page (kit select) · "1–25 of 97" · ‹ ›). Wide tables
scroll inside the card with fade edges (`kit-scroll-x`), never the page. Numbers come through
`lib/format` — a cell with >2 decimals is `raw-float`.

**BarList** — replaces every CSS hbar block. `rows[{key,label,value,display?,note?,color?}]` or
`groups`, `ariaLabel` required, `legend` chips above, `domain` to share a scale. Label column
(min 160) · rounded track fill growing from zero (600ms, 40ms stagger) · right-aligned value
inside the card gutter; negatives get a centred axis and red fill to the left; hover dims
siblings and shows the shared `ChartHover` tooltip. Story: `Kit/PageFrame/BarListSigned`.

**KpiCard / KpiGrid** — `label` · `value` (CountUp, Barlow 32) · `unit` · `trend`
(TrendBadge: ▲ chip + caption) · `sparkline` right; `variant="tint"` adds soft brand bg + dotted
pattern + watermark icon. Equal heights inside `KpiGrid`. Stat strip = one card split by
hairlines with `IconBadge` ring per cell.

**Charts** (`TrendChart`, `DonutChart`, `ChartTooltipCard`, `RadialStat`, `Sparkline`, plus
`components/svg-*.tsx` + `charts-premium.css`) — draw-in on mount (bars grow, lines draw
left→right, donut sweeps; ~800ms easeInOutSine, ~150ms stagger), floating tooltip card that
follows the pointer (fade+scale 150ms), hovered mark lightens ~15% while siblings dim, rounded
caps, dashed gridlines, gradient area fill, donut with centre total + legend below, legend dots
top-right for cartesian. A chart file with no `<Tooltip>`/`ChartTooltipCard` is
`chart-without-tooltip`; `isAnimationActive={false}` is `chart-animation-disabled`. At 390px
every category label renders (no `----`, no ellipsis) — the webview guard asserts it.

**AnimatedTabs / TabPanel** — `items[{value,label,count?,icon?}]`, `variant` underline|pill,
sliding indicator, count badges, horizontal scroll with fade at overflow, never wraps; panel
crossfade without height jump. One strip component for every route.

**FilterBar** — `search` · children (kit `SelectField`, `LinkSelect`, `DateRangeField`, chips)
· `actions` · `summary` row ("12 results" + active chips). Wraps at phone width with ≥96px
controls and 44px targets. Native `<select>` and `<input type=date>` are banned outside the kit
wrappers (`native-select`, `native-date-input`).

**Overlay** — `Dialog` (scale-in from 0.96, header row, actions right; the ONLY confirm — no
`window.confirm`), `Sheet` (drawer slide, 24px padding, title + close, definition-list body,
chips in their own column, scrollable body with `overflow-y:auto`), template `CustomPopover` +
MUI `MenuList`/`MenuItem` (in-place lists: `components/app/dropdown-paper`), tooltip.

**Skeleton family** — `SkeletonKpiRow`, `SkeletonTable`, `SkeletonChart`, `SkeletonList`,
`SkeletonCard`, `SkeletonDrawer`, shimmer 1.5s left→right. `loading.tsx` composes them inside
the same `PageShell` + `PageHeader` as the page so the shape matches (kanban → columns of card
blocks, table → header + N row bars, KPI → title bar + number block + spark block). Generic grey
rectangles = fail.

**Buttons** — `Button` variants contained|soft|outlined|text, tones, ripple + press scale 0.98,
hover lift; `iconbtn` for icon-only (44px at phone). `RetryButton` for error states.

**Notifications panel** — drawer with header + mark-all + settings; tabs "All · Unread ·
Archived" with counts; item = icon/avatar + bold actor + action + object, meta line "time ·
category", unread dot, inline actions, quoted block, tag chips, "View all" footer. No
"Enable notifications" nag, no four-line prose per item.

## Adding to the kit

New component → `components/kit/<name>.tsx`, export from `index.ts`, tokens only, reduced-motion
branch, a `stories/kit/<Name>.stories.tsx` with default / states / mobile (390) / play
assertions, then `visual:stories --only <id>` green before use in a feature.
