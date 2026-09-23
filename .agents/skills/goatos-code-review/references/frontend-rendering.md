# Frontend Rendering — navigation, paint order, and the hit area

Load this chapter when the diff touches in-app navigation controls (tabs, back
links, row-click targets, breadcrumbs), overlay/scrim/sticky CSS, or any
measurement of a UI element — `apps/admin-web/app/mesha-theme.css`,
`apps/admin-web/components/**`, `apps/admin-web/features/**/*.tsx`, or a
smoke/measurement script under `apps/admin-web/scripts/**`.

Sibling chapters: [`frontend.md`](./frontend.md) (contract, data access, state,
a11y) · [`mobile.md`](./mobile.md) (the Android twin) ·
[`verification-and-coverage.md`](./verification-and-coverage.md) (what makes the
measuring check trustworthy).

---

## 1. In-app navigation must not reload the document {#no-raw-anchor-internal-routes}

**Reject** a raw `<a href="/internal/route">` — including one whose `href` comes
from a local `href()` helper — used as an in-app navigation control. **Require**
`useRouter`/`Link`, or the local overlay controller when the target is a
same-page overlay.

A raw anchor to an internal route is a full document navigation: the browser
re-downloads the page, re-runs every Server Component, and discards client state.
The user paid a page load to switch a tab.

Instance (`origin/main`, 2026-09-23):
`apps/admin-web/features/health/health-config.tsx:120` is

```tsx
<a className={tab === "treatment" ? "btn" : "btn ghost"} href={href("treatment")}>
```

so every Treatment/Diagnosis tab switch re-downloads the page. That file holds five
such sites (`:120,123,312,454,459`), and the class is **12 uncorrected sites across
6 files**: the five above plus `leadership-tasks-page.tsx:287,419`,
`plan-console.tsx:132,289`, `row-drawer.tsx:161`, `health-register.tsx:125` and
`live-tracker-board.tsx:349`.

Re-derive that number rather than quoting it. Scan every `<a>` tag under
`apps/admin-web/**/*.tsx`, then subtract three legitimate shapes: external and
`target="_blank"` hrefs; the 9 anchors carrying `download` (a file download IS an
anchor); and the 10 carrying an `onClick` interceptor, which is the correct
progressive pattern — a real href for middle-click and a client-side navigation on
plain click. What is left is the class. Getting this wrong in either direction is
the point of `verification-and-coverage.md#comment-is-not-a-contract`: the first
version of this rule quoted a paraphrased tag that `git grep` returns zero hits for.

**`make admin-web-local-overlay-guard` is green on all 12, by construction.** Its
three regexes key on the literal tokens `veil`, `drawer`, `overlay`,
`shedDrawerHref` and `drawerPageHref`; these sites use `href("treatment")`,
`tasksAliasFixedHref`, `draftHref`, `scheduleHref`, `listHref` and
`editElsewhere.href`. The guard covers the overlay half of the rule and nothing
else — do not read its pass as coverage of this class.

Reviewer test: for every clickable control the diff adds or moves, ask which of
three things it is — a route change (`useRouter`/`Link`), a same-page overlay
(`LocalOverlayLink` + local controller, see `frontend.md` "State"), or an
external link (`<a>`, correct). A raw anchor in the first two categories is a
merge-blocking finding.

Scope note: this is a class, not a page. Sweep every tab, sub-tab, modal, drawer,
row action and inline editor — see
[`verification-and-coverage.md#example-is-never-the-scope`](./verification-and-coverage.md#example-is-never-the-scope).

## 2. `position:sticky` combined with `backdrop-filter` tears on mobile GPUs {#sticky-backdrop-filter}

**Reject** a new element that carries both `position:sticky` (or `fixed`) and
`backdrop-filter`. **Require** an opaque or `color-mix` background without the
filter for anything that stays pinned while content scrolls under it.

The filter forces the compositor to re-sample the scrolling content behind a
layer that is itself being repositioned every frame; on mobile GPUs the result
is a visible tear or smear during the scroll.

Three elements in `apps/admin-web/app/mesha-theme.css` carry the pair today:
`.top` (the app header), `.navback` (the in-app back/breadcrumb bar) and
`.lt-fbar` (the Tasks filter bar). The file already documents the hazard in the
Tasks mobile block — `backdrop-filter:none` there is called load-bearing — so
treat those three as the known baseline and reject growth, not as permission.

## 3. An opaque overlay that shows the content behind it mid-transition is a paint-ordering bug {#overlay-paint-order}

**Reject** an overlay whose stacking context is created (or dropped) by the
animation itself — a `transform`, `filter`, `opacity` or `will-change` on the
panel, or a panel and its dimmer only one `z-index` step apart. **Require** the
opaque panel hold its own stacking context for the whole transition, and sit
clearly above the dimmer.

Symptom to look for in the filmstrip: the panel is opaque in the first and last
frame, and for ~0.2s in between the page is visible through it.

Instance: the Tasks filter panel (`.lt-page .lt-fsheet-host .lt-fgroup.open`,
`z-index:151`, `background:var(--panel)`) stops painting for about 0.2s while the
page shows through the 42% dimmer at `z-index:150`.

Evidence rule: a still cannot show this. Flicker findings need a GIF or a
filmstrip (contract §7).

## 4. Measure the effective hit area, not the element rect {#effective-hit-area}

**Reject** a target-size finding measured with `getBoundingClientRect()` on the
inner element. **Require** the measurement take the whole interactive target —
the element whose click handler fires, including the wrapper's padding, any
`::before`/`::after` expander, and `label`-for association.

Instance: a search input measured 196x18 and was reported "too small to tap". The
padding is on the wrapper; the real target is fine. A false positive here is
expensive twice — it wastes the fix, and the next real one gets ignored.

Reviewer test: from the measured node, walk up to the nearest element with a
click/`label` binding and measure that. If the script cannot decide, it reports
`not-measured`, never a size —
[`verification-and-coverage.md#no-unearned-verdict`](./verification-and-coverage.md#no-unearned-verdict).

## 5. Deep-linking never exercises L2 or L3 {#deep-link-never-exercises-l2}

**Reject** a UI sweep that reaches every route by URL and reports the surface
covered. **Require** the journey navigate the way a person does for anything that
exists only after an interaction.

Instance: `.navback` is rendered only after tapping a row, so a sweep that
deep-linked every route had never once seen it — and could not, by construction.
Full statement:
[`verification-and-coverage.md#deep-link-never-exercises-l2`](./verification-and-coverage.md#deep-link-never-exercises-l2).
