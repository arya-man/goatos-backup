---
name: mobile-webview-guard
description: >-
  Use before, during and after ANY browser-visible change under apps/admin-web/** — a page,
  route, table, chart, KPI strip, filter bar, drawer, modal, popover, pager, or the theme CSS —
  and when reviewing one. Holds the taxonomy of the seven mobile/Android-WebView defect classes
  that have been re-fixed ~40 times (horizontal overflow at phone width, clipped control and
  cell text, missing or `----` chart labels, sub-44px tap targets, drawer/overlay scroll traps,
  `100vh` instead of `100dvh`, filter rows collapsing wrong), the concrete runtime check for
  each, and the exact commands that must pass before any UI push. Machine backing:
  `apps/admin-web/scripts/check-mobile-webview.mjs` (`npm run smoke:webview`).
version: 0.1.0
user-invocable: true
argument-hint: "[route or component being changed]"
---

# Mobile / Android-WebView guard (admin-web)

The Goat OS dashboard is opened on phones. The Android app shows admin-web pages in a
**WebView**, whose URL bar resizes the viewport, whose font boosting changes text metrics, and
which shrink-to-fit zooms the whole page out the moment anything is wider than the screen.

The same seven defects have shipped and been re-fixed ~40 times between Aug and Sep 2026.
Each fix was one route, one selector, one commit. Nothing stopped the next feature from
reintroducing it two days later — because "I checked it at 1440px" was accepted as proof.

**This skill exists so that stops.** Desktop-only proof is not proof. Read the taxonomy,
write the code so the signature cannot occur, then run both lanes.

---

## The commands (non-negotiable before any UI push)

```bash
# 1. static half — no app, no browser, runs anywhere, every commit
npm --prefix apps/admin-web run smoke:webview:static

# 2. full sweep — every smoke route, 1440x900 AND Pixel 5 (393x851, Android UA, touch,
#    deviceScaleFactor), in BOTH themes. Needs a live admin-web + backend.
GOATOS_ADMIN_WEB_BASE_URL=http://127.0.0.1:3300 npm --prefix apps/admin-web run smoke:webview

# 3. the existing visual lanes stay mandatory, they are not replaced by this one
npm --prefix apps/admin-web run smoke:visual:live
npm --prefix apps/admin-web run responsive:guard

# focused run while iterating on one page
node apps/admin-web/scripts/check-mobile-webview.mjs --routes feed-analytics,sales-loads

# full report with nothing waived (what a reviewer reads)
npm --prefix apps/admin-web run smoke:webview:report
```

The guard exits non-zero on any finding, writes `report.json` and a full-page PNG per failing
route/viewport/theme under `.codex-goatos-render/admin-web-webview/<timestamp>/`. **Open the
PNGs.** A JSON summary is not visual proof; AGENTS.md already requires you to look.

### Updating the baseline (deliberately, never casually)

Known debt lives in `apps/admin-web/scripts/check-mobile-webview-waivers/mobile-webview-waivers.json`
as a list of waived finding keys. It exists so the guard is green on today's debt and red the
moment a NEW instance appears.

```bash
npm --prefix apps/admin-web run smoke:webview:update-baseline   # rewrites the waiver list
git diff apps/admin-web/scripts/check-mobile-webview-waivers/    # READ EVERY ADDED LINE
```

Rules:
- **Fixing the defect is the default. Waiving is the exception**, and the exception needs a
  sentence in the PR body saying which class it is and when it will be fixed.
- A waiver list that GREW in a UI change is a review finding. Growing the baseline to land a
  change is the same refused move as growing the phone-viewport ratchet.
- Never run `--update-baseline` from a machine whose backend is down: every route then renders
  the contract-unavailable state and you will waive an empty app.
- CI runs `--require-baseline`, so a missing baseline fails loudly instead of silently passing.

---

## The taxonomy: seven classes, the signature, and how to not write it

Ranked by how often it has come back. For each: what the user sees, the machine signature the
guard asserts, and the rule that prevents it.

### 1. Horizontal overflow at phone width (~14 recurrences)
User: the page pans sideways, cards cut at the right edge, or the whole page renders zoomed out.
Checks: `page-horizontal-scroll`, `viewport-shrink-to-fit`, `element-overflows-viewport`,
`table-not-scroll-contained`.
Signature: `documentElement.scrollWidth > 393`; `innerWidth` widening past device width
(Chrome/WebView shrink-to-fit — this is why measuring against `innerWidth` hides the bug);
any visible element whose rect escapes the viewport without a horizontally scrollable ancestor;
a `min-width` table with no `overflow-x:auto` owner, or an owner that spills past its card.
Rule: **every flex/grid child that can hold wide content gets `min-width: 0`.** A wide table or
chart may exceed the screen only inside its own `overflow-x:auto` wrapper that a thumb can pan,
and that wrapper stays inside the card. Never `overflow-x: hidden` on a page-level element.

### 2. Clipped / truncated text in controls, cells and labels (~11 recurrences)
User: `VERIFIE…`, half a shed name, a tag with its last letter shaved off.
Check: `clipped-text` (`scrollWidth > clientWidth + 2` or `scrollHeight > clientHeight + 8`
combined with `overflow: hidden|clip`, on `a, button, th, td, .tag, .lab, .nm, .chip, label`).
Rule: at phone width let text **wrap** (`white-space: normal`) instead of `nowrap + hidden`.
`text-overflow: ellipsis` is not a fix; if it is genuinely required, the element must carry the
full text in `title`/`aria-label` (which is the guard's only escape hatch).

### 3. Chart / axis labels missing, empty or fallback-marked (~8 recurrences)
User: `----` across the load-wise chart, vendor names gone, y-ticks clipped, legend money split
from its label. The class Codex was fixing on 2026-09-18.
Checks: `chart-label-count` (rendered `.gclab` count < `.gcol` count), `chart-label-empty-or-fallback`
(text empty, `/^[-–—.…]{2,}$/`, `NaN|undefined|null|Infinity`), `chart-label-invisible`,
`chart-label-clipped`, `chart-label-zero-width` (SVG `getBBox().width === 0`),
`chart-label-outside-viewbox`, `chart-empty-slot-visible` (a `.gcb` with `.gcbar.none` and no
`.gcval` that is not `visibility: hidden`), `chart-legend-clipped`.
Rule: an empty series slot is **hidden**, never painted as a dash. Gate the whole chart on
`hasAnyValue`. Axis labels wrap or rotate; they never ellipsis. SVG text stays inside the
`viewBox` with `svg { max-width: 100%; height: auto }`.

### 4. Tap targets under 44px (~6 recurrences)
User: sort links, chips, pager arrows and the search box cannot be hit with a thumb.
Checks: `tap-target-too-small` (any visible `a[href], button, [role=button], summary, select,
input, label[for], .chip` under 44x44 at phone width), `tap-targets-overlap` (centres < 8px
apart), `pagination-unreachable`.
Rule: `min-height: 44px; min-width: 44px` on every touchable control at `max-width: 620px`.
This is almost always a CSS-only fix in `app/mesha-theme.css` — there is no excuse for it.

### 5. Sidebar / drawer / overlay viewport and scroll traps (~5 recurrences)
User: the page is still there behind the mobile menu, a drawer cannot scroll to its last row,
a popover hangs off the right edge, a sticky header silently does nothing.
Checks: `overlay-offscreen`, `overlay-below-backdrop`, `overlay-not-clickable`
(`elementFromPoint` at the overlay centre lands elsewhere), `scroll-trap`
(`scrollHeight > clientHeight` inside `overflow: hidden`), `sticky-inside-overflow-hidden`,
`sticky-does-not-stick` (re-measured after a 600px scroll).
Rule: popovers and drawers are clamped to `width: min(<N>px, calc(100vw - 24px))`; the page
behind an open mobile menu is hidden, not merely covered; a scrollable drawer body uses
`overflow-y: auto`; `position: sticky` never lives inside an `overflow: hidden` ancestor.

### 6. `100vh` instead of `100dvh` (~3 recurrences, still open today)
User: the bottom row or CTA sits under the Android WebView chrome.
Checks: static `vh-instead-of-dvh` over `app/*.css` and `components/*.css` (a line that also
names `dvh`/`svh` is an accepted fallback pair); runtime `fixed-bottom-without-safe-area`.
Rule: full-height layout uses `100dvh` (with a `100vh` fallback on the same declaration), and
any `position: fixed` bottom element pads with `env(safe-area-inset-bottom)`.

### 7. Filter bars / form rows stacking wrong at phone width (~4 recurrences)
User: a `select` squeezed to 40px, a date input too short to tap, labels orphaned from controls.
Checks: `form-control-too-narrow` (< 96px at 393), `date-input-too-short` (< 38px tall).
Rule: filter bars `flex-wrap: wrap` with `min-width: 0` on each control and a sane `min-width`
floor; native date inputs keep their full height at phone width.

### Secondary (rare, wide blast radius)
`backdrop-filter` with no `@supports` fallback (a blank panel in older WebViews) and computed
font-size under 10px at phone width — both reported, both cheap to avoid.

---

## What a reviewer must refuse

- A UI change whose proof is a 1440px screenshot only.
- A UI change that adds a route/tab/drawer URL state without adding it to
  `scripts/smoke-visual-live.mjs` (this guard reads its route list from there, so the new route
  is covered automatically once the smoke list has it — and is silently uncovered until then).
- A grown waiver baseline with no explanation.
- Chart work with no phone-width screenshot showing every category label rendered.
- A new fixed px width >= 480 on a non-scrolling box (that one is `make admin-web-phone-viewport-guard`).

## Before you push a UI change — the checklist

1. `npm --prefix apps/admin-web run smoke:webview:static` — green.
2. Live app up, then `npm --prefix apps/admin-web run smoke:webview` — green, or every new
   finding fixed (not waived).
3. `npm --prefix apps/admin-web run smoke:visual:live` and `responsive:guard` — green.
4. Open the 393px PNGs for the routes you touched, in **both** themes, and confirm with your
   eyes: chart labels all present, tables panning inside their card, modals/drawers on top and
   tappable, pagination reachable, nothing clipped, no sideways page scroll.
5. Put the phone-width screenshot in the handoff next to the desktop one.

Canonical prose: `docs/decisions/admin-web-phone-viewport.md`, root `AGENTS.md`
("Admin-web must render on a PHONE"), `apps/admin-web/AGENTS.md` (visual QA scope).
