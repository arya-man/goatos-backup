# Sales executive analytics

Maintainer request, 2026-10-02: a page under Sales that shows what the sales desk is doing --
vendors first (who is adding them, how many, whether they are being changed), then calls and
sales recorded -- and how that flow moves over time.

## The page

`/sales/executive-analytics` (page key `sales-executive-analytics`, nav leaf after Buyer
analytics), read-only by contract (no control at all), gated on `sales.read` on both the leaf and
the data route `GET /procurement/sales-executive-analytics?days=7|30|90`.

- Headline: vendors added, vendors changed, calls recorded (market + buyer), sales recorded (value
  and payments), each beside the same count for the equal period immediately before.
- Trend: one LINE per kind of work (vendors added, changed, calls, sales). A 7-day period is
  drawn day by day; 30 and 90 days week by week (seven business days counted forward from the
  period's first day, the last week possibly shorter), so the whole period fits on one screen. The
  backend owns the buckets (`trend_grain`, `daily[].date`/`date_to`); they are disjoint and sum back
  to the headline.
- Who is doing what: one row per person who recorded anything, busiest first -- vendors added,
  changed, calls, sales, payments, sale updates (deal status changes), days active, last active. A
  person appears only once they have recorded something, so no row is all zeros.
- Latest vendors (the register, newest first, with who added it -- "From the old sheet" for an
  imported row) and Latest activity (the period's activities, newest first): both paged 20 at a
  time on the SERVER (`vendor_offset`, `activity_offset`, refused past 10000), shown side by side at
  one height with their lists scrolling inside the card.

Every figure is read live on each page load -- no cache, no projection -- so a vendor added or
changed, or a call / sale / payment recorded, by anyone (web or phone) shows on the next refresh.

## Where each fact comes from (recorded cross-module reporting read)

Procurement owns the vendor register and joins OUT, read-only, the buyer-analytics shape. Nothing
here gates or edits anything.

| Activity | Source | Grain |
|---|---|---|
| Vendor added | `procurement_vendors.created_by/created_at` | one vendor with a known adder |
| Vendor changed | `audit_log` `procurement.vendor.update` / `.status` UNION the row's last-edit stamp | one (person, vendor, IST day) |
| Market call | `market_price_entries.recorded_by` | one (person, city, survey day) |
| Buyer call | `audit_log` `sales.buyer_lead.record/status`, `sales.fpo_lead.record/status` | one audited write |
| Sale / payment / status | `audit_log` `sales.deal.record`, `.payment_record`, `.status_set` | one audited write |

Buckets are disjoint by kind; the day series sums back to the headline. A name resolves through
`workforce_members.user_id` (the active row, else agree-or-go-bare across inactive rows). An audit
row with no actor (a backfill) is counted in the headline but attributed to nobody.

## Vendor edits were not recorded before this change

The register keeps only its LAST editor on the row, so earlier edits were lost. From this change
`UpdateVendor` and `UpdateVendorStatus` write an audit row inside the edit transaction (a failed
audit rolls the edit back). Before deploy, only each vendor's last edit is visible, via its stamp;
the union with the audit rows is grouped per (person, vendor, day), so the newest save is never
counted twice. Of 692 vendors on STG on 2026-10-02, 661 came from the old sheet with no adder.
