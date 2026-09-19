# The Weighing FCR tab: feed given against weight gained

Maintainer decision, 2026-09-07.

## What was asked for

> "Under Weighing lets have FCR - we know feed consumption and weight gain, lets display FCR for
> different breed wise, weights wise, time wise, etc. all dimensions. Under weighing add a tab called
> as FCR ... for that shed what feed we have given in that time frame ... while calculating take we
> are selling at avg price of 425 per kg ... and price don't fix it, keep it somewhere in db, we need
> to be able to change it ... try to keep for sheep and goat different."

Confirmed the same day, before code: one editable price per species (goat and sheep, both seeded at
₹425), stored in the database with no UI write control (the maintainer edits the rows); the feed
side is the DIRECTED sheet quantity; the cuts are pen, breed, weight band, park, farm-born vs
purchased, weekly time, and sex.

## The rule

**FCR is kilograms of feed per kilogram of live weight gained over the SAME animals and the SAME
days. Lower is better. The base grain is the PEN, and every cut is a roll-up of pens.**

Feed is directed to a pen and weighing happens per pen (whole-shed) or per scanned kid inside a
pen, so nothing finer than a pen has both facts. The unit of measurement is the **segment**: two
consecutive weighing rounds of one pen.

```text
segment feed_kg   = Σ quantity_kg the sheet directed to the pen on days [round₁, round₂)
segment head_days = Σ over those days of the sheet's own head count for the pen
segment adg       = whole-shed: (avg₂ − avg₁) × 1000 / days
                    scanned:    mean over kids weighed in BOTH rounds of (w₂ − w₁) × 1000 / days
segment gain_kg   = adg × head_days / 1000
pen FCR           = Σ feed_kg / Σ gain_kg over its segments
group FCR         = Σ feed_kg / Σ gain_kg over its member pens      (never a mean of pen ratios)
```

ADG × fed head-days is what keeps numerator and denominator on one population: a pen's TOTAL weight
moves when animals enter or leave, which is not growth, while the feed sheet's head count on each
day is the population that actually ate. It also makes whole-shed and scanned pens one formula.

## The grain bridge

A weighing bucket names a location plus an optional partition label, and on the live estates that
location is often a legacy alias row (`Godel 1 - Part 3` as its own `locations` row with a blank
label, beside the newer `Godel 1` + label `Part 3`). The feed sheet keys on the physical shed plus
the partition label the herd register writes. So every bucket is resolved to (physical shed,
scrubbed partition key) before anything is joined: a bucket carrying a label IS on its physical
shed; a bucket with no label takes the longest active shed name in its park that prefixes the
bucket's name as the shed and the remainder as the label; a bucket with neither is an undivided
shed. The scrub (`lower(btrim())`, leading `part`/`pt` and separators removed) is the one
`weight_demographics.go` and `sex_scope.go` already apply. Verified on the local estate copy:
every bucket with feed rows resolves to exactly one feed pen in both parks.

## Cohorts are agree-or-neither, at pen grain, and the filters too

A pen is filed under a breed, a sex, a species or an origin only when EVERY live resident agrees;
otherwise it is one `Mixed` row, never split and never filed under the majority. This extends to the
page filters: feed is given to the whole pen and cannot be split, so under a Sex or Origin filter a
pen counts only when all its residents match. That is stricter than the Weights page, which claims a
scanned kid individually; the tab says so in its own note.

## Absence is never zero

A pen weighed once, a pen whose sheet has no rows between its rounds, and a pen that did not gain
each report a status and no ratio, stay in the pen table, and stay out of every chart and every
farm total. A blocked sheet cell (`quantity_kg IS NULL`) is counted and flagged; the pen's feed is
then UNDERSTATED and the row says so. An unpriced kilogram of feed is counted as unpriced, never as
free.

## The sale price is data

`growth_sale_price_assumptions` (migration 000363): one row per (tenant, species, effective_from),
append-only, seeded at ₹425 for goat and sheep on 2026-09-07. The read takes the newest row
effective on or before the reported period's last day and prints it, with who set it and when,
beside the figures it prices. Gain value = gain kg × the pen's species price; a mixed-species pen is
not valued. Break-even FCR = average sale price over the valued gain ÷ blended feed cost per kg.

**The Comparison (Load-wise) tab now reads the same rows.** It had valued stock at ₹430 sheep /
₹450 goat hard-coded in page copy since 2026-09-03; the maintainer chose one editable price for the
whole Weighing area, so those copy keys are deleted and the tab prices remaining stock from
`GET /growth-director/sale-prices`.

## Where it lives

Backend: `backend/internal/growthdirector` (`adapters/postgres/fcr.go`, `domain/fcr.go`), the
read-only reporting module that already reads weighing, herd and feed tables side by side. Weighing
itself is untouched and its isolation guard needs no new exemption. Routes
`GET /growth-director/fcr` and `GET /growth-director/sale-prices`, both on `weighing.monitor` with
the Weights-screen park scope. Admin-web: the eighth tab on `/weighing/analytics`
(`features/weighing/fcr-tab.tsx`), every string from the page contract.

## Known and accepted

- Feed is the DIRECTED quantity, not a measured intake; the only measured feed figure (the
  verifier's packed weight) is sparse. The tab's `basis` field and note disclose this.
- Park-level external feed consumption that no sheet assigns to a pen is left out.
- The Growth Director block on `/weighing/weights` still shows its older `kg_feed_per_kg_gain` per
  pen (per-head feed ÷ median ADG). Repointing that widget to this read is a follow-up the maintainer
  should decide; until then the two figures can differ for a pen, and this is flagged rather than
  hidden.

## Pinned by

`domain/fcr_test.go` (sum-over-sum, absence statuses, agree-or-neither, pen-grain filters, Monday
weeks), `adapters/postgres/fcr_integration_test.go` (a whole-shed pen and a scanned pen reached
through the alias bridge, asserted on the rendered numbers; the once-weighed window),
`permissions/growth_fcr_route_test.go`, and `features/weighing/fcr-tab.contract.test.mjs` (single
Promise.all under the page filters, backend-authored copy, no client-side ratio, rates retired from
copy).
