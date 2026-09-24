Sections: Shared rules | S1 Stock cards (days left, balance, kg/day) | S2 Per-farm concentrate table | S3 Next-7-days requirement | S4 Loads table | Traps

# Feed Analytics > Stock tab: logic card

Screen: admin-web `/feed/analytics?tab=items` (`apps/admin-web/features/feed/feed-analytics.tsx`, `StockCards` at :1709). The Stock tab asks for `sections=items,farm_items,forecast` (:379-382).
Endpoint: `GET /feed-analytics/stock` (`backend/internal/feeddirection/adapters/http/handler.go:83`, handler `analytics_handler.go:742`) -> `Repository.StockAnalytics` (`adapters/postgres/analytics.go:2578`). Loads table: `GET /feed-analytics/stock-loads` (handler.go:84).
Filters -> SQL: top-bar park -> `$2 park_ids uuid[]`. An empty array means all parks. The date range does NOT affect the stock cards, the farm table or the forecast, because those read the whole ledger. The date range only affects the Overview expenditure and spend reads.

## Shared rules (all stock numbers)
- **Purchases** (`feed_purchases`): only `delivery_status='reached'`. Net kg = `stock_kg - consumed_at_import_kg`. `stock_kg` is a generated column: the received weight, else the bought weight, and 0 while in transit. Depletion counts from `depletes_from`, which follows `reached_on`.
- **Consumption** = locked sheets (`feed_direction_issues.state='locked'`, `feed_direction_issue_rows.quantity_kg IS NOT NULL`) UNION ALL `feed_effective_external_consumption`, re-grouped to (park, item, day).
- **UHT litres**: the view `feed_effective_external_consumption` (migration `000216_uht_stock_from_milk_preparation.sql`) reads `milk_preparation_proof_attempts.answers->>'uht_milk_quantity_litres'` for the current attempt of every non-retired `milk_preparation_completions` row. The value is keyed on **feeding_date** and read as kg 1:1. The `feed_external_consumption` ledger (000185) is only a fallback for days that have no prep row.
- **Family merge**: `domain.StockFamilyMerge` (`domain/stock_family_merge.go:50`) folds 4 retired split concentrates into 2 families:
  - `mesha_adult_concentrate_goat|_sheep` -> `mesha_adult_concentrate`
  - `mesha_kids_goat|sheep_concentrate` -> `mesha_kids_concentrate`

  Each item's balance is computed first, then the balances are SUMmed per family. The balance is never clamped at 0. The burn rate is computed on family-day kg.
- **Rate override**: `domain.StockRateOverrides` (`domain/stock_rate_override.go:37`) = `{CBE, concentrate, 55}`. It replaces the computed kg/day for that (farm, family_key).
- **Burn-rate window**: the average of the 3 most recent days the family APPEARED in consumption (`ROW_NUMBER ... rn<=3`). These are not the last 3 calendar days.
- **Card membership**: `feed_item_catalog.status <> 'retired'` on the family key. A key with no catalog row is still shown.

## S1 Stock > Stock cards: Days left / Balance kg / kg per day / Batch / Low / Not started
| | |
|---|---|
| Endpoint | `GET /feed-analytics/stock?sections=items` -> `.items[]` |
| Code | `analytics.go:1666` `stockItemsSQL`, run by `stockItems` :2676. Low flag at :2691 (`DaysLeft < domain.LowStockDays`, where LowStockDays=5 at `domain/analytics.go:807`) |
| Formula | `balance_kg = Σ_members( Σ reached (stock_kg - consumed_at_import_kg) - Σ consumed kg since that item's MIN(depletes_from) )`. `avg_daily_kg = COALESCE(override, avg of last 3 family-days)`. `days_left = GREATEST(floor(balance/avg),0)`, which is NULL when avg<=0. `not_started = avg<=0 AND balance>0`. `latest_batch` = batch of the freshest depletes_from |
| Filters->SQL | park -> `$2`. `$3..$5` = merge arrays, `$6..$8` = override arrays |
| UI | The UI hides the key `concentrate` (the legacy CBE feed). It also hides a hideable concentrate whose days_left<=0 (`feed-analytics.tsx:1659-1688`). Sort order is days_left ASC NULLS LAST |
| Traps | Grain is **per farm**, and there is no tenant-wide days-left. The farm label on a card is CBE/CPT (`feed_purchases.farm_label`), not the location name. The CBE override is keyed on `concentrate`, which is now retired and hidden, so it does **not** apply to any shown card today. Negative member balances reduce the family total on purpose. UHT consumption includes TOMORROW's feeding_date as soon as the prep is submitted (see milk.md) |

SQL sketch: run `references/feed-stock-days-left.sql` as-is. It is the stockItemsSQL with the tenant, all parks, the merge and the override inlined.

stg value (24/09/2026):

| farm | feed | balance kg | kg/day | days left | batch |
|---|---|---|---|---|---|
| CBE | UHT Milk | 32.0 | 7.0 | 4 (LOW) | 363 |
| CPT | UHT Milk | 45.0 | 6.0 | 7 | 339 |
| CPT | Kids Conc | 5221.8 | 374.2 | 13 | |
| CBE | Adult Conc | 2877.9 | 201.1 | 14 | |
| CPT | Adult Conc | 1825.4 | 126.4 | 14 | |
| CBE | Kids Conc | 6369.3 | 451.5 | 14 | |
| CBE | Masoor Bhusa | 11870.7 | 639.8 | 18 | |
| CPT | Masoor Bhusa | 9691.4 | 427.4 | 22 | |

Q: "How many days of concentrate are left at CBE?" / "CBE mein concentrate kitne din chalega?" · "Which feed runs out first?" / "Sabse pehle kaunsa feed khatam hoga?" · "How much bhusa is in stock?" / "Bhusa kitna bacha hai?" · "Is UHT milk running low?" / "UHT doodh kam hai kya?"

## S2 Stock > Per-farm Mesha concentrate table
Columns: Item, Farm, Consuming last load since, Last load (batch/date/qty/vendor/₹/₹ per kg), Avg kg/day, Weekly kg, Stock kg, Days left.
| | |
|---|---|
| Endpoint | `sections=farm_items` -> `.farm_items[]` |
| Code | `analytics.go:2317` `stockFarmItemsSQL`, run by `stockFarmItems` :2700. Keys come from `domain.MeshaConcentrateStockKeys` (`domain/analytics.go:841`, 6 keys) plus the family merge |
| Formula | `ledger_stock_kg` = the same family balance as S1. `avg_daily_kg` = the 3-family-day average (**no override**). `weekly_required_kg = avg*7`. `last_load_consumption_from` = the FIFO day on which cumulative family kg since depletes_from first exceeds the net kg of all earlier loads. It is '' when the last load has not been touched. Days left is computed in the UI as `floor(stock/avg)` |
| Traps | Only the Mesha concentrates appear here. The row is hidden when its days left is <=0 (`isTemporarilyHiddenFarmItem`) |

```sql
-- Same numbers as S1 for the concentrates; last load:
SELECT farm_label, feed_item_key, batch_no, purchase_date, stock_kg, vendor, total_cost, per_kg_cost
FROM feed_purchases WHERE tenant_id='00000000-0000-4000-8000-000000000001' AND delivery_status='reached'
  AND feed_item_key IN ('mesha_adult_concentrate','mesha_kids_concentrate')
ORDER BY farm_label, feed_item_key, depletes_from DESC, purchase_date DESC, batch_no DESC;
```
stg value (24/09/2026):
- CBE Adult: 2877.9 kg, 201.1/day, weekly 1407.5. Last load #359, 22/09, 1700 kg, ₹69,530 @ ₹40.90.
- CPT Adult: 1825.4 kg, weekly 884.8, #361.
- CBE Kids: 6369.3 kg, weekly 3160.3, #362, 4500 kg ₹1,84,050.
- CPT Kids: 5221.8 kg, weekly 2619.4, #360.
- The "consuming since" column is empty for all 4 rows, because the older loads are still being fed.

Q: "When was the last concentrate load and at what rate?" / "Last concentrate load kab aaya, kitne ka?" · "Weekly concentrate need?" / "Hafte ka concentrate kitna chahiye?"

## S3 Stock > Next 7 days requirement & cost
Columns: Farm, Item, kg/day, Required kg (7d), Stock kg, Shortfall kg, ₹/kg, Required ₹.
| | |
|---|---|
| Endpoint | `sections=forecast` -> `.forecast[]` |
| Code | `analytics.go:1844` `stockForecastSQL`, run by `stockForecast` :2728 (`$3 = domain.StockForecastDays = 7`, `domain/analytics.go:893`) |
| Formula | Keyed on consumption at (park, item). avg = the last 3 days on which that item appeared. The item is kept only if it had kg>0 on one of the park's last 3 feed days. `required = avg*7`. `stock` = per-ITEM balance (**no family merge, no override**). `shortfall = GREATEST(required - stock, 0)`. `₹/kg` = the latest reached load's `per_kg_cost`, else `total_cost/stock_kg`. `required_cost = required*₹/kg` |
| Traps | Farm names come from `locations.name` (Coimbatore/Channapatna), not CBE/CPT. Stock here can differ from the cards: for example CPT Adult shows 1738.4 here and 1825.4 on the card, because the leftover of the retired split members is not folded in |

SQL sketch: extract `stockForecastSQL` from analytics.go:
- `$1` = tenant
- `$2` = `'{}'::uuid[]`
- `$3` = `7`
- `feedPurchaseStockKgSQL` = `stock_kg`

stg value (24/09/2026):
- Coimbatore UHT: 7.0/day, required 49.0, stock 32.0, **shortfall 17.0**, ₹63.64/L, ₹3,118.
- Channapatna UHT: required 42.0, stock 45.0, shortfall 0.
- Coimbatore Kids Conc: required 3160.3, stock 6713.3, ₹1,29,255.
- Coimbatore Bhusa: required 4478.6, ₹76,136.
- Channapatna Kids Conc: ₹1,07,133.

Q: "How much feed do we need to buy this week?" / "Is hafte kitna feed kharidna padega?" · "Any shortfall next 7 days?" / "Agle 7 din mein kya kam padega?" · "What's next week's feed cost?" / "Agle hafte feed ka kharcha kitna?"

## S4 Stock > Loads table (load-by-load)
Endpoint: `GET /feed-analytics/stock-loads?park_id=&limit=&offset=` (`handler.go:84`). The table lists `feed_purchases` rows one by one. `delivery_status` includes in-transit loads, which show as rows but never count toward stock.
```sql
SELECT farm_label, feed_item_key, batch_no, purchase_date, delivery_status, stock_kg, consumed_at_import_kg, depletes_from, total_cost
FROM feed_purchases WHERE tenant_id='00000000-0000-4000-8000-000000000001' ORDER BY purchase_date DESC, batch_no DESC LIMIT 20;
```
stg value (24/09/2026): all 28 (farm, item) groups are `reached`. The latest loads, #359-#364, arrived 22-23/09.
Q: "Show last feed deliveries" / "Recent feed load kaunse aaye?" · "Is any load in transit?" / "Koi load raste mein hai?"

## Traps (summary)
1. Days left depends on the farm. Always name CBE/CPT, and never sum the two farms into a tenant runway.
2. Use `delivery_status='reached'` only. In-transit loads = 0 kg.
3. The family merge applies to the cards and the farm table, NOT to the forecast table.
4. The CBE 55 kg/day override is dormant: its key `concentrate` is retired and hidden.
5. The burn rate uses the last 3 days the feed APPEARED, not the last 3 calendar days. A feed that stopped being fed keeps its old rate.
6. UHT stock is reduced by the Milk Preparation litres on feeding_date (tomorrow), so today's balance already includes tomorrow's prep. The litres come from the operator's entry, not from the 800/1200/400 ml formula.
7. The UI hides the `concentrate` card and hides concentrate cards or rows that show 0 days left.

## S5 Stock > Loads table derived columns (appended 24/09/2026): status · purchased/consumed/left kg · days consumed · days left · days said · gap
Endpoint `GET /feed-analytics/stock-loads?park_id&farm&feed_item_key&limit&offset` → app/analytics.go:165 → adapters/postgres/stock_loads.go:42 stockLoadsSQL; gap rule domain/stock_loads.go:20,143. UI features/feed/feed-stock-loads-table.tsx.
- purchased_kg = `stock_kg − consumed_at_import_kg` (reached loads); in-transit rows show `quantity_kg`, consumed 0, status `in_transit`.
- consumed_kg = FIFO: consumption per (park,item,day) = **locked**-sheet kg + external milk; running total per (farm,item) from the ledger start is split across loads ordered (depletes_from, purchase_date, batch_no); the load newest *as of that day* absorbs any overrun (left_kg can go negative → status stays `in_use`).
- status: left<0 → in_use; consumed ≥ purchased → finished (**hidden**); consumed>0 → in_use; else not_started. Retired feeds hidden.
- days_consumed = count of feed days that drew on the load. days_left = floor(running kg left of the family up to and incl. this load ÷ family burn rate) – the newest load equals the stock card's days left. burn rate = avg of the family's last 3 fed days, or the pinned override.
- days_said = buyer's `days_of_stock`; gap_days = days_said − days_consumed − own_days_left (own = this load's left ÷ rate). Positive = runs out sooner than promised.
- **Go-only**: the split-concentrate family merge (domain/stock_family_merge.go StockFamilyMerge) and rate overrides are Go constants passed as arrays ($7–$11); with them the SQL is stock_loads.go verbatim. Not reproducible ad-hoc without copying those lists.
Q: "Which load are we feeding from now?" / "abhi kaunsa load chal raha hai?" · "Did the last bhusa load last as long as promised?" / "bhusa load jitne din bola utna chala?"
