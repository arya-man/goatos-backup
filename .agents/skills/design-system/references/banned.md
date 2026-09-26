# Banned patterns — check id, signature, fix

Static checks run in `apps/admin-web/scripts/check-design-system.mjs` over `app/`, `components/`,
`features/` (tests and stories excluded). Runtime checks run in `scripts/lib/render-integrity.mjs`
inside both visual lanes. **P0** = cannot be waived.

## Static (`npm run design:guard`)

| Check | Tier | Signature | Fix |
|---|---|---|---|
| `foreign-palette` | P0 | `#0A9F6C #4FD89A #131A21 #1B242E` anywhere | delete; use `var(--…)` |
| `brand-lock` | P0 | a locked Mesha value missing from `app/mesha-theme.css` | restore the value |
| `theme-token-drift` | P0 | a hex value on `origin/main`'s theme files no longer present | restore; rename tokens, never recolour |
| `google-fonts-link` | P0 | `fonts.googleapis.com` / `fonts.gstatic.com` | next/font in `app/layout.tsx`; Storybook uses `.storybook/fonts.css` |
| `native-select` | waivable | `<select` outside `components/kit/select-field.tsx` | `SelectField` / `LinkSelect` / template `CustomPopover` + `MenuList` |
| `native-date-input` | waivable | `type="date|datetime-local|month|week"` | `DateRangeField` / `ThemedDatePicker` |
| `window-confirm` | waivable | `confirm(` / `alert(` | kit `Dialog` with explicit actions |
| `hex-colour-in-code` | waivable | `#rrggbb` in `.ts/.tsx` (pure `#000`/`#fff` allowed) | token; SVG illustrations use `currentColor`/tokens |
| `hex-colour-in-css` | waivable | `#rrggbb` in `.css` outside the two theme files | token |
| `tailwind-palette-class` | waivable | `bg-emerald-500`, `text-slate-400`, … | kit class or token |
| `f2-literal` | waivable | `"F2"` / `>F2<` outside `lib/stage-display.ts`, `lib/stage-labels.ts` | lifecycle label from `lib/stage-labels` |
| `fixed-px-width` | waivable | `width/minWidth ≥ 480px` (inline, `w-[…px]`, `width={…}`) with no max-width/overflow on the line | `min(<N>px, 100%)`, `clamp`, or a `kit-scroll-x` owner |
| `prose-under-title` | waivable | `<p className="muted|sub|desc|help|hint|lead…">` within 3 lines of `<h1-3>`/`PageHeader`/`CardHeader` | delete the paragraph; labels, fields, tables, buttons only. Empty/error bodies belong in the state component, not under the title |
| `raw-float-format` | waivable | `.toFixed(3+)`, or a `` `${n} kg` ``-style template with no formatter inside the braces | `lib/format` (max 2 decimals) |
| `chart-without-tooltip` | waivable | recharts root with no `<Tooltip>`/`ChartTooltipCard` | add the shared tooltip |
| `chart-animation-disabled` | waivable | `isAnimationActive={false}` | remove; reduced-motion is handled centrally |
| `missing-loading-tsx` | waivable | `app/(admin)/**/page.tsx` with no sibling `loading.tsx` | add a shape-matched skeleton in `PageShell` |
| `page-outside-shell` | soft | neither the page nor its feature entry (one import hop, barrels followed) references `PageShell`/`PageHeader`/`kit-page` | wrap in `PageShell` + `PageHeader` |

Waivers: `scripts/check-design-system-waivers/design-system-waivers.json`, key
`check|file|normalized-line` — survives unrelated edits, dies when the offending line changes.
`--update-baseline` rewrites it; `--report` lists everything including waived; `--self-test`
proves every check fires on a synthetic fixture.

## Runtime (inside `visual:stories` and `visual:routes`)

| Check | Signature | Fix |
|---|---|---|
| `page-horizontal-overflow` | `documentElement.scrollWidth > viewport` | `min-width:0` on grid/flex children; scroll inside the card |
| `element-overflows-viewport` | a visible element's right edge past the viewport with no `overflow-x:auto` ancestor | same; clamp overlays to `calc(100vw - 24px)` |
| `clipped-text` | text in an `overflow:hidden|clip` box with `scrollWidth > clientWidth` (ellipsis + `title`/`aria-label` is the only escape) | let it wrap at phone width |
| `chart-svg-empty` | a chart-sized `<svg>` with 0×0 rendered size or no path/rect/circle/line marks | gate the chart on data; size the container; never paint an empty slot |
| `bad-text` | `NaN`, `undefined`, `null`, `[object`, `Infinity` rendered | format via `lib/format`; guard nulls |
| `f2-literal` | "F2" rendered as text | lifecycle labels |
| `raw-float` | a `td`/KPI value with more than two decimals | `lib/format` |
| `font-not-loaded` | Public Sans or Barlow has no loaded face | next/font vars present; Storybook `fonts.css` imported |
| `console-error` | any `console.error` / page error (transport noise filtered) | fix the error |

Route lane, phone profiles only (`smoke-routes-visual.mjs`): `mobile-axis-text-too-small`
(chart/axis text < 11px), `mobile-sticky-detached` (sticky header whose nearest overflow
ancestor is hidden/clip with no scroll owner in between), `mobile-table-or-chart-clipped`
(table/chart past the viewport with no horizontal scroll owner). `mobile-tap-target-too-small`
is recorded as informational there — the 44px rule is gated, with its own waivers, by
`check-mobile-webview.mjs` (`npm run smoke:webview`), whose static half the route lane merges
into its summary with `--with-webview-static`.

Waivers: `apps/admin-web/visual-baselines/{stories,routes}/waivers.json`, key
`check|context|target`; rewritten only by the lane's `--update-baseline`.

Minimal greys (`--grey-50…900`, `#F4F6F8` = `--grey-200`) are tokens written only in `app/minimal-tokens.css`; anywhere else use `var(--grey-N)` / `rgb(var(--gN-rgb)/a)` (P0 `minimal-grey-literal`).
