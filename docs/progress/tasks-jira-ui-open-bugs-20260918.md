# Jira-like tasks — open UI bugs found by the CEO (2026-09-18)

Found by the maintainer clicking the real page, after my own verification passed.
**My verification was inadequate and this is the reason: I checked that pages
RENDER and never clicked every interactive element.** Screenshots of a correct
layout are not proof that a control works. The repo's own rule says visual
regression AND click-through E2E are both required and that one is not the other;
I delivered the first and reported it as if it were both.

## B1 — the `+5` overflow chip is dead decoration

`features/leadership-tasks/leadership-tasks-board.tsx:227` renders it as
`<span className="ltb-person ltb-person-rest" aria-hidden="true">`. It is not a
link, has no handler, and is hidden from assistive technology. With 9 assignable
people and 6 avatars shown, five people are simply unreachable from the board's
person filter — clicking the chip does nothing, which is exactly what the
maintainer hit.

It also cannot be fixed by making the chip a link: there is nowhere for it to go.
The person filter needs a way to reach every person, not a sixth avatar.

## B2 — there is no way to SEARCH for a person

The board's filter is an overlapping avatar group showing initials only, which at
this tenant renders as `D D M D M A` — two D's, two M's, indistinguishable. The
toolbar's Assignee/Raised-by controls are native `<select>`s. Neither offers
typing a name. For a CXO filtering 424 tasks across 9+ people this is the primary
affordance and it is unusable.

## B3 — "Show only this" means nothing to a reader

It sits under each board column header. What it actually does is apply
`filter=<status>` so the keyset pager can page through that single status — it
exists because a column cannot show a status's whole 183 rows. The wording
describes the implementation, not the intent, and the maintainer read it as
belonging to the card.

## B4 — the board with a status filter shows three empty columns

With `filter=done` the row query returns only done tasks, so Open and Doing
render "0 on this page" while their header pills still read 183 and 118 — the
true whole-list totals. Each number is individually correct and the combination
is nonsense: a board that looks empty above counts that say otherwise. A status
filter and a status-column board are two ways of expressing the same thing and
should not both be active.

## B5 — MY ERROR, recorded because it wasted the maintainer's time

The view parameter is `t_view` (`features/leadership-tasks/params.ts:46`). I
opened `...&view=list` in Chrome for the maintainer, which is silently ignored,
so they got the board with a stale `filter=done` and concluded switching views
was broken. The product's own Board/List toggle writes the correct param. Worth
deciding whether an unknown `view` param should be tolerated, but the immediate
fault was mine.

## What has to happen

A click-through E2E that enumerates EVERY interactive element on this page —
every chip, avatar, select, disclosure, pager control, column link, card, tab,
toggle, modal button and composer — clicks each one, and asserts the URL, the
rendered result and the absence of a console error. Not a screenshot sweep. Then
the same at phone width for everything a thumb would touch.

Until that exists, this page is not verified, whatever the guards say.
