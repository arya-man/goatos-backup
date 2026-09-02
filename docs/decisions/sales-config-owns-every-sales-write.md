# Sales Config owns every sales write; Sales and Purchase and Born are read-only

**Maintainer decision, 2026-09-01.**

> "in sales now add one more page called as sales config addition everything
> regarding load sales and normal sales everything move it to sales config like
> addition changes editing should happen in that page only sales and purchase and
> barn only for seeing"

## The rule

There is one sales entry surface: **`/sales/config`, "Sales Config"**. Every
sales fact is recorded, edited or corrected there, for load sales and ordinary
sales alike:

| What is entered | Where it used to be |
| --- | --- |
| Record a sale | `/sales` header button |
| Tag the animals a sale is made of | `/sales` header button |
| A deal's payments and status | `/sales` deal drawer |
| Buyer leads, farmer-group leads | `/sales` pipeline section headers |
| Market quotes, sold-tag lists, weight checks | `/sales` evidence section headers |
| A purchased load's landed cost | `/sales/loads` row click |

`/sales` (the board) and `/sales/loads` (Purchase and Born) are **for seeing**.
They read the same facts back and offer no way to change them. A deal row on
`/sales` still opens its drawer, because reading a deal's detail is seeing; the
drawer simply carries no form.

## How read-only is enforced

Not by hiding buttons in the renderer. The **backend page contracts for `sales`
and `sales-loads` declare no write control at all** — not a disabled one — so
`controlEnabled(...)` is false for every principal including the CEO, and a
renderer that tried to draw a button would have no capability to draw it from.
`compileSalesConfigControls` is reached only by the `sales-config` case in the
contract compiler.

This matters more than a hidden button would: a control is the thing the
role-scoped-UI lock makes the renderer gate on, so removing it removes the
action from every role at once and leaves no per-role conditional to drift.

## What did NOT merge: the authorities

Sharing a page is not sharing authority. On `/sales/config`:

- `record_sale`, `record_pipeline`, `record_sales_deal_payment` and
  `update_sales_deal_status` are gated on `permissions.SalesWrite`.
- `record_load_cost` keeps `permissions.LoadCostWrite`. Costing a load is the
  buying desk's money, not the sales desk's, and the two desks are different
  people. A sales director therefore sees that one control disabled with its own
  reason while every other control on the page is live.

The **page** is reached on `permissions.SalesRead` rather than on a write
permission — the `/health/config` shape. A reader who cannot record still opens
it and finds each control disabled with a backend-owned reason; withholding the
leaf instead would read as a broken product rather than as an answer.

## IA

`/sales/config` is the third and last entry in
`check-ia-guard.mjs` `MODULE_SURFACE_ROUTE_EXCEPTIONS`, after `/feed/config` and
`/health/config`, and is admitted for the same reason: it is a module's own
data-entry surface, classified `module-surface` and not `authority-screen`, and
it duplicates no top-level lens. `/config` remains the single generic
protocol-rule authority screen. The `config` segment stays in
`COMMAND_SEGMENTS`, so any other nested config route still fails without its own
recorded decision.

## Why one page rather than a button on each board

The two boards are read at a different time, and by a different eye, than the
desk work of entering a sale. More concretely: an entry form that exists on two
screens is a form whose two copies drift — different field sets, different
vocabularies, different validation — and the drift is invisible until someone
records a sale that the other screen cannot show. Moving the forms rather than
copying them keeps one implementation: `/sales/config` mounts the *same* drawer
components the read pages used to mount, so the field vocabulary, the
idempotency keys and the blocked-safe boundaries are unchanged by this decision.

## Proof

- `TestSalesReadPagesCarryNoWriteControl` — `/sales` and `/sales/loads` declare
  none of the five write controls for a CEO who holds every sales permission,
  and all five are enabled on `/sales/config`. Mutation-tested: restoring the
  write compilation on the `sales` page turns it red.
- `TestSalesConfigPageContract` — the route, both row-click params, the merged
  copy (both read pages' keys plus the page's own headings) and the shared form
  vocabulary.
- `TestSalesConfigNavLeafRidesSalesRead` — the page is reached on the read
  permission, not the write.
- `TestSalesRecordSaleControlIsCapabilityGated` and
  `TestRecordLoadCostControlIsCapabilityGated` now read `/sales/config`; the
  latter gained a sales-director row, which is the case that would go green if
  the load-cost control ever started riding the page's own permission.
- `TestRetiredProcurementDirectorLensIsReproducedByTicks` — the procurement
  director, who owns the load-cost write, gains the new leaf.
