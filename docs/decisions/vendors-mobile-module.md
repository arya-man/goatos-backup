# Vendors on the phone (maintainer decision, 2026-09-03)

## What changed

Procurement moved onto the phone as ONE module, **Vendors**, with two bottom-bar tabs:

| Tab | Href | Reads | Writes |
|---|---|---|---|
| Vendors | `/vendors` | the vendor register, searched and narrowed by status | add a vendor (three-step wizard) |
| Feed Purchases | `/vendors/feed-purchases` | the feed purchase ledger, narrowed by delivery state | record a purchase (two-step wizard) |

Marking a purchase reached, instalments, edits, and a vendor's payment instruments stay on the web.

**Who.** `ceo_internal`, `procurement_director`, `procurement_manager` — the holders of
`procurement.vendor.read`. The module is offered on that PERMISSION (the toxin shape), never on a
job, so `feed_director` — who reads feed purchases on the web — does not get it. The two
procurement roles gained `app.bootstrap` for this; the earlier "admin-web only" note on the
director is retired. Every existing person with a web `vendors` tick received a mobile one
(migration `000249`), because a tick narrows and never widens.

## Vendor capacity and frequency

A vendor now carries **capacity** (how much per delivery, in a catalog unit: kg, tonnes, animals,
litres, bags) and **supply frequency** (every week, every 2 weeks, every month, every 3 months,
one time). Quantity and unit travel as a pair; one without the other is refused. Both vocabularies
are `procurement_vendor_catalog` rows (`capacity_unit`, `supply_frequency`), so the farm can
reword or extend them without a deploy. `capacity_display` ("5,000 kg · Every 2 weeks") is
backend-composed from the labels so the phone and the web phrase it identically. Migration `000247`.

## The voice note

A vendor may carry a **voice note**: an `audio` proof recorded on the phone's microphone. It rides
the existing proof pipeline end to end — durable capture row, proof outbox upload on the vendor's
own group (so it lands BEFORE the vendor write that references it), tenant-scoped signed download
for playback on the phone and the web. Nothing about it is a second media path.

`audio` is a new `proof_artifacts.proof_type` (migration `000248`). The proof service holds an
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
`TestVendorCapacityTravelsAsAPair`, the Room `migration 54 to 55` test, `VendorsPresentationTest`,
and the `vendors L0 roots` chrome test.
