# Sales: Buyer analytics

Maintainer request 2026-09-15 (Manohar). Status: accepted, implemented.

## What was asked

Under Sales, a **Buyer analytics** page: customer name, phone number, repeatability, number
purchased so far, and the like — who the farm sells to, read back per buyer.

## Where it lives and why

`GET /procurement/buyer-analytics`, served by the **procurement** package, rendered at
`/sales/buyer-analytics` (page key `sales-buyer-analytics`, nav label "Buyer analytics", between
Market analytics and Vendors).

It is a procurement read and not a sales read because of what a buyer row needs. The name, phone
number, category and place of a buyer live only on the vendor register (`procurement_vendors`);
the deals live on the sales ledger (`sales_deals`, `sales_deal_lines`). Sales reads NOTHING from
procurement (migration 000173's lock), so the join can only be made from the procurement side —
the same recorded cross-module reporting shape as load-wise (`docs/decisions/sales-loadwise.md`):
read-only, reporting grain only, nothing here gates a sale or edits a vendor, and the sales lock
is untouched because the dependency points the other way.

## Who is ONE buyer

The ledger carries two identities. A deal recorded in the app names a vendor
(`buyer_vendor_id`, migration 000215); the 2026-08-17 sheet import carries only the typed name;
and two app deals on STG point at vendor ids whose rows have since been deleted. On STG,
"Mahendran" is 16 sheet deals plus 2 app deals — keying on the vendor id alone splits him in two,
keying on the typed name alone throws the register away.

So a deal is claimed by a buyer in this order, resolved ONCE in
`procurement/adapters/postgres.ClosedBuyerDeals` so every figure ranges over one identity:

1. the vendor its `buyer_vendor_id` names, when that row still exists;
2. otherwise the ONE vendor whose **business name** matches the typed name, whitespace-collapsed
   and case-insensitively (`domain.NormalizeBuyerName` is the same normalization the SQL
   applies). A name held by two vendors is claimed by NEITHER — agree-or-go-bare, the rule the
   sex and origin filters use — because picking one would put a stranger's phone number on the
   row. The contact person's name is never matched, for the same reason;
3. otherwise the typed name itself, reported as `in_register: false` on the wire. The page
   does NOT flag it (maintainer instruction 2026-09-16 retired the "Not in register" chip and the
   headline count): the sales desk reads buyers, not register hygiene. A name-only buyer shows
   the newest deal's spelling and place.

Rows are ordered by LAST SALE, newest first (same maintainer instruction), with revenue as the
tie-break: the page answers "who bought lately", not "who paid most".

## The figures

Per buyer, over CLOSED deals only (the same status predicate as every sales aggregate):
purchases (deal count), animals (the deal's lines summed, live products only, or the deal-level
count for a pre-lines deal), revenue and its share of the whole-filter revenue, outstanding
(`sales_value − payment_received`, never negative per deal), first and last sale date, product
types, and:

- **Comes back** — `repeat` is true once a buyer has bought more than once;
  `repeat_purchases` is every purchase after the first.
- **Cadence** — `avg_days_between` is the span from first to last sale over the gaps between
  them. Absent (not 0) for a one-time buyer and for a repeat buyer whose purchases all landed
  on one day: "buys every 0 days" is not a cadence.
- **Recency** — `days_since_last`, a difference of IST business dates.

The summary is whole-filter: buyers, repeat vs one-time, not-in-register, purchases, animals,
revenue, repeat-buyer revenue and its share, outstanding. `limit`/`offset` page the rows only.
Pinned by `TestBuildBuyerAnalyticsFoldsDealsIntoOneBuyerAndReadsRepeatCadence`,
`TestBuildBuyerAnalyticsPaginationSlicesRowsButNotTheSummary` and the DB round-trip
`TestClosedBuyerDealsOneToManyLinesAndVendorsResolveToOneFactPerDeal` (dangling vendor id, ambiguous name,
contact-only match, mixed-line deal, non-closed and other-tenant rows).

## Permissions

The page and the read ride `SalesRead`: it is sales money. The **phone number is register
data**, and `/sales/vendors` is gated on `VendorRead` precisely so a sales reader who was never
given the register does not reach it through a second door. The same rule holds here on both
halves of the capability-gated lock:

- the endpoint includes `phone_number` only for a caller holding `VendorRead`, resolved from the
  per-person permission set when that decided the request and from the grant roles otherwise
  (`callerMaySeePhones`; pinned and mutation-tested by
  `TestBuyerAnalyticsPhonesFollowVendorRead`), and says so with `phones_visible`;
- the page contract declares `buyer_phone_column` as a read control on `VendorRead` with the
  backend's reason (`compileBuyerAnalyticsControls`), so the column is absent with an
  explanation rather than a column of blanks that reads as "no buyer has a number".

Every role holding `SalesRead` today also holds `VendorRead`, so by role the disabled branch is
unreachable; it exists for the per-person path.

Migration `000314` appends the page key to every explicit web Sales row and designation
template (the 000309 shape) so nobody with named ticks misses a page shipped after their rows
were written; the CEO floor needs no row.
