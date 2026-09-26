# Admin-web design system & UI redesign

Living record of the admin-web visual redesign: what the rules are, what has been built,
how it is verified, and what is still open. Branch: `design/minimal-redesign-preview`.

---

## 1. Brand lock (non-negotiable)

Mesha's palette is fixed. A redesign may change layout, components, motion and icons —
**never the colors.**

| Token | Dark | Light |
|---|---|---|
| brand | `#7CCB45` (hover `#69BA37`, soft `rgba(124,203,69,.15)`) | `#54A02C` / `#44831F` |
| page bg | `#0E1512` | `#FFFFFF` (Minimal) |
| panel / panel-2 | `#161F1A` / `#1D2820` | `#FFFFFF` / `#F1F5EF` |
| sidebar | `#0A0F0C` | `#ECF1E8` |
| on-brand ink | `#08130B` | `#FFFFFF` |

Plus the existing status colors and the `--gain-*` ramp. Canonical source:
`apps/admin-web/app/mesha-theme.css` (aliases and overrides in `app/minimal-theme.css`).

**Banned anywhere in the codebase:** `#0A9F6C`, `#4FD89A`, `#131A21`, `#1B242E`
(another product's palette), and hardcoded Tailwind palette classes (`slate-*`, `rose-*`, …).
Use `var(--…)` tokens only.

Any change to a color token in the two theme files is a **P0 defect**, caught by
`git diff origin/main -- app/mesha-theme.css app/minimal-theme.css`.

Reference material from third-party templates is **style reference only**: patterns, spacing,
motion — never their code, CSS, images, illustrations or wording. Icons are lucide.

## 2. Pattern spec

The visual contract for every page: page chrome and breadcrumbs, table toolbar
(Columns/Filters/Export/Settings, date range, search, filter chips), tables (identity cell,
status chips, sticky header, sort arrow, row menu, footer with dense toggle + rows-per-page +
range + arrows), shape-matched skeletons, stat strips / tinted KPI cards / progress rows /
radial stats, tabs with count badges and content transition, form and editor patterns,
detail pages with timelines, content cards, sidebar nesting, and motion timings.

Full spec: `scratchpad/redesign/audit/pattern-spec.md` (to be moved under `docs/design/` when
the branch lands).

## 3. Component kit

`apps/admin-web/components/kit/` — Card, CardHeader, KpiCard (+ tint variants), CountUp,
TrendBadge, IconBadge, Sparkline (line/bar), ProgressRow, RadialStat, ListRow, SelectField,
LinkSelect, FilterBar, AnimatedTabs (+ count badges, TabPanel transition), DataTable treatment,
TableFooter, RowMenu, DenseToggle, Overlay (Dialog/Drawer/Popover/Menu/Tooltip), charts,
PageEnter, Skeleton variants (KpiRow, Table, Chart, List, Card, Drawer), ThemeToggle,
RetryButton, tone/theme helpers.

Shared chart components (`components/svg-bars.tsx`, `svg-series.tsx`, `svg-column-bars.tsx`,
`hbar-list.tsx`, `grouped-columns.tsx`, `month-columns.tsx`, `chart-hover.tsx`) carry the
premium chart presentation in `components/charts-premium.css` (tokens only, zero color literals):
tracks, rounded caps, dashed gridlines, right-aligned value column, donut with centre total,
gradient area fill, unified tooltip, draw-in animation with stagger.

`app/kit-preview` is a temporary gallery route. **It must be deleted before merge** — Storybook
replaces it.

## 4. Verification layers

A UI change is not done until every layer is green. The agent-facing contract is
`.agents/skills/design-system/SKILL.md` (read it first); the layers below are its machine backing.

| Layer | What it proves | Command | Runs |
|---|---|---|---|
| 0. Design guard (static) | brand lock (P0), banned patterns, `loading.tsx` beside every admin page, PageShell adoption — see `references/banned.md` | `npm run design:guard` (`design:guard:report`, `design:guard:update-baseline`) | every commit, `make ci-local` |
| 1. Story visual regression + integrity | EVERY Storybook story at 1440x900 and 390x844, dark + light; interaction frames for tabs / dialog / drawer / row menu / chart hover / chart draw-in; render-integrity probe per capture | `npm run visual:stories` | `make ci-local` (skipped only under `GOATOS_FAST_LOCAL_CI`) |
| 2. Route visual regression + integrity | every smoke route at desktop 1440 / phone 390 (iPhone UA) / webview 390 + webview-small 360 (Android WebView UA, `; wv`), dark + light; shared probe + mobile probe (axis text ≥ 11px, tap targets ≥ 44px, sticky headers, clipped tables/charts); optional drawer capture | `npm run visual:routes` | `make ci-local` when `GOATOS_ADMIN_WEB_BASE_URL` is set |
| 3. Mobile / WebView taxonomy | the seven recurring failure classes (§5) | `npm run smoke:webview:static`, `npm run smoke:webview` | every commit (static) / live app |
| 4. Legacy lanes | route health assertions, a11y, sales tolerance layout, click matrices | `npm run smoke:visual:live`, `smoke:visual:all`, `responsive:guard` | live app |
| 5. Perf budget | capture budget contract | `npm run test` (`scripts/perf-capture-budget-contract.test.mjs`) | every commit |

Supporting guards: `smoke-visual-route-coverage.test.mjs`, `check-mock-fidelity.mjs`,
`check-token-leak.mjs`, `npx tsc --noEmit`, `npm run lint`. All registered in
`tools/ci/run-local-ci.sh` (`run_admin_web`) and `tools/ci/guardrail-manifest.json`, so
`make land-main` refuses a red lane.

### `visual:stories` — Storybook regression, Paparazzi-style

```bash
# Storybook already running on :6007 (npm run storybook)
GOATOS_STORYBOOK_URL=http://127.0.0.1:6007 npm --prefix apps/admin-web run visual:stories
# or let the lane build storybook-static itself
npm --prefix apps/admin-web run visual:stories

# focused run while iterating (one browser, sequential)
node apps/admin-web/scripts/smoke-stories-visual.mjs --storybook-url http://127.0.0.1:6007 --only kit-kpicard
node apps/admin-web/scripts/smoke-stories-visual.mjs --storybook-url http://127.0.0.1:6007 --only motionframes --themes dark

# intended change: rewrite manifest + local PNGs + integrity waivers, then OPEN the changed PNGs
GOATOS_STORYBOOK_URL=http://127.0.0.1:6007 npm --prefix apps/admin-web run visual:stories:update-baseline
```

What it does, per story from `index.json` (a story that is skipped or never reached is a failure):
1. renders it at desktop 1440x900 and mobile 390x844, dark and light, waits for the story's
   terminal phase (a thrown play function fails the story), waits for Public Sans + Barlow to be
   LOADED, freezes motion, captures full page;
2. runs `scripts/lib/render-integrity.mjs`: page/element horizontal overflow, clipped text, empty
   chart `<svg>`, `NaN`/`undefined`/`[object`/`F2` text, raw floats in cells, fonts, console
   errors — findings fail the story unless keyed in `visual-baselines/stories/waivers.json`;
3. for stories with `parameters.motionFrames` (`stories/kit/MotionFrames.stories.tsx`) reloads
   with motion live, pauses the page clock (CDP virtual time), fires the trigger and captures a
   frame at each requested ms (tabs 0/60/180/300, dialog + drawer 0/60/180/300, row menu on the
   last row 0/60/180, chart hover 60/500, chart draw-in 150/450/900) — each frame is its own
   baseline, so a flicker, height jump or missing indicator is a red frame with a diff image;
4. diffs against the baseline: pixelmatch (threshold 0.1, max ratio 0.01) when the local PNG in
   `.codex-goatos-render/admin-web-story-baselines/` exists, else the committed perceptual hash
   in `apps/admin-web/visual-baselines/stories/manifest.json`; prints the diff image path.

Output: `.codex-goatos-render/admin-web-story-screenshots/<timestamp>/{summary,integrity,stories}.json`
+ PNGs + `diffs/`. The summary reports `stories_in_index`, `stories_passed`, `stories_failed`,
`stories_unreached`.

### `visual:routes` — route regression at desktop, phone and WebView

```bash
GOATOS_ADMIN_WEB_BASE_URL=http://127.0.0.1:3300 GOATOS_BEARER_TOKEN=<token> \
  npm --prefix apps/admin-web run visual:routes
# phone-critical routes only (scripts/webview-critical-routes.json), drawers opened, static webview lane merged
node apps/admin-web/scripts/smoke-routes-visual.mjs --webview-critical --open-drawers --with-webview-static
# focused
node apps/admin-web/scripts/smoke-routes-visual.mjs --only alerts,verify --profiles webview --themes dark
```

Route list = `scripts/smoke-visual-live.mjs` (parsed, never re-typed). Same baseline contract
(`visual-baselines/routes/manifest.json`, local PNGs in
`.codex-goatos-render/admin-web-route-baselines/`, waivers in `visual-baselines/routes/waivers.json`).
Never run `--update-baseline` while the backend is down. The per-route table at the end marks
`[webview-critical]` routes — those are the ones driven on the real Android emulator at the final gate.

### Baseline storage decision

PNG baselines are **not committed**: ~1,000 story captures + ~800 route captures per sweep is
~150MB that churns on every intended change, against a 5MB tracked-file guard. The committed
artefact is the manifest (size + sha256 + 256-bit perceptual hash per capture), which is small,
diffable per capture and enough to fail a run whose picture moved; the PNGs live beside the other
render output in the gitignored `.codex-goatos-render/` so pixelmatch can still produce a diff
image locally. A machine without the PNGs falls back to hash distance (max 0.04) and says so.

## 5. Mobile / WebView

Desktop Chrome at 390px is an approximation. The Android **WebView** differs in font boosting,
`100vh` vs `dvh`, sticky positioning, `backdrop-filter`, keyboard insets and safe areas — the
source of repeated regressions (most recently chart axis labels rendering as `----` on Load wise).

`scripts/check-mobile-webview.mjs` encodes the recurring failure classes as automated checks at a
real Android-Chrome device profile: no horizontal page scroll, no clipped content, chart labels
present and legible, tap targets ≥ 44px, sticky headers that stick, dialogs above their backdrop,
table scroll contained in its card, pagination reachable. The taxonomy behind it (mined from git
history, with commit references and detectable signatures per class) is in
`scratchpad/redesign/audit/…/webview/taxonomy.md`.

Rules for agents (Claude and Codex) live in `.agents/skills/` (`design-system`,
`mobile-webview-guard`) and the AGENTS.md visual-QA sections: **every UI change runs the design
guard, the story and route visual lanes, and the WebView guard before push.**

The Android operator app is native Compose and does not embed admin-web in a WebView today; the
WebView profiles emulate Android Chrome / WebView rendering of `dashboard.mesha.sg` on a phone.
`scripts/webview-critical-routes.json` lists the phone-critical routes (derived from the app's
native module routes + the leadership surfaces); `--webview-critical` runs only those.

## 5b. Pattern → Guard (production bug CLASSES)

Every recurring visible defect is an automated check. New instances fail at push, not in production.

| Pattern | Guard | Lane |
|---|---|---|
| text over text | `text-overlap` in `scripts/lib/regression-checks.mjs` | route visual live |
| text over icon in flex/grid row | `P-text-icon-overlap` in `scripts/lib/visual-pattern-guards.mjs` | route visual |
| words broken mid-word / numbers or ₹ split | `C-cell-mid-word-wrap`, `chip-crushed` in regression-checks | route visual live |
| table text spilling into next column | `C-cell-overpaint` in regression-checks | route visual live |
| wide table without scroll wrapper | `table-not-scroll-contained` in `check-mobile-webview.mjs` + `P-wide-table-no-wrapper` in visual-pattern-guards (every lane) | webview + route visual |
| clipped / crushed labels on phone | `clipped-text` in `render-integrity.mjs`, `chart-label-clipped` in webview | route visual + webview |
| tap targets < 44px on phone | `tap-target-too-small` in webview | webview |
| page overflow at 390 / 412 | `page-horizontal-scroll` in webview, `D-page-overflow` in regression-checks | webview + route visual |
| chart text < 11px on any viewport | `P-chart-axis-tiny` in visual-pattern-guards (11px), `A-svg-text-tiny` in regression-checks (8px floor), `font-below-legibility` in webview (10px phone floor) | route visual + webview |
| chart labels / legend / tooltip missing or clipped | `A-chart-label-*` in regression-checks, `chart-label-*` in webview | route visual + webview |
| chart hover tooltip missing OR re-mounted between frames | `P-chart-hover-remount` (`assertChartHoverStability`) in visual-pattern-guards | route visual |
| pinned bar blurring content flickers on Android WebView | `P-pinned-bar-blur-flicker` in visual-pattern-guards (runtime), `backdrop-filter-without-supports` in webview (static) | route visual + webview |
| drawer / export / modal must inherit page filters | `P-drawer-filter-mismatch` in visual-pattern-guards (compares `[data-drawer-filters]` / `[data-export-filters]` to `[data-page-filters]`) | route visual |
| internal codes (snake_case) shown to users | `J-raw-text` in regression-checks | route visual live |
| YYYY-MM-DD visible dates | `J-raw-text` + `make date-format-guard` | route visual + static |
| raw floats in KPI / cells / chart labels | `raw-float` in `render-integrity.mjs`, `raw-float-format` in `check-design-system.mjs` | design:guard + route visual |
| card / section spacing off template grid | `raw-px` / `raw-radius` / `raw-shadow` / `raw-font-size` ratchet in `design-kit-ratchet.mjs` (tokens only, shrink-only baseline) | design:guard |
| non-Apex chart libraries (recharts, d3, chart.js, nivo, victory, visx, echarts, highcharts, …) | `raw-chart-lib` in check-design-system | design:guard |
| new page without a template mapping | `route-template-map-missing` in check-design-system (see `route-template-map.json`) | design:guard |

The MUI Minimal template is the reference: `~/mesha/mui/Minimal_TypeScript_v7.7.0` (Ravi laptop; licensed source, **NOT** committed to the repo). Every admin-web area maps to a template SECTION in `docs/design/route-template-map.json` so a reviewer can compare the rendered page against the template it was built from. The Mesha palette is locked; template gives structure, density, motion, and interaction patterns only — never brand colours, images or copy.

## 5c. Adding a NEW page

1. Pick the closest template section from `docs/design/route-template-map.json` (or add a new area
   mapping in the same change if no existing area fits). The template lives at `~/mesha/mui/…`.
2. Port the section to `components/minimal/<area>/` or reuse a kit component. **Never** copy the
   template's brand colours, images or copy — palette is the locked Mesha green, icons are lucide.
3. Add `page.tsx` inside PageShell/DashboardLayout with a matching `loading.tsx`.
4. Write one Storybook story per state (default, empty, loading, error, mobile). Kit components
   live under `stories/kit/`, features beside the feature.
5. Add the route to `scripts/smoke-visual-live.mjs` so the route visual + webview lanes cover it.
6. Verify at 1440 / 390 / 412, dark + light, chart hover interactive; run `npm run design:guard`,
   `npm run visual:stories`, `npm run visual:routes` and `npm run smoke:webview`.

## 6. Inventory & worklists

- Master route checklist (63 entries: 59 renderable routes, 69 tab strips, 360 controls,
  121 overlays, 124 tables): `scratchpad/redesign/audit/checklist.md`
- Per-module gap lists with P0/P1/P2 items: `scratchpad/redesign/audit/gaps/*.md`

## 7. Known open items

- `app/kit-preview` must be deleted before merge.
- `/routines` cannot render on the current local backend (page not in this principal's contract),
  so its redesign is unverified; the Feed cards SOP editor is likewise unreachable with current data.
- `/leave` has no rows in local data — only its empty state has been verified.
- Legend placement differs between recharts (pinned top-right) and the SVG charts (top-left).
- `svg-series` uses one viewBox across wide and narrow cards, so axis label sizing varies.

Neutrals are the MUI Minimal TEMPLATE's (Ravi 2026-09-27): grey scale `--grey-50…900` = `#FCFDFD…#141A21`, dark surfaces `#141A21` / paper `#1C252E` / neutral `#28323D`, light `#FFFFFF` / neutral `#F4F6F8`, text and divider as the template derives them; only brand + status hues are Mesha. `theme/theme-config.ts` and `app/minimal-tokens.css` must carry exactly those values (P0 `template-neutrals`); the retired green-tinted neutrals (`#0E1512`, `#161F1A`, `#94A89A`, `#F4F7F2` …) fail everywhere (P0 `retired-neutral-literal`). Write greys as `var(--grey-N)` / `rgb(var(--g500-rgb)/a)` or theme tokens.
