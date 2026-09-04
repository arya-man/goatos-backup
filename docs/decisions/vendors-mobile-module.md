# Procurement on the phone (maintainer decisions, 2026-09-03 and 2026-09-04)

## What changed

Procurement moved onto the phone as ONE module, shown as **Procurement** (module key `vendors`,
renamed on screen 2026-09-04; the key stays because it is the person_module_access tick and the
web capability key), with three bottom-bar tabs:

| Tab | Href | Reads | Writes |
|---|---|---|---|
| Vendors | `/vendors` | the vendor register, searched and narrowed by status | add a vendor (three-step wizard) |
| Feed Purchases | `/vendors/feed-purchases` | the feed purchase ledger, narrowed by delivery state | record a purchase (two-step wizard) |
| Sales | `/vendors/sales` | the sales ledger, narrowed by farm | record a sale (three-step wizard); tag animals to a sale (pick → review → confirm) |

Marking a purchase reached, instalments, edits, payments on a sale, deal status changes, leads,
market quotes, and a vendor's payment instruments stay on the web.

## Sales (maintainer instruction 2026-09-04)

The Sales tab mirrors exactly the two entry flows of the web's `/sales/config`: the record-sale
drawer and the tag-animals (sale allocation) drawer. Same routes, same fields, same rules:

- `GET /sales/deals` (offset-paged, Room-first like the feed ledger; each row is also cached as
  the deal's detail because the backend has no per-deal read), `POST /sales/deals` through the
  outbox (`SALES_DEAL_CREATE`, stable client id as the required `Idempotency-Key`).
- `GET /sales/options` is NEW: farms, product types, the breeds per product, the four statuses
  with their chip tones, the default status and the 60-day date horizon — the vocabulary the web
  drawer keeps as page-contract option groups, now backend-owned for both surfaces
  (`sales/domain.BreedsByProduct`, `StatusTone`, `MaxSaleDateDaysAhead`; pinned by
  `TestSalesOptionsCarryEveryVocabularyTheWebDrawerOffers`).
- The buyer is picked from `GET /procurement/vendor-options` (reachable on `sales.read`), with
  the web's four list states kept apart: loading, unavailable, empty, ready (+ truncated).
- Tagging animals is `GET /admin/goats/sale-locations` → `GET /admin/goats/sale-candidates`
  (cursor-paged, searched by RFID or animal id) → `POST .../preview` → `POST .../confirm`
  (Idempotency-Key, fresh after a refusal). It is deliberately ONLINE: every step is judged
  against the live herd and a cached "sellable" would be a lie by confirm time. Blocked animals
  and pen names are the server's own copy, rendered verbatim.

`procurement_manager` now holds `sales.read`, `sales.write` and `sales.allocate_animals`
(the same three the director held), because the maintainer named the manager as a user of this
module. The backfill and the web pages follow the same permissions.

**Who.** `ceo_internal`, `procurement_director`, `procurement_manager` — the holders of
`procurement.vendor.read`. The module is offered on that PERMISSION (the toxin shape), never on a
job, so `feed_director` — who reads feed purchases on the web — does not get it. The two
procurement roles gained `app.bootstrap` for this; the earlier "admin-web only" note on the
director is retired. Every existing person with a web `vendors` tick received a mobile one
(migration `000250`), because a tick narrows and never widens.

## Vendor capacity and frequency

A vendor now carries **capacity** (how much per delivery, in a catalog unit: kg, tonnes, animals,
litres, bags) and **supply frequency** (every week, every 2 weeks, every month, every 3 months,
one time). Every part is optional on its own (maintainer instruction 2026-09-03, "keep capacity as
optional only"): a quantity, a unit or a frequency is recorded when given and none is required
with another; an entered quantity must still be a positive amount. Both vocabularies
are `procurement_vendor_catalog` rows (`capacity_unit`, `supply_frequency`), so the farm can
reword or extend them without a deploy. `capacity_display` ("5,000 kg · Every 2 weeks") is
backend-composed from the labels so the phone and the web phrase it identically. Migration `000248`.

## The voice note

A vendor may carry a **voice note**: an `audio` proof recorded on the phone's microphone. It rides
the existing proof pipeline end to end — durable capture row, proof outbox upload on the vendor's
own group (so it lands BEFORE the vendor write that references it), tenant-scoped signed download
for playback on the phone and the web. Nothing about it is a second media path.

`audio` is a new `proof_artifacts.proof_type` (migration `000249`). The proof service holds an
audio upload to the video rule's honesty: `capture_source = in_app_microphone`, a known uploader,
a captured window; there is no gallery path. `procurement/adapters/proof` validates the ref before
it is stored on the vendor (completed, in-tenant, declared AND stored as audio, in-app microphone),
and an unusable ref is refused `vendor_voice_note_invalid`, never stored unchecked.

Route permissions: `procurement.vendor.write` joined the proof-upload OR; `procurement.vendor.read`
joined the proof-download row. Neither widens `task.execute` / `task.read` to anyone.

## Phone shape

- Offline-first reads: Room is the single source of truth (`vendor_items`, `feed_purchase_items`
  with per-scope cursors; one blob cache for detail, catalog and form options), ~20-row pages,
  refresh on open. Room v55, `MIGRATION_54_55`.
- Offline-first writes: `VENDOR_CREATE` and `FEED_PURCHASE_CREATE` outbox ops with STABLE client-id
  keys (`vendors:create:<id>`, `vendors:purchase-create:<id>`). A vendor create carries no server
  idempotency header; the register's natural key refuses a duplicate with `409 vendor_duplicate`,
  which a retry reads as "already recorded". A purchase create sends its key as the backend's
  required `Idempotency-Key`.
- Every visible sentence on a card or detail row is backend-owned and rendered verbatim. Form field
  labels are app chrome, as on every other phone entry form.

## Pinned by

`TestVendorsModuleIsOfferedOnVendorRead`, `TestProcurementDeskReachesThePhoneAndTheProofHandshake`,
`TestVendorCapacityIsOptionalInEveryPart`, `TestSalesOptionsCarryEveryVocabularyTheWebDrawerOffers`, the Room `migration 54 to 55` test, `VendorsPresentationTest`,
and the `vendors L0 roots` chrome test.
