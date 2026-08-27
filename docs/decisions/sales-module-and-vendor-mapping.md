# Sales is its own module, and every sale names a vendor

**Maintainer decision, 2026-08-27.** Two changes, decided together.

> "Now make sales a standalone module... keep a separate modular sales, in that the sales page will
> be there. And whenever I'm recording any sale, I need to map it to one vendor. We don't do random
> sales. We do sales of anything to the vendor."

---

## 1. Sales split out of Procurement

Sales was a leaf inside the **Procurement** nav group, beside Source Entry, Vendors and Feed
Purchases. It is now its **own top-level vertical** with one page.

```text
Procurement          Sales
  Source Entry   ->    Sales      /sales
  Vendors
  Feed Purchases
```

**Nothing about the page's behaviour changed.** Same contract, same tables, same overview, same
pipeline and evidence panels, same permissions. This is a nav regrouping, the same shape as the
2026-07-31 Milk split out of Counts.

**Why it is a vertical and not a Procurement module.** Procurement is the BUYING desk. Selling is
not one of its workflows, and the codebase already said so before the nav did: Sales owns its own
permissions (`sales.read` / `sales.write`, recorded in `permissions.go` as deliberately NOT a reuse
of `ProcurementRead` or `VendorRead`), its own tables (`000173_sales_ledger.sql`), and its own
backend module (`backend/internal/sales`).

**The route MOVED**, unlike the Milk split which kept `/counts/milk-preparation`. Sales is no longer
inside Procurement in any sense, so a URL saying it would have been the last thing still claiming
otherwise. `/procurement/sales` is gone and there is **no redirect** — the surface is reached from
the sidebar and had no external links.

Two things that had to move with it, because neither is derivable from the route:

- The `banknote` icon token must be registered in admin-web's `iconByToken` map. An unregistered
  token does not fail — the group silently renders the **Control Tower** icon, which is the defect
  `milk` and `wheat` each shipped with. (`scale`, Weighing's token, was found missing in the same
  pass and registered too.)
- The **procurement-director lens** filters the contract by route prefix. That workspace has always
  carried Sales, because that role is the one non-founder holder of `sales.write`; splitting the
  group out from under the `/procurement` prefix would have silently removed his screen. The lens
  keeps the `sales` group and the `/sales` prefix explicitly.

## 2. Every sale is made TO a vendor

The record-sale drawer now requires the buyer to be picked from the **procurement vendor register**.
Free-text spellings of a buyer name were producing several buyers out of one counterparty on the
buyer board.

### The lock this crosses, and how it is kept

`000173_sales_ledger.sql` states: *"Sales owns its own tables and reads NOTHING from the herd,
vaccination, or procurement schemas."* A vendor link crosses that in words. It does not cross it in
fact, because it follows the shape `000177_goat_sale_allocations.sql` already settled for the same
problem in the other direction:

- `sales_deals.buyer_vendor_id` is an **opaque uuid**, deliberately **not a foreign key**. A real FK
  would let a procurement migration block a sale — the coupling the lock exists to prevent.
- The **vendor is picked in admin-web**, which reads the register through its own
  `/procurement/vendors` surface under `VendorRead`. The sales module still reads no procurement
  table; it stores the id it was handed.
- Only the **shape** of the id is validated server-side (`uuid.Parse`). That it names a real vendor
  is the caller's guarantee, exactly as `000177` validates its `sales_deal_id` against the deal read
  rather than by constraint.

### `buyer_name` stays, and stays editable

The vendor selection **prefills** buyer name and place; both remain editable. Two reasons, and the
first is the load-bearing one:

- `buyer_name` is a **snapshot** of what the buyer was called at the moment of sale. A vendor row
  gets renamed, merged, re-contacted; an old sale must not silently re-describe itself. Same reason
  `000177` snapshots the animal's location instead of reading it back off the goat.
- A sale is sometimes made under a name that differs from the register's spelling, and the ledger
  and buyer board have always shown `buyer_name`.

The vendor id is the durable link. The two text fields are the snapshot.

### Required on the write path, nullable in storage

The column is **nullable**, because the 2026-08-17 sheet import predates the register and has no
vendor — requiring one in the DB would either reject real history or invent a counterparty. The
requirement lives in `sales/domain.DealWrite.Validate`, so every deal **recorded in the app** must
carry one.

### The picklist is its own read

`GET /procurement/vendor-options` serves the register as a bounded picklist. Deliberately not a mode
of `GET /procurement/vendors`:

- **Active-only.** An inactive, negotiating or banned counterparty must not be offerable as the
  buyer of a NEW sale. A vendor deactivated later keeps its existing sales intact.
- **Unpaged.** The list endpoint caps at 100 rows per page and the register holds 307. A dropdown
  that stops at page one silently hides buyers, and draining pages from SSR is the exact full-walk
  shape `make admin-web-request-reads-guard` bans. This is ONE bounded query; a register larger
  than the cap reports `truncated: true` rather than presenting a partial list as complete.
- **Five columns.** Never the payment instruments `procurement.vendor.finance.read` guards, so a
  picklist can never become a side channel around that split.

Gated on `VendorRead`, not `SalesRead` — it is a procurement read whichever screen asks for it.
**Every role that can record a sale today already holds both** (`procurement_director`,
`ceo_internal`). A future sales-only role must be granted `VendorRead` or it will be unable to
record a sale at all.

### The dead end has an exit

A required field a person may be unable to fill needs somewhere to go. The drawer distinguishes
three states, which are NOT the same fact:

| state | what happened | what it shows |
|---|---|---|
| unavailable | the register could not be read | "The vendor register could not be read..." — Save disabled |
| empty | read fine, no active vendor | "No vendors are on the register yet..." + **Go to Vendors** — Save disabled |
| ready | there is someone to sell to | the select, plus "Not on this list? Add them on the Vendors page, then come back." |

Collapsing the first two would tell someone to add a vendor that already exists, or leave them
staring at an empty dropdown with no reason.

---

## Canonical sources

- Migration: `backend/migrations/postgres/000193_sales_deals_buyer_vendor.sql`
- Nav + page contract: `backend/internal/adminui/app/service.go`
- Write rule: `backend/internal/sales/domain.DealWrite.Validate`
- Picklist: `backend/internal/procurement/{ports,app,adapters}` → `ListVendorOptions`
- Pinned by `TestSalesPageContractAndNavigation`, `TestDealWriteValidateRejectsEachBrokenField`,
  `TestCreateDealNormalizesValidatesAndRequiresAKey`,
  `TestProcurementDirectorLensKeepsOnlyProcurementAndFeed`,
  `TestVendorOptionsPicklistIsActiveOnlyAndNameOrdered`
