# Admin-web must render on a phone

> Status: **enforced** (maintainer rule 2026-09-14) · Owner: admin-web
> Static guard: `make admin-web-phone-viewport-guard`
> (`tools/agent-hooks/check-admin-web-phone-viewport.mjs`, in the admin-web job of
> `make ci-local`). Runtime proof: the 390px mobile lane of
> `npm --prefix apps/admin-web run smoke:visual:live`.

## The rule

The admin-web dashboard (`https://dashboard.mesha.sg`) is opened on phones — a director
on the farm, the CEO between meetings, a park head who has the app but wants a chart.
Every browser-visible admin-web surface therefore has TWO viewports it must be correct
on, not one:

| Viewport | Width | What must hold |
|---|---|---|
| laptop | 1440px | the mock (`mock/goatos-dashboard-mock.html`), pixel for pixel |
| phone | 390px | nothing clipped, nothing broken, the page body never scrolls sideways |

"Nothing clipped" means every card, chart, table, KPI strip, drawer, modal and control is
reachable and readable. A TABLE or a CHART may be wider than the screen, but only inside its
own `overflow-x: auto` wrapper that a thumb can pan; the PAGE must not be the thing that
scrolls. Text truncates with an ellipsis or wraps; it does not run off the edge. A drawer or
modal fits the viewport it opens in.

This is a rule for **every agent and every developer** — Claude, Codex, Cursor, humans —
and it is part of what "done" means for UI work, alongside the existing visual-regression
and click-through E2E proof: after the final edit, the changed route is reproduced at
**both** widths in Chrome, and the phone-width screenshot goes into the handoff next to
the desktop one. A change proven only at laptop width is not proven.

## Two halves, and why both exist

**The static guard** (`check-admin-web-phone-viewport.mjs`) catches the CSS/JSX shapes that
break a phone layout before a browser ever opens, so `make ci-local` refuses them with no
stack running:

- `fixed-px-width` — `width` / `min-width` / `flex-basis` ≥ 480px (CSS, or a JSX `style`
  object) on a box that is not a table, svg, canvas, pre, img, video, iframe, or a named
  scroll container. A 480px box in a 390px viewport clips or drags the page sideways.
- `px-grid-sum` — a `grid-template-columns` whose px **floor** (plain px tracks plus the
  minimum of every `minmax()`/`clamp()`, times a numeric `repeat()`) exceeds 360px. A grid
  can never lay out narrower than its floor.
- `page-overflow-hidden` — `overflow-x: hidden` on `html`, `body`, `.main`, `.screen`,
  `.wrap` or `.page`. That hides the sideways scroll instead of fixing the box that caused
  it; the clipped content is simply gone on a phone.

A declaration is not flagged when it sits inside `@media (min-width: …)` (it never applies
on a phone) or when the same file overrides it for the same selector inside a
`@media (max-width: ≤640px)` block — that is what responsive CSS looks like, and it is the
fix the guard asks for. `phone-viewport:ignore: <reason>` on the line (or the line above)
is the reviewer-facing escape hatch for a print sheet or a canvas that owns its own pan.

Pre-existing debt is frozen per `(file, rule) → count` in
`tools/admin-web-phone-viewport/baseline.json` and the guard is shrink-only in both
directions, exactly like the exception/telemetry ratchets
(`docs/observability/GUARDRAIL_RATCHET.md`): a file fails the moment its count for a rule
grows, and a count below the baseline also fails so fixed debt is taken off the books in the
same change (`make admin-web-phone-viewport-baseline-update`). Growing the baseline to land a
new finding is not an accepted way to land code. `make admin-web-phone-viewport-guard-list`
prints every current finding with its line.

**The runtime lane** is what actually sees a phone. `smoke:visual:live` visits every live
sidebar leaf in Chrome at 1440px and again at 390px with iPhone emulation, and at the phone
width it fails on real horizontal overflow of the document, on a wide table that a touch pan
cannot scroll, on text that leaves its cell without an ellipsis, and on controls smaller
than a thumb. The static guard cannot see a width that only appears with real data, a
Tailwind `w-[600px]`, a `white-space: nowrap` on a container, or a chart whose SVG is sized
by its series; the browser can. Neither half replaces the other.

## What the static guard cannot see (stated so a green run is never mistaken for phone proof)

- Utility-class widths (`w-[600px]`) and widths computed at runtime.
- `white-space: nowrap` on a page-level container.
- Overflow that only appears with real data (a long pen name, a 12-column matrix).
- Whether the `overflow-x: auto` wrapper a wide table needs actually exists.

Those are the runtime lane's job, and they are why the phone-width screenshot is part of
the handoff for every UI change.

## History

The existing visual smoke already carried a 390px lane with overflow and truncation
assertions, but nothing in the always-loaded agent context said the phone lane was
mandatory, and nothing in `make ci-local` failed a fixed-width box without a browser. The
maintainer's instruction on 2026-09-14 — "whenever we do UI, graph or anything, it should be
visible for phone also; it should not get cut, UI should not be broken" — is now both the
AGENTS.md rule and the CI guard above.
