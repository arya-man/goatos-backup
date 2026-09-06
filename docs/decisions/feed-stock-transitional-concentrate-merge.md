# Feed stock: the transitional split-concentrate merge

**Status: TEMPORARY. This is built to be deleted.** Read
[Reverting it](#reverting-it) before changing anything here — the revert is a
deletion, and it is meant to stay that way.

Maintainer decision, 2026-09-06.

## What changed

The farm used to buy **four** in-house concentrates — adult goat, adult sheep,
kids goat, kids sheep. It now buys **two**, one adult and one kids, each fed to
both species. That is a purchase decision, not a data migration, so for as long
as the old sacks last the store physically holds up to three feeds that are one
feed operationally.

The Stock tab showed them as three unrelated cards. On 2026-09-06, Coimbatore
read **"2 days left"** for adult goat beside **"166 days left"** for the merged
adult feed, and a third card said 4,300 kg of kids feed had never been fed. None
of those numbers was the farm's runway. The true answer was **12 days**.

The stock cards and the low-stock alert now fold each retired split feed into
its successor, on **both sides of the division**:

```
family stock = Σ each member's own balance
family rate  = the family's kg per CALENDAR DAY   (not Σ each member's rate)
days left    = family stock ÷ family rate
```

Everything outside those four keys is untouched, byte for byte.

## Why the rate is per family-day and not a sum of the members' rates

Because the feeds **substitute** for each other while the ration grid switches
over. On 5 and 6 September, Channapatna fed 84 kg of the merged adult feed
*instead of* the sheep feed. Adding each member's own 3-day average gave
**184.9 kg/day** against a true family draw of **128.2** — 40% high, and a
days-left a third short.

Re-grouping consumption to the family *before* averaging counts each day once
and needs no substitution rule. This is the single most important line in the
change, and it is what `family_day` in both queries does.

## What it does to the live numbers

Read from STG on 2026-09-06 (read-only), before and after:

| farm | family | stock kg | rate kg/day | days | replaces |
|---|---|---|---|---|---|
| CBE | Mesha Adult Concentrate | 2,620.2 | 206.9 | **12** | 2 · 166 |
| CBE | Mesha Kids Concentrate | 4,599.0 | 379.8 | **12** | 3 · 0 · "not started" |
| CPT | Mesha Adult Concentrate | 1,644.4 | 128.2 | **12** | 3 · 26 |
| CPT | Mesha Kids Concentrate | 3,815.6 | 348.4 | **10** | 0 · "not started" |

Sixteen served rows become ten. The six unmerged cards — Concentrate 0 and 64,
UHT Milk 7 and 16, Dry Masoor Bhusa 10 and 13 — are unchanged.

## Four choices worth knowing

**A negative member is SUBTRACTED, not floored.** Channapatna's adult sheep
balance is −67.4 kg: the farm fed more than the ledger bought. In a merged store
that feed physically came out of a sibling sack, so subtracting it is both the
truer figure and the more conservative one. It also keeps a family balance the
plain sum of the same per-item balances the tab already shows, and preserves the
existing never-clamp rule on every unmerged card. Flooring instead would read 13
days at CPT rather than 12.

**The retired check reads the FAMILY key.** Adult sheep is `retired` in
`feed_item_catalog`, which is why its 4 kg at CBE and −67.4 kg at CPT were
invisible before. A retired *member* now still contributes its stock, while a
retired feed with *no* successor (Hedge Lucerne, Toor Dal Bhusa Pellet) still
drops out. That is the 2026-09-06 active-vocabulary rule kept intact one level
up.

**The card title comes from the mapping, not the catalog**, so a family reads
correctly at a farm that has not bought the successor yet — the members alone
must still title the card with the feed the farm buys now.

**The low-stock push folds identically.** Both queries take the same three
parameters from the same `domain.StockFamilyMergeArrays()`. Without that, the
daily alert would still say "Mesha Adult Concentrate Goat · 2 days" while the tab
read 12 — the cross-surface disagreement `AGENTS.md` bans.

## What is deliberately NOT folded

| surface | why |
|---|---|
| the per-farm Mesha concentrate table (`MeshaConcentrateStockKeys`) | it is the audit view: which sacks were bought and drawn, per feed. Folding it would delete the only place the split is still visible |
| the 7-day requirement/forecast table | keyed on consumption, and it answers "what will be directed", which is still per item until the grid switches |
| the expenditure series and money charts | priced per load per item; a family has no purchase price |
| feed sheets, ration grid, packing, transport, distribution | the operational feed chain is untouched. This is a **reporting** fold, and no write path reads it |

## Reverting it

The merge **expires on its own**. Once every member's stock reaches zero and the
ration grid names only the successors, each family is a single feed, the fold
changes nothing, and it can be deleted with no visible effect.

Check it is safe to remove:

```sql
-- Expect zero rows. Any row still holding stock is still being folded.
SELECT farm_label, feed_item_key, SUM(quantity_kg - consumed_at_import_kg) AS bought
FROM feed_purchases
WHERE tenant_id = $1
  AND feed_item_key IN ('mesha_adult_concentrate_goat','mesha_adult_concentrate_sheep',
                        'mesha_kids_goat_concentrate','mesha_kids_sheep_concentrate')
GROUP BY 1, 2 HAVING SUM(quantity_kg - consumed_at_import_kg) > 0;
```

Then, in one commit:

1. Delete `backend/internal/feeddirection/domain/stock_family_merge.go` and its
   test.
2. In `backend/internal/feeddirection/adapters/postgres/analytics.go`, delete the
   `merge_map` CTE and the `$3/$4/$5` parameters from `stockItemsSQL` and
   `feedLowStockSQL`; in each, collapse `item_balance`/`family_stock` back into
   the `SELECT` (the `COALESCE(mm.family_key, …)` becomes the item's own key) and
   `family_day` back into `locked_cells`/`fed`.
3. Delete `TestStockCardsFoldTheSplitConcentratesIntoOneFamily`.
4. Delete this file and the pointer to it in `AGENTS.md`.

**An empty mapping is already the pre-merge behaviour exactly** — every item is
its own family, and both queries reduce to the per-item shape they had before.
That property was verified against live STG data (empty arrays reproduced all
sixteen pre-merge rows, including the `2 / 166 / 3 / not started` spread), and it
is what makes step 2 a mechanical simplification rather than a rewrite. If you
only need to switch the behaviour off in a hurry, returning empty slices from
`StockFamilyMergeArrays()` does it, with the SQL left in place.

## What the permanent fix is, when it is wanted

A successor/substitution relationship on `feed_item_catalog`, authored on
`/feed/config` — one nullable `succeeded_by_feed_item_key`, and the same fold
driven from the catalog instead of a Go table. That is a schema change, an
authoring screen and a permission, which is why it was not done for a mapping
with four rows and a life expectancy measured in weeks. The forward-looking
alternative to the whole days-left question — a **planned** rate from the ration
grid × live head count, instead of a backward-looking burn — is a separate and
larger piece of work; the 3-day and 7-day windows agree to within 0.5% on the
live data, so it was not needed here.

## Proof

- `domain.TestStockFamilyMergeIsAFlatFoldOfExactlyTheFourSplitFeeds` — mapping
  invariants: parallel arrays, no duplicate member, no chain, one label per
  family, and the folded set equals `MeshaConcentrateStockKeys`. Mutation-tested
  by adding a chain row; goes red.
- `postgres.TestStockCardsFoldTheSplitConcentratesIntoOneFamily` — one card per
  family, stock summed across an overdrawn member, and a **substitution day** that
  makes the naive sum-of-rates read 240 kg/day against a true 120. Asserts 8 days,
  which neither the sum-of-rates (4) nor the floored-negative (9) variant reaches.
- Live read-only STG comparison, both directions, recorded in the table above.
