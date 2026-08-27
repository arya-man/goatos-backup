# Page-grain access retires the admin-web role lenses

Maintainer decision, 2026-08-27. Supersedes the MECHANISM of the 2026-08-21
procurement-director workspace decision; its OUTCOME is preserved byte for byte.

## What was wrong

Two different layers decided what a person saw on admin-web, and they disagreed.

1. **Permissions.** What the person is allowed to do.
2. **Lenses.** Hand-written Go that deleted nav leaves and page contracts for a
   named ROLE, regardless of permissions.

The Procurement Director holds `feed_config.read/write`, `operators.*`,
`roster.*` and `verification.act` through the `feed_director` role he also
wears. His sidebar showed none of it, because
`adminui/app/procurement_director_lens.go` kept only the Procurement and Feed
groups and explicitly hid `/feed/config`.

Both layers were correct about their own question. Together they meant a screen
reading the permission layer — the new People access editor — would show modules
the person could not reach, and every future "this person should not see that
page" was a code change.

## The decision

**A person's admin-web sidebar is exactly the pages ticked for them.** One layer.

- The module tick says whether they touch the module at all.
- Page ticks inside it say which of its screens they open.
- `procurement_director_lens.go` is DELETED. Its narrowing was written onto the
  holder's own rows by the backfill, once, as data.

**The verifier lens STAYS** (maintainer instruction, same day). It is not the
same kind of thing: it does not subtract from the ordinary console, it composes
a DIFFERENT workspace — a queue, its own registry-built modules, its own
landing. Retiring it would delete a product surface, not a narrowing.

**The phone does not change.** Android composes its bar from the mobile module
registry; a page tick is web-only and never reaches it.

## What the model is

`permissions/capability_pages.go` holds the catalog: every admin-web nav leaf,
its module, its sidebar label, its route. Three properties:

1. **Pages are web-only.**
2. **An empty page list means EVERY page of that module.** This is what makes a
   page shipped tomorrow reach whoever already holds the module, rather than
   silently reaching nobody until someone re-ticks thirty people. Narrowing is
   opt-in.
3. **The catalog is asserted against the real navigation.**
   `TestEveryNavLeafIsATickablePage` fails if a leaf ships without a row here
   (it would be unwithholdable) and `TestEveryPageContractRouteIsOwnedByAModule`
   fails if a page contract's route belongs to no module (it could never be
   narrowed).

Storage is `person_module_access.pages` (migration `000216`). Resolution is
`permissions.PageAccessForAssignments`, read by BOTH the bootstrap narrowing and
the access editor, so the ticks the editor shows are the ticks the sidebar obeys.

## Two fail-open cases, deliberately

A person with **no stored rows** is not narrowed at all — they are still on the
retired role path, and narrowing them to nothing would lock out anyone the
backfill has not reached. A **source error** is likewise not a narrowing: it is
logged and the full contract is served. The sidebar is a convenience; the route
behind every page is independently permission-gated, and the 403 is the lockout.

## Two refusals on the write path

- A page key belonging to another module, or to no module, is REJECTED. A
  dropped tick reads on screen as granted while granting nothing.
- A granted module that HAS screens with NONE ticked is REJECTED. It would
  resolve to every page (property 2), which is the opposite of what the admin
  just did on screen.

## Proof

`TestRetiredProcurementDirectorLensIsReproducedByTicks` composes the holder's
backfilled ticks — both roles stacked, as he really holds them — and asserts the
sidebar is exactly the six leaves his live bootstrap served on 2026-08-27:
Source Entry, Vendors, Sales, Feed Purchases, Feed Analytics, Feed SOP; that
`/feed/config`, `/people`, `/verify` and `/` carry no page contract; and that
the kept leaves still do. `TestCeoIsNeverNarrowed` pins the leadership exemption
both retired lenses carried.

Verified live on the local stack the same day: his bootstrap before and after
the retirement is identical, ticking Feed Config in the editor made the leaf and
its page contract appear with no code change, and unticking it removed them.
