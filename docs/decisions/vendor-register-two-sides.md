# The vendor register has two sides, and Sales is its own phone module

**Maintainer decision, 2026-09-05.** Two changes, decided together.

> "Under sales module also have one more tab as vendors, and change these vendor categories to
> there — sales categories, not under vendors in procurement. When it comes to app, create one more
> module as sales, same access as who has procurement now. Change the sales tab from procurement to
> sales module and add a vendors tab there. Categories are Agents, Butchers, Farmer, Slaughter
> House, Company."

---

## 1. One register, two sides

`000217` added five buyer-side record types — **Agent, Butcher, Company, Farmer, Slaughter
House** — to the one shared vendor register, on the reasoning that *every buyer is a vendor* and one
table can hold both. That is still true of the STORAGE. It turned out to be false of the SCREEN.

A person running the buying desk opened Procurement > Vendors and was offered five categories that
have nothing to do with buying. A person recording a sale who had just met a new butcher had to open
the buying desk's page to add him, and pick his category out of forty that were mostly building
trades. The register was one list answering two different questions.

So the **vocabulary** splits, not the table:

```text
Procurement > Vendors     record types on the 'procurement' side   (the supply desk's 35)
Sales > Vendors           record types on the 'sales' side         (the five above)
```

Same table, same endpoint, same drawer, same form, same copy map — one component renders both. Only
which record types each carries differs.

### Where the side lives, and why not anywhere else

`procurement_vendor_catalog.register_side` (migration `000256`). **Not** a list in Go: business-
managed dropdown vocabularies come from Postgres, which is the same reason `000156` put record types
in this table instead of a CHECK. A hardcoded Go list of five would mean the farm can add a sixth
sales category as data and then find it silently filed under Procurement.

**Not** a column on the vendor row either. A vendor's side is not an independent fact about the
vendor; it is implied entirely by its record type. Storing it twice would let the two disagree, and
every re-categorisation would need a backfill.

### The two sides are complementary, and that is the load-bearing property

Sales is the record types marked `sales`; procurement is **everything else**. Every vendor is on
exactly one side and **none is on neither**.

The failure mode guarded against is a predicate that requires a catalog row to exist at all.
`domain.Validate` deliberately does not check `record_type` against the catalog, so an
**uncatalogued** record type is a real state — and under such a predicate that vendor would vanish
from both pages and be unfindable. Mutation-tested: dropping the `NOT` from the procurement branch
makes the uncatalogued vendor disappear and turns the test red.

The predicate is spelled `NOT EXISTS` rather than `NOT IN`. That is a robustness preference and
**not** a live bug guard — the subquery projects `value`, which is `NOT NULL` inside the catalog's
primary key, so the `NOT IN` null-swallowing trap cannot fire here. Confirmed by mutation: the
`IN`/`NOT IN` form keeps the test green. `NOT EXISTS` is kept because it stays correct if the
subquery is ever widened to project something nullable.

An uncatalogued type therefore falls to **procurement**, which is where it is listed today. Failing
toward the status quo is the choice; the alternative is a vendor nobody can find.

### Absent means both, unknown is refused

An absent `side` reads the WHOLE register, because that is what every reader meant before the split
and what the vendor picklist — which names the buyer of a sale — still needs. An **unrecognised**
side is refused `vendor_side_unknown`, never widened to both: a page that asked for one half and
silently received both would put buyers back on the buying desk's screen, and nothing on the page
would say so.

Both pages therefore name their side **explicitly** in the page contract's table `data_source`
(`/procurement/vendors?side=sales`), rather than relying on the default. The renderer reads the side
back out of its own contract instead of taking it as a prop, so there is one statement of the fact
and no way for the two to disagree.

### What is NOT narrowed

Only the record types. Breed, state, city, status, feed, capacity unit and supply frequency stay
whole on both sides — a butcher and a feed stockist sit in the same states and are reached in the
same towns, and the city facet is derived from the vendors that exist, which has no side at all.

The narrowing happens on the **server**. The full list never reaches the browser, so the Add-vendor
form physically cannot offer a category the page does not own.

## 2. Sales is its own phone module

Selling gets its own phone module, the way it got its own web vertical on 2026-08-27.

```text
Procurement (key `vendors`)        Sales (key `sales`)
  Vendors        /vendors            Sales     /sales           (MOVED from /vendors/sales)
  Feed Purchases /vendors/…          Vendors   /sales/vendors   (new)
```

The Sales tab **moved**; it is not duplicated. A tab living in both modules would be two doors onto
one ledger, which is what splitting the desks was meant to end. Procurement keeps its own Vendors
tab — the buying desk still adds suppliers. One table, two sides, one tab each.

**Who.** Offered on `sales.read`, held today by exactly the principals who hold
`procurement.vendor.read`: `ceo_internal`, `procurement_director`, `procurement_manager` — the
maintainer's "same access as procurement has now". It is keyed on the SALES permission rather than
the vendor one because this is the Sales module: if the two sets ever diverge, a sales reader should
get the module, and the Vendors tab inside it refuses them on its own permission rather than the
whole module vanishing.

**The module and its Vendors tab answer to different permissions, deliberately.** The module is
offered on `sales.read`; the Vendors tab is gated on `procurement.vendor.read`, because it renders
vendor rows, contact numbers and a drawer that reaches the payment instruments
`VendorFinanceRead` guards. Gating it on `sales.read` would hand the register to a sales reader who
was deliberately never given it, through a second door. The same applies to the web leaf: it ticks
with the `sales` module and is reached on `VendorRead` — module ownership and authority are separate
questions, exactly as Feed SOP is grouped under Feed and opened on `sop.read`.

**`sales` gained `SurfaceMobile`, so it needed a tick backfill.** A person's phone modules are their
ticks, and a tick NARROWS an offer — it never widens one. Everyone already backfilled carries a web
`sales` tick and no mobile one, so the new module would be narrowed away on every existing phone
until an admin re-ticked thirty people. Migration `000257` copies each person's web `sales`
capabilities onto a mobile row, once, with a ledger table so Down removes exactly those rows. This is
the identical shape and reasoning as `000251` did for Vendors.

## Pinned by

`TestVendorSideIsRefusedRatherThanWidened`, `TestVendorFilterNormalizeDropsAnUnknownSide`,
`TestCatalogSideNarrowsRecordTypesAndNothingElse`, `TestAnUnknownSideIsRefusedOnBothReads`,
`TestTheTwoRegisterSidesPartitionTheWholeRegister` (Postgres),
`TestCatalogNarrowsOnlyTheRecordTypes` (Postgres),
`TestSalesVendorsIsTheSameRegisterNarrowedToItsSellingHalf`,
`TestSalesVendorsIsGatedOnTheRegistersOwnPermission`,
`TestSalesIsItsOwnPhoneModuleCarryingItsOwnVendorsTab`, and
`TestSalesModuleAndItsVendorsTabAnswerToDifferentPermissions`.

Each was mutation-tested when written: dropping `?side=sales` from the contract, gating the leaf on
`SalesRead`, re-adding the Sales tab to the Procurement module, gating the phone Vendors tab on
`SalesRead`, and returning `("", true)` for an unknown side each turn one red.

## Two fixtures this change had to correct, and why they were wrong rather than unlucky

- `capability_parity_test.go` used **`sales` on mobile** as its example of "a module absent from
  this surface". The moment Sales became a phone module that fixture stopped testing the rule and
  started testing a real grant. It now names `sale_allocation`, which is still web-only.
- `vendors_module_offer_test.go` asserted **three** nav items. It now asserts two, exactly rather
  than as a minimum, so re-adding a Sales tab under any key or label fails.
