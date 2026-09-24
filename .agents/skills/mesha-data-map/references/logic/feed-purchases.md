# Feed purchases & payments (/procurement/feed-purchases)

Metrics: purchase count · quantity kg · spend (total_cost) · bill total per load · per-kg landed cost · paid so far (running column vs instalment ledger) · balance / outstanding · payment status chip · delivery status chip · stock kg · farm/delivery filters · vendor totals · period totals.

Base tables (public schema, no ceo_ai view): `feed_purchases` (one row = one load), `feed_purchase_payments` (instalments). Farm is `farm_label` = 'CBE' (Coimbatore) / 'CPT' (Channapatna). Date column = `purchase_date` (IST business date, already a date). Money in rupees, numeric(14,2).

---
### 1. Purchase count, quantity, spend (header summary)
- Screen: header tags "N purchases · Quantity X kg · Spend ₹Y" (feed-purchases.tsx:105-107, 228-236); whole-filter, not the 25-row page.
- Endpoint: GET /procurement/feed-purchases?farm=&delivery=&limit=&offset= (adapters/http/feed_purchase_handler.go:52)
- Code: adapters/postgres/feed_purchase_repository.go:151 (`count(*), COALESCE(sum(quantity_kg),0), COALESCE(sum(total_cost),0)`); WHERE from buildFeedPurchaseFilter :86
- Formula: count rows; sum buying `quantity_kg` (NOT received weight); sum `total_cost` (NULL totals count as 0).
- Filters->SQL: farm chip CBE/CPT -> `farm_label = 'CBE'|'CPT'`; "All" -> none. Delivery chip -> `delivery_status = 'purchased'|'reached'`; "All loads" -> none. NO date, vendor or payment filter on screen.
- Traps: screen is all-time; add a `purchase_date` window yourself for period questions. 40 sheet-import rows have NULL total_cost (quantity counted, spend not). No soft-delete column.
- SQL: `SELECT count(*), sum(quantity_kg), sum(total_cost) FROM public.feed_purchases;`
  stg 24/09/2026: 243 loads, 846,790.695 kg, ₹1,78,82,812.08
- This month: `... WHERE purchase_date >= date_trunc('month', now() AT TIME ZONE 'Asia/Kolkata')::date` -> stg 24/09/2026: 16 loads, 63,477 kg, ₹19,20,169.36
- CEO asks: "How much feed did we buy this month?" / "Total feed spend so far at Coimbatore?" / "Is mahine kitna feed kharida?" / "Channapatna ka feed kharcha kitna hua?"

### 2. Bill total per load (total_cost)
- Screen: "Total" column (feed-purchases.tsx:292) and drawer cell (feed-purchase-drawer.tsx:464).
- Code: domain/feed_purchase.go:549 TotalOrSplitSum; insert repository.go:360-383; edit :731
- Formula: entered total_cost if given, else feed_cost + transport_cost + loading_cost + unloading_cost (non-null parts only); NULL if no part given. Stored, not recomputed on read.
- Traps: split columns may not add up to total on sheet rows (total was entered directly) - always use total_cost, never re-sum the parts.
- SQL: `SELECT purchase_date, farm_label, feed_item_label, batch_no, total_cost FROM public.feed_purchases ORDER BY purchase_date DESC LIMIT 10;`
- CEO asks: "What did the last maize load cost?" / "Last load ka bill kitna tha?"

### 3. Per-kg landed cost (per_kg_cost)
- Screen: "Per kg" column (feed-purchases.tsx:295, 2 decimals), drawer :465.
- Code: domain/feed_purchase.go:93 DeriveFeedPerKgCost; recomputed on delivery weight entry repository.go:851
- Formula: total_cost / (reached_weight_kg if >0 else quantity_kg); NULL when no total. Stored column.
- Traps: shrinkage raises the rate (received kg divisor). For a blended rate never average per_kg_cost: use `sum(total_cost)/sum(kg)` over rows with total_cost NOT NULL.
- SQL: `SELECT round(sum(total_cost)/sum(quantity_kg),2) FROM public.feed_purchases WHERE total_cost IS NOT NULL;` (buying-kg basis)
  stg 24/09/2026: ₹24.35/kg
- CEO asks: "Average feed rate per kg?" / "Per kg feed kitne ka pad raha hai?" / "Which feed is costliest per kg?"

### 4. Paid so far
- Screen: drawer "Paid so far" = `payment_released` (feed-purchase-drawer.tsx:558-559); instalment table below it (:576-584).
- Endpoint: POST /procurement/feed-purchases/{id}/payments (handler :57)
- Code: repository.go:573-590 — inserts instalment, then `payment_released = old payment_released + amount`.
- Formula: running column `feed_purchases.payment_released`. Ledger = `sum(feed_purchase_payments.amount_rupees)`.
- Traps: the running column is the truth the screen shows. Sheet history has payment_released but NO instalment rows, so summing the ledger badly understates payments. On stg 2 loads have released > ledger sum (released seeded from sheet, then instalments added). Status-only "Paid" loads may have NULL released.
- SQL: `SELECT sum(payment_released) FROM public.feed_purchases;` ; ledger: `SELECT count(*), sum(amount_rupees), count(DISTINCT feed_purchase_id) FROM public.feed_purchase_payments;`
  stg 24/09/2026: payment_released ₹1,32,07,492 (all loads); ledger 21 instalments, ₹16,05,254.00 across 17 loads
- CEO asks: "How much have we paid vendors for feed?" / "Vendor ko ab tak kitna payment gaya?"

### 5. Balance / outstanding
- Screen: "Balance" column (feed-purchases.tsx:311) and drawer (feed-purchase-drawer.tsx:564-565); backend-derived.
- Code: domain/feed_purchase.go:217 PaymentBalance
- Formula: status 'Paid' -> 0; else total_cost NULL -> NULL ("—"); else greatest(total_cost - coalesce(payment_released,0), 0). Never negative.
- Traps: Paid overrides arithmetic (most sheet Paid rows have released < total). Overpayment shows 0, not credit.
- SQL:
  ```sql
  SELECT sum(CASE WHEN payment_status='Paid' THEN 0 WHEN total_cost IS NULL THEN NULL
             ELSE greatest(total_cost - coalesce(payment_released,0),0) END) AS outstanding,
         count(*) FILTER (WHERE payment_status='Pending') AS pending_loads
  FROM public.feed_purchases;
  ```
  stg 24/09/2026: ₹22,71,081.22 outstanding on 31 Pending loads. Top vendor: Navaladi poultry farm and cattle feeds ₹4,63,755.02.
- CEO asks: "How much do we owe feed vendors?" / "Which vendor has the biggest pending bill?" / "Feed vendors ka kitna baaki hai?" / "Kis vendor ka payment pending hai?"

### 6. Payment status chip
- Screen: chip in list (feed-purchases.tsx:302) and drawer (feed-purchase-drawer.tsx:552); tone from page contract.
- Code: domain/feed_purchase.go:35-41 (only 'Paid','Pending'); chips backend/internal/adminui/app/service.go:9491 (Paid=ok green, Pending=warn amber); unknown/empty -> muted "—" (feed-purchase-format.ts:19). Auto-derive on instalment: domain/feed_purchase.go:453 — Paid when released >= total_cost - 0.005, else Pending; unknown total keeps current. Manual override: PUT /procurement/feed-purchases/{id}/payment-status (repository.go:631).
- Traps: NO "partly paid", "unpaid" or "overpaid" states. Partly paid = Pending with payment_released > 0. Empty string ('' not NULL) on 40 sheet rows = no status recorded. Status can be set manually, so Paid does not prove released >= total.
- SQL: `SELECT payment_status, count(*) FROM public.feed_purchases GROUP BY 1;`
  stg 24/09/2026: Paid 172, Pending 31, '' 40. Released > total (would-be overpaid): 6 loads.
- CEO asks: "How many feed bills are unpaid?" / "Kitne bills pending hain?"

### 7. Delivery status chip & stock kg
- Screen: delivery chip "In transit"/"Delivered" + reached date (feed-purchases.tsx:280-285); delivery filter chips.
- Code: domain/feed_purchase.go:48-71 labels; StockKg :241 (mirrors generated column `feed_purchases.stock_kg`); reached date read repository.go:48 `COALESCE(reached_on, purchase_date)` when reached.
- Formula: stock kg = NULL on screen while 'purchased' (the DB generated column `stock_kg` is 0, not NULL); reached -> coalesce(reached_weight_kg, quantity_kg).
- SQL: `SELECT delivery_status, count(*), sum(stock_kg) FROM public.feed_purchases GROUP BY 1;`
  stg 24/09/2026: all 243 'reached', 0 in transit.
- CEO asks: "Any feed loads still on the road?" / "Kitna feed raste mein hai?"

### 8. Vendor totals (not a screen filter)
- Screen: vendor column only; vendor datalist = distinct vendors by latest purchase, LIMIT 100 (repository.go:199-204).
- Traps: free text; same vendor may be spelled differently - group on `btrim(lower(vendor))` and show variants.
- SQL: `SELECT vendor, count(*), sum(total_cost) FROM public.feed_purchases GROUP BY 1 ORDER BY 3 DESC NULLS LAST LIMIT 5;`
  stg 24/09/2026: Navaladi poultry farm and cattle feeds 36 loads ₹34,41,566.38; Mishka Cattle Feeds 40 ₹28,64,719; Vinod 47 ₹27,29,050
- CEO asks: "Who is our biggest feed supplier?" / "Sabse zyada feed kis vendor se liya?"

### 9. Ledger list order & range
- Order: purchase_date DESC, feed_purchase_id; page 25 (max 100, offset <= 10000) (repository.go:123, domain :110-127).
- Range on stg 24/09/2026: 24/01/2025 to 22/09/2026; `entry_source` 'app' (24 loads) vs 'sheet_import' (219).
- Dates in answers: DD/MM/YYYY; farm names Coimbatore / Channapatna.
