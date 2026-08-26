# Sales vertical and the Economics page

Maintainer decisions, 2026-08-25.

## 1. Sales is its own vertical

Sales moved OUT of the Procurement nav group into its own **Sales** group:
selling is not buying. This is a **nav regrouping, not a route change** — the
Sales page keeps its `/procurement/sales` href, exactly as the Milk split kept
`/counts/milk-preparation` (see the nav comment in
`backend/internal/adminui/app/service.go`). Deep links, the page contract's
route id, and every existing test keep working; only the sidebar group and the
breadcrumb (`crumb: "Sales"`) changed.

The Sales group holds two leaves: **Sales** (the deals board, gated `SalesRead`
as before) and **Economics** (`/sales/economics`, new).

## 2. The Economics page is the core-of-the-business read

`/sales/economics` lays the three sides of the business against each other, per
animal: what a day of feed costs (the priced feed sheet), what a day of growth
returns (weighing pairs priced at the realized ₹/kg), and what a kg actually
sells for (the sales ledger). One backend read serves it:
`GET /economics/overview`.

### Leadership-only

The page carries feed spend, sale values and margins side by side, so it is
gated on the **dedicated `sales.economics.read`**, granted to `ceo_internal`
ALONE. It is deliberately NOT a reuse of `SalesRead` (held by the sales and
procurement directors) and NOT granted to `growth_director` — each would see
the other's money numbers. Widening it to a director later is a one-line grant
change. Pinned by `TestSalesEconomicsNavLeafIsLeadershipOnly` and
`TestEconomicsGateIsTheDedicatedPermission`.

### Its own read-only module, in the Growth Director's shape

`backend/internal/economics` is a READ-ONLY reporting module outside weighing,
sales, feeddirection and identity, mirroring `backend/internal/growthdirector`
— the recorded precedent for consuming weighing tables without touching the
weighing isolation boundary (`docs/weighing/growth-director-weights-widgets.md`
→ "Architecture decision — why a new module"). It gates nothing, writes
nothing, and needs no new guard exemptions:

- **Weighing isolation**: untouched — zero diff under
  `backend/internal/weighing/**`; tags resolve through
  `goat_identifiers.normalized_value = upper(tag_key)` with the one-hop merge
  redirect, the same join growthdirector uses. A tag that resolves to nothing
  simply stays out of this report (the pulse discloses the denominators).
- **Sales lock (000173)**: untouched — sales still reads no herd table, and
  this module reads `sales_deals` at DEAL grain only, never joined to a herd
  table. (An earlier draft reached per-animal through the identity-owned
  `goat_sale_allocations` mapping; with the sold panel removed that read is
  gone entirely.)

### Estimate semantics (`estimate: true`, always)

Every rupee figure is a disclosed estimate:

- **Feed cost** is what the sheet DIRECTED, priced at the latest same-park
  purchase load on or before the feed day (the same LATERAL the Feed Analytics
  expenditure read uses) — never a measured consumption. Blocked cells (NULL)
  are never coalesced to zero; an item with no purchase on record prices
  NOTHING and is counted in `pulse.unpriced_feed_items` instead of invented.
  Experiment pens ARE priced per head by dividing the pen's authored total by
  its recorded cohort size — dividing a real total by a real count is honest;
  what stays banned is MULTIPLYING grams by an informational head count.
- **Per-animal feed cost** is the animal's own (shed, pen, stage tag, breed)
  grain cell. A mixed pen's sheet row carries `'_+_'`-joined composite keys, so
  the match is SET MEMBERSHIP, collapsed with avg() to keep one row per animal
  (`TestEconomicsOneToManyFeedCellsCollapsePerAnimal`).
- **There is no per-animal sale figure.** A sold panel splitting the deal value
  evenly across its animals was built and then REMOVED (maintainer decision,
  same day): no per-animal price is recorded anywhere, so an even split is a
  number the farm never negotiated, shown at a grain it was never agreed at.
  The honest sale figure is the deal-grain realized price per kg on the pulse,
  and the Sales page owns the deals themselves. The module no longer reads
  `goat_sale_allocations` at all.
- **Deal figures are tenant-wide** (realized price, sold revenue): the sales
  ledger records a farm label, not a park id, so the park filter narrows animal
  and feed figures only (`TestEconomicsParkScopeNarrowsAnimalsNotDeals`).
- **Realized price basis** is disclosed: the window's own closed weighed deals,
  falling back to the trailing 365 days, falling back to null — never a made-up
  price.

### Two figures a reader will subtract must cover one set

Maintainer decision 2026-08-26, correcting a shipped defect. The feed tile used
to be the WHOLE FARM (1,649 animals) while the value tile covered only animals
weighed twice (309). Side by side they invited a subtraction that read as "the
farm loses ₹39,000 a day", when the truth was "most of the herd has not been
weighed". Both tiles now range over the PRICED set — paired animals whose
ration cell resolved and priced — and `net_per_day_rupees` IS that subtraction
(on the live herd: ₹10,614 feed, ₹14,804 value, +₹4,190 net over 309 animals).

The sharp edge, and the thing a future change must not "tidy": a priced animal
that did NOT measurably grow stays in the feed figure at full cost and adds
ZERO value. Filtering the feed side to growers only would flatter the farm by
hiding what non-growing animals eat. That is the mutation
`TestEconomicsFeedAndValueTilesCoverTheSameAnimals` catches.

The whole-farm number is still published as `farm_feed_cost_per_day_rupees`,
because "what is feed costing us" is a real question — but it is rendered on its
own line with its population (`farm_animals`) and its true denominator
(`farm_feed_days` — a 90-day window held only 18 sheets) named, and with copy
telling the reader not to subtract it.

### The scale-noise floor is load-bearing here

A weight change within 3% of starting body weight is scored FLAT (0 g/day) —
the same rule and threshold as `growthdirector`'s slow-growth read. It matters
more on this page than anywhere else because this module DIVIDES BY the gain:
on the live herd 21% of pairs (68 of 326) sit inside that band, and without the
floor they rendered "₹10,149 per kg of gain", which reads as precision and is
scale drift. A flat animal KEEPS its row (its feed cost is real) but carries no
cost-per-kg, no value-added and the Watch verdict — a gain that was not
measured cannot be priced. Pinned and mutation-tested by
`TestEconomicsSubNoiseGainScoresFlatAndIsNeverPriced`.

### One animal is one animal: tags resolve BEFORE pairing

Maintainer correction 2026-08-26, found because a pen rendered "18 measured of
17 held" — an impossibility on screen. Nearly every goat carries BOTH
`animal_identifier_1` and `animal_identifier_2`, and the chain paired weighs by
TAG, which broke in both directions: an animal weighed under both tags became
TWO animals (310 rows for 296 animals, its cost and value counted twice), and an
animal whose two weighs happened to land on different tags VANISHED, because
neither tag alone had a pair.

Every scan is now resolved to its goat in `animal_obs` before any pairing, so
the pairing grain is the ANIMAL. The correction moved real answers: Osmanabadi
flipped from −₹0.6 to +₹0.9 per head per day, Malai from −₹8.1 to −₹2.0. The
"measured of held" pair is now also a permanent check on this class of defect —
measured can never exceed the herd, and a test asserts it. Weighing itself stays
free-flow and tag-grained; THIS module is the one resolving tags to animals, and
the pulse keeps reporting the raw scanned-tag denominator beside it. Pinned and
mutation-tested by `TestEconomicsResolvesTagsToOneAnimalBeforePairing`.

### Measured is not the herd

Both counts are published on every shed and breed row and rendered "121 of 846".
Every figure on a row is computed from animals weighed twice AND priced, but a
bare count beside a breed name reads as "how many of this breed do we have" —
the herd holds 846 Anantapur Sheep and only 121 qualified, and a reader taking
the first number for the second concludes the farm shrank by 85%. Pinned by
`TestEconomicsPublishesHerdCountBesideMeasuredCount`.

### The grain is the PEN and the BREED, not the animal

Maintainer decision 2026-08-26. A per-animal list is hundreds of rows nobody
acts on; a pen and a breed are things the farm can change. The page therefore
shows **shed by shed** (worst daily net first — the pens costing money lead) and
**breed by breed** (best net first — which breed pays for its feed).

Every group figure is PER HEAD PER DAY, never a group total: only the weighed
animals of a pen are in scope, so a total would understate a pen where few
animals were weighed, while a per-head figure compares honestly across pens of
any size. The figures are means and are consistent with each other — the value
figure is the shown gain priced — and an animal scored flat by the noise floor
is INSIDE the mean at zero gain, because a pen that is not growing must read as
not growing.

The breed chart encodes the actual question — *how much of what this breed
returns does its feed eat* — as one bar per breed: the bar is the feed cost, a
marker line is the return, and a red bar past its line is a breed losing money
every day it stays. The shared `SvgColumnBars` was tried first and rejected: it
draws unlabelled columns (right for a day series, where position is the label,
wrong for six breeds that need their names) and puts float values in an SVG
`<title>`, which hydration-mismatches. `features/sales/breed-economics-bars.tsx`
follows the same rules as the shared primitives — server component, CSS custom
properties, no copy of its own.

### The whole-farm figure is the LATEST DAY, and says what it could not price

Maintainer correction 2026-08-26, after the reported figure (~₹69k) did not match
the page (₹54k). Two causes, both real:

1. **An average across a window where spend doubled describes no real day.**
   Daily feed spend ran ₹27,648 → ₹64,676 across the 18 sheet days of a 90-day
   window. The page now reports the LATEST sheet day with its date named.
2. **Unpriced feed was silently dropped.** Two concentrates (Vijay, RGS) have no
   purchase rows at all, so ~86 kg a day sat outside the money and the total was
   quietly short. The kg is now reported beside the figure. It is never estimated
   at another item's rate — inventing a price would make the total look complete
   when it is not. Recording those purchases is what closes the remaining gap.

### Grain and caps

The shed and breed tables are capped at `MaxGroupRows` as a backstop (both are
naturally bounded); every pulse and band figure is a whole-filter aggregate
computed independently of that grouping and cap, so neither ever bends a
headline number (`TestEconomicsPaginationCapNeverBendsSummaries`). Status boundaries
(rework weighs, non-closed deals, blocked cells) are pinned by
`TestEconomicsStatusMatrixReworkDealsAndBlockedFeed`.
