---
name: design-system
description: >-
  Use before, during and after ANY change under apps/admin-web/** that a person can see — a new
  page or feature, a route, table, chart, KPI card, filter bar, drawer, dialog, skeleton, tab
  strip, sidebar item, or the theme CSS — and when reviewing one. Holds the admin-web redesign
  contract: the locked Mesha palette and fonts, the page frame (PageShell/PageHeader), the kit
  components every screen is built from (Card/TableCard/BarList/KpiCard/Skeleton/AnimatedTabs/
  FilterBar/Overlay), the motion constants, the banned patterns, and the exact guard commands
  that must be green before a push. Machine backing: `npm run design:guard` (static),
  `npm run visual:stories` and `npm run visual:routes` (Storybook + route visual regression with
  render-integrity checks), beside the existing `smoke:webview` mobile guard.
version: 0.1.0
user-invocable: true
argument-hint: "[route, feature or component being changed]"
---

# Design system (admin-web)

The dashboard is being rebuilt on ONE frame and ONE kit so that every existing route and every
NEW feature looks like the same product, on a 1440px laptop and inside the 390px Android WebView.
Style reference is the Minimal (MUI) dashboard — **structure, density and motion only. Never its
palette, code, images or copy.** Owner rule: *"web and mobile must render properly — no breaking
graphs, tags, numbers, nothing."*

This skill is the contract in agent-facing form. The prose sources are
`docs/design/README.md` (brand lock + verification layers) and
`docs/progress/admin-web-redesign-progress.md` (what has landed). References:

- `references/tokens-and-fonts.md` — the locked palette, the token names, fonts.
- `references/components.md` — the frame and each kit component: when, props, what "done" is.
- `references/motion.md` — the timing constants.
- `references/banned.md` — every banned pattern with the check that catches it and the fix.

---

## The commands (non-negotiable before any UI push)

```bash
# 1. static — no app, no browser, every commit. Brand lock, banned patterns, loading.tsx,
#    PageShell adoption. Green today via the waiver file; red on any NEW instance.
npm --prefix apps/admin-web run design:guard

# 2. component visual regression — every Storybook story at 1440 + 390, dark + light, with the
#    render-integrity probe (overflow, clipped text, empty chart svg, NaN/undefined/F2 text,
#    raw floats, fonts, console errors). Needs Storybook: `npm run storybook` (port 6007).
GOATOS_STORYBOOK_URL=http://127.0.0.1:6007 npm --prefix apps/admin-web run visual:stories

# 3. route visual regression — every smoke route at desktop 1440 / phone 390 / Pixel-5 WebView
#    393, dark + light, same probe, same baseline contract. Needs a live app + backend.
GOATOS_ADMIN_WEB_BASE_URL=http://127.0.0.1:3300 GOATOS_BEARER_TOKEN=... npm --prefix apps/admin-web run visual:routes

# 4. the mobile/WebView taxonomy guard and the perf budget stay mandatory
npm --prefix apps/admin-web run smoke:webview:static
GOATOS_ADMIN_WEB_BASE_URL=http://127.0.0.1:3300 npm --prefix apps/admin-web run smoke:webview
npm --prefix apps/admin-web run test   # includes perf-capture-budget-contract.test.mjs

# focused runs while iterating
node apps/admin-web/scripts/smoke-stories-visual.mjs --storybook-url http://127.0.0.1:6007 --only kit-kpicard
node apps/admin-web/scripts/smoke-routes-visual.mjs --only alerts,verify --profiles webview --themes dark
node apps/admin-web/scripts/check-design-system.mjs --report
```

Every lane writes PNGs + `summary.json` + `integrity.json` under `.codex-goatos-render/…/<timestamp>/`
and a **diff image per failing capture**. Open them. A JSON summary is not visual proof.

### Baselines and waivers (deliberate, never casual)

- Story/route baselines: `apps/admin-web/visual-baselines/{stories,routes}/manifest.json`
  (committed fingerprints) + PNGs in `.codex-goatos-render/admin-web-{story,route}-baselines`
  (local, gitignored). Rewrite with `npm run visual:stories:update-baseline` /
  `visual:routes:update-baseline` **only for an intended change**, with the changed PNGs opened
  and the reason in the PR body. Never update the route baseline while the backend is down.
- Static debt: `apps/admin-web/scripts/check-design-system-waivers/design-system-waivers.json`.
  Integrity debt: `apps/admin-web/visual-baselines/{stories,routes}/waivers.json`.
  **A waiver list that grew in a UI change is a review finding.** P0 checks (foreign palette,
  theme token drift, Google Fonts link, brand lock) cannot be waived at all.

---

## The contract in one screen

**Brand lock (P0).** Palette lives in `apps/admin-web/app/mesha-theme.css` and does not move:
brand `#7CCB45`/`#69BA37` (dark), `#54A02C`/`#44831F` (light); bg `#0E1512`/`#FFFFFF`;
panels `#161F1A`/`#1D2820`; sidebar `#0A0F0C`; on-brand ink `#08130B`/`#FFFFFF`. Use
`var(--…)` tokens only. Fonts: Public Sans (body) + Barlow (display) via next/font — no
Google Fonts `<link>`. Banned foreign values: `#0A9F6C #4FD89A #131A21 #1B242E`.

**Frame.** Every `app/(admin)/**/page.tsx` renders inside `PageShell` (24px rhythm, 16 at
≤768) with one `PageHeader` (eyebrow · title · crumbs · actions · tabs · toolbar; **no
description slot**), and has a sibling `loading.tsx` whose skeleton uses the same shell so it
cannot bleed to the edges and matches the final layout's shape.

**Kit.** `Card` (r16, p24, hairline, elevation-1) · `TableCard` (header band, 56/44px rows,
StatusChip, portaled RowMenu, `TableFooter`, horizontal overflow scrolls inside the card) ·
`BarList` (label · track · value, grows from zero, centred axis for negatives, shared tooltip) ·
`KpiCard` (label · Barlow value · unit · TrendBadge · Sparkline, CountUp on mount) ·
`AnimatedTabs` (one strip everywhere, count badges, scrolls with fade, never wraps) ·
`FilterBar` (search · kit `SelectField`/`DateRangeField` · actions · summary) ·
MUI `Dialog` (DialogTitle/Content/Actions) / `Drawer` (components/minimal/drawer) / template `CustomPopover` + `MenuList` (row actions: `components/app/row-menu`) ·
`Skeleton*` (shape-matched, 1.5s shimmer). Charts: draw-in, floating tooltip card, rounded
caps, dashed grid, gradient area, donut with centre total.

**Motion.** Page-enter stagger 400ms / 60ms apart / 8px rise; bars 600ms / 40ms stagger;
charts ~800ms easeInOutSine / 150ms series stagger; tabs slide; dialog scale from 0.96; drawer
slide; popover fade + 4px; button press 0.98. `prefers-reduced-motion` → none.

**Banned.** Native `<select>` / `<input type=date>` (outside the kit wrapper), `window.confirm`
/ `alert`, hex colours in TSX/CSS (outside the two theme files), Tailwind palette classes,
the legacy "F2" code, fixed px widths ≥ 480 that cannot fit 390, explanatory prose under a
title or card header, a chart without the tooltip card or with `isAnimationActive={false}`, a
page without `loading.tsx`, a page outside `PageShell`.

---

## Building a NEW feature — the order

1. Read `references/components.md`; sketch the page as PageHeader + sections (KpiGrid /
   Card / TableCard / BarList). If a section needs a component the kit lacks, add it to the
   kit **with a story** first — never a one-off inside `features/`.
2. Write `page.tsx` + `loading.tsx` together; the skeleton mirrors the final layout.
3. Every state gets a Storybook story: default, empty, loading, error, mobile (390). Stories
   live in `apps/admin-web/stories/kit/*.stories.tsx` (kit) or beside the feature.
4. Add the route(s) to `scripts/smoke-visual-live.mjs` — the webview guard and the route
   visual lane read their route list from there; an unlisted route is uncovered.
5. Run the four commands above. Fix findings; do not waive them. Update baselines only for
   the captures you intended to change, and open those PNGs.
6. Hand off with the desktop AND the 390px screenshot for every touched state, both themes.

## What a reviewer must refuse

- A UI change whose proof is a 1440px screenshot only, or a JSON summary with no PNG opened.
- A grown waiver file or a rewritten baseline with no sentence explaining which capture and why.
- A new page without `loading.tsx`, outside `PageShell`, or with a paragraph under its title.
- A colour literal or a Tailwind palette class anywhere; any edit to a theme token value.
- A new component in `features/` that duplicates a kit component, or a kit change without a
  story and a green `visual:stories` run.
- A chart with no tooltip, no draw-in, or category labels that ellipsis/`----` at 390px.
- A chart imported from anything except Apex (`components/minimal/chart`, `components/kit`) or the
  two inline helpers (`svg-bars`, `svg-series`) — `raw-chart-lib` refuses recharts, d3, chart.js,
  nivo, victory, visx, echarts, highcharts.
- A NEW page without an area in `docs/design/route-template-map.json`, or a route added to
  `scripts/smoke-visual-live.mjs` without a matching `route_prefixes` entry
  (`route-template-map-missing`).

Minimal greys (`--grey-50…900`, `#F4F6F8` = `--grey-200`) are tokens written only in `app/minimal-tokens.css`; anywhere else use `var(--grey-N)` / `rgb(var(--gN-rgb)/a)` (P0 `minimal-grey-literal`).

## Tab, filter and pager clicks never reload the page

- **Tab / filter / pager clicks are soft navigations (guard: `soft-navigation`,
  `components/app/soft-navigation-guard.test.mjs`).** Links come from `next/link` or
  `@/components/no-prefetch-link`, never `next/dist/client/link` (the Pages Router Link: in `app/` it
  has no router and the browser reloads the whole document, painting the route skeleton on every
  click). GET filter/search forms use `<Form>` from `next/form`, never a native `<form method="get">`.
  URL-driven tab strips navigate through `useUrlTabNav` (`components/app/use-url-tab-nav.ts`,
  already inside `AnimatedTabs`, `SegmentTabs`, `SegmentedLinks`): `router.push(href,{scroll:false})`
  in a transition, the pressed tab selected at once, the header / crumbs / tabs / filters and the old
  panel kept on screen (dimmed at most) until the new page is ready. Never hide the live panel or
  swap the page for a skeleton on a same-route param change; `loading.tsx` is for the first entry.

## Production bug CLASSES as guards (2026-09-26)

`scripts/lib/visual-pattern-guards.mjs` codifies six recurring visible defect classes as automated
checks, wired into the route visual lane so a new instance fails at push, not in production:

- `P-text-icon-overlap` — text drawn over an icon sibling in a flex/grid row.
- `P-wide-table-no-wrapper` — table wider than viewport with no `overflow-x` ancestor; every lane.
- `P-chart-axis-tiny` — chart axis / legend / SVG text below 11px on any viewport.
- `P-pinned-bar-blur-flicker` — sticky/fixed bar with `backdrop-filter` above actually-scrolling
  content (Android WebView repaint hazard).
- `P-drawer-filter-mismatch` — an open overlay whose `data-drawer-filters` / `data-export-filters`
  differ from the page's `data-page-filters`.
- `P-chart-hover-remount` — tooltip missing after hover, or re-mounted between three rAF ticks.

`docs/design/route-template-map.json` maps every admin-web area to its MUI Minimal v7.7.0 template
section (template lives at `~/mesha/mui/Minimal_TypeScript_v7.7.0`; licensed, **not** committed).
Full pattern → guard table and the how-to-add-a-page ordering: `docs/design/README.md` §5b + §5c.
