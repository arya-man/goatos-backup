# Load wise values unsold animals by weight × Sales Config ₹/kg

**Status:** decided (maintainer, 2026-10-02). Supersedes the stock-value half of
`docs/decisions/sales-loadwise.md` §4 (remaining × average sold price), the 2026-09-19 Sales Config
"unsold animal price", and the 2026-09-25 "by weight only when every animal is weighed" rule.
Machine gate: `make loadwise-stock-valuation-guard`.

## The rule

| Part of a load | Valued at |
|---|---|
| Animals already sold | what they actually sold for (closed deals, each animal's own sale line) |
| Animals still on farm | each animal's **latest weight × the ₹/kg of its stage, species and gender** on Sales Config's Farm valuation |
| Position (profit / loss) | sold value + stock value − landed purchase cost |

- **Latest weight** is the newer of the animal's own scan (not sent back for rework) and the latest
  whole-pen weigh of the pen it stands in, taken after it arrived in that pen.
- **An animal not weighed yet** carries the current average weight of **its own species** in the
  load (goats the goat average, sheep the sheep average), falling back to the whole load's average
  only when no animal of its species is weighed.
- **No current weight at all** (none of the load's live animals weighed) → the stock is not valued,
  and the position reads **₹0**, never "sold value minus the whole cost". The tooltip says
  "Not valued: none of the N animals on farm is weighed yet".
- **An animal whose stage Sales Config does not price** is left out of the stock value, never priced
  at a guess.
- The bucket's "weight used (kg)" on Sales Config is a **Farm value** figure. Load wise does not use it:
  it values every animal at its own weight.

### Why

The retired rule priced a load's remaining animals at that load's own average sold price, falling back
to the farm-wide average. One sale therefore priced a whole load: on STG, load 129 had sold one animal
for ₹16,720 and all 76 animals left were carried at ₹16,720 each (₹12.71L). Two weak animals sold cheaply
dragged a whole batch down the same way. A sale price describes the animal that was sold; the animals
still on farm are worth what they weigh today at the market rate.

### The seven cases (cost ₹6,25,000, ₹450/kg, 35 kg)

| Case | Sold | Stock | Position |
|---|---|---|---|
| 1 Nothing sold | ₹0 | 70 × 35 × 450 = ₹11,02,500 | +₹4,77,500 |
| 2 One sold for ₹10,000 | ₹10,000 | 69 × 35 × 450 = ₹10,86,750 | +₹4,71,750 |
| 3 Two sold cheap (₹6,000 each) | ₹12,000 | 68 × 35 × 450 = ₹10,71,000 | +₹4,58,000 |
| 4 Half sold | ₹5,60,000 | 35 × 35 × 450 = ₹5,51,250 | +₹4,86,250 |
| 5 All sold | ₹11,20,000 | — | +₹4,95,000 |
| 6 No current weight | ₹0 | not valued | **₹0** |
| 7 30 goats + 40 sheep | — | 30 × 35 × 450 + 40 × 35 × 430 = ₹10,74,500 | +₹4,49,500 |

Pinned by `procurement/domain.TestLoadPositionFollowsTheFarmsSevenCases`.

## Sales Config prices by species (2026-10-02)

Farm valuation buckets are **stage × species × gender**, keyed `<stage>_<species>_<gender>`
(`fattening_goat_female`, `fattening_sheep_male`, …). The stages stay one list for both species;
each stage carries four prices (goat female, goat male, sheep female, sheep male). Species comes
from `goats.species`, which the register constrains to `goat` / `sheep`.

**Initial values (deploy and seed):** whatever each stage × gender is priced at today becomes the
price for BOTH goats and sheep. Migration `000470_valuation_is_priced_by_species.sql` copies every
stored figure into both species rows; the seeded defaults for a new tenant do the same; and until
the migration has run, a row stored the old way is READ as one price for both species (in SQL by
`farmvaluation.PricingCTEs`, in the editor by `salesdomain.UpgradeLegacyValuationBuckets`). The
migration is idempotent; its Down collapses back to the goat figure.

This applies to **Farm value** as well: both pages read the same config. One consequence there,
measured on STG on 2026-10-02: a bucket priced at its MEASURED weight (fattening) now averages the
weight per species, so sheep fattening males (28.58 kg) and goat fattening males (26.79 kg) are no
longer carried at one blended 27.49 kg. Prices unchanged, Farm value moved +₹1,86,836 (+0.7%,
₹2,81,39,848 → ₹2,83,26,684) for exactly that reason. Load wise did not move (₹56,73,616 stock).

## One pricing rule, shared

Farm value and Load wise used to carry separate copies of the stage / bucket / rate SQL. Both now
splice `backend/internal/farmvaluation`:

- `PricingCTEs` — authored stages (or the seeded defaults, **generated from**
  `salesdomain.DefaultValuationAssumptions` so the SQL fallback cannot disagree with Go) and the
  bucket rates;
- `StageJoinsSQL` — milk cohort first, then management stage, through one normalizer;
- `BucketKeySQL` — the SQL mirror of `salesdomain.ValuationBucketKey` (Mother reads female; no
  gender on file reads female; a species other than goat/sheep is `unmapped`).

`TestLoadwiseAndFarmValuePriceOneSheepTheSameMultipleDimensions` proves one fattening sheep is worth
the same rupees on both pages, at the sheep rate.

## Screens

- **Load wise › Money per load:** the hatched bar is the stock value; the tooltip states the rule and
  the total only ("63 animals × latest weight × ₹/kg by stage and sex on Sales Config = ₹9,37,108").
  Profit / loss is one value; the "Realised" split is not shown.
- **Sales Config › Farm valuation:** four price groups per stage. The "Unsold animal price" input is
  removed; a save clears the stored column.
- **Wire:** `avg_sold_price`, `price_basis`, `remaining_value`, `assumed_value_method`,
  `overall_avg_sold_price` and `unsold_price_basis` are removed from `GET /procurement/loadwise-sales`.

## Guard

`make loadwise-stock-valuation-guard` (`tools/agent-hooks/check-loadwise-stock-valuation.mjs`, in
`make guardrails` and `ci-local`), each rule with an adversarial self-test fixture:

1. `sold-price-values-stock` — no procurement Go or Load wise screen names the retired sold-price basis.
2. `valuation-sql-copied` — only `farmvaluation` (and the editor's own store) reads
   `sales_valuation_assumptions`.
3. `shared-rule-not-spliced` — both pricing pages splice the three shared fragments.
4. `bucket-without-species` — the bucket key carries the species in Go and in SQL.
5. `unvalued-stock-reads-as-loss` — the ₹0 branch and its seven-case test exist.
6. `realised-split-on-screen` — Load wise shows profit as one value.

Blind spots: names and shapes, not arithmetic. The runtime half is the three tests named above, each
mutation-tested (removing the ₹0 branch, ignoring species in `BucketKeySQL`, dropping the
load-average fill each turn one red).
