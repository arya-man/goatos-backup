Sections: Model & ledger | On-hand position (ceo_ai view) | Items master | Available / FEFO lot | Movements ledger | Reorder flag | Vaccination > Inventory vaccine progress cards | Traps

# INVENTORY / STORES (vaccines + medicines)

## Model & ledger
- Tables: `public.inventory_items` (item master), `public.inventory_stock` (one row per lot: quantity_in_stock, quantity_reserved, expiry_date, status, location_id), `public.inventory_stock_movements` (append-only ledger: movement_type receive/reserve/consume/release/adjust/expire/transfer_*, quantity, occurred_at, batch_id, idempotency_key). Backend: `backend/internal/inventory` (domain/types.go:63-69).
- No inventory HTTP routes and no stores/stock admin-web screen exist. Stock is written only by vaccination drives (reserve/consume) via app/reserve.go, app/consume.go.
- Balance math (repository.go:310 RecordMovementAndAdjustBalances, :977 AdjustBalances): each movement writes a ledger row AND applies deltas to the lot: quantity_in_stock += inDelta, quantity_reserved += reservedDelta. on_hand = quantity_in_stock; available = quantity_in_stock - quantity_reserved.
- Feed stock (kg, days-left) is NOT here: it is computed from feed purchases (see feed logic card).

## On-hand position
View: `ceo_ai.inventory_stock_position` = sum(quantity_in_stock) per tenant x item x location, excluding status 'retired'; park_label = parent location name; last_reconciled_at = last reconcile/adjust/count movement else stock updated_at.
Traps: includes expired/quarantined lots (only 'retired' excluded); reserved stock is NOT subtracted; reorder_flag is always NULL.
```sql
SELECT item_label, category, park_label, stock_on_hand, unit, last_reconciled_at
FROM ceo_ai.inventory_stock_position
WHERE tenant_id='00000000-0000-4000-8000-000000000001' ORDER BY item_label;
```
stg value (24/09/2026): 0 rows (inventory_stock empty).
Q: "How much vaccine stock do we have?" / "kitna vaccine stock bacha hai?"

## Items master
```sql
SELECT category, status, count(*) FROM public.inventory_items
WHERE tenant_id='00000000-0000-4000-8000-000000000001' GROUP BY 1,2;
```
stg value (24/09/2026): medicine 34 active, vaccine 7 active (41 items, none stocked).
Q: "How many medicines/vaccines are in the item list?" / "store me kitne item register hain?"

## Available / FEFO lot (what a drive would use)
Code: backend/internal/inventory/adapters/postgres/sqlc/query.sql:12 PickFEFOLot; :34 ResolveStockLocation (walks shed -> park -> farm, up to 8 levels).
Formula: lots WHERE status='active' AND quantity_in_stock > quantity_reserved AND (expiry_date IS NULL OR expiry_date >= CURRENT_DATE), earliest expiry first; available = in_stock - reserved.
```sql
SELECT i.name, s.location_id, s.lot_code, s.expiry_date, s.quantity_in_stock - s.quantity_reserved AS available
FROM public.inventory_stock s JOIN public.inventory_items i USING (item_id)
WHERE s.tenant_id='00000000-0000-4000-8000-000000000001' AND s.status='active'
  AND s.quantity_in_stock > s.quantity_reserved AND (s.expiry_date IS NULL OR s.expiry_date >= (now() AT TIME ZONE 'Asia/Kolkata')::date)
ORDER BY i.name, s.expiry_date NULLS LAST;
```
stg value (24/09/2026): 0 rows.
Q: "Which vaccine lots expire first / are usable?" / "kaunsa vaccine lot pehle expire hoga?"

## Movements ledger (receipts / issues)
Formula: receipts = movement_type='receive'; issues = 'consume'; reserve/release move quantity_reserved only. Date = occurred_at (IST).
```sql
SELECT movement_type, count(*), sum(quantity)
FROM public.inventory_stock_movements
WHERE tenant_id='00000000-0000-4000-8000-000000000001'
  AND (occurred_at AT TIME ZONE 'Asia/Kolkata')::date >= date_trunc('month', now() AT TIME ZONE 'Asia/Kolkata')::date
GROUP BY 1;
```
stg value (24/09/2026): 0 movements (all time).
Q: "How much stock was received/issued this month?" / "is mahine store me kitna aaya aur kitna gaya?"

## Reorder flag
Not implemented: no min/reorder level column; view returns reorder_flag NULL. Answer "reorder levels are not configured"; do not invent.
Q: "Which items need reorder?" / "kaunsa item reorder karna hai?"

## Vaccination > Inventory vaccine progress cards
Screen: admin-web `/vaccination` operations (features/preventive-care-vaccination/inventory-vaccine-progress.tsx). These are PC-care TASKS whose category='inventory_vaccine', not stock quantities.
Endpoint: GET /app/pc-care/tasks?date=<IST today>&park_id=&category=inventory_vaccine&limit=100 (drained up to 20 pages).
Code/Formula (tsx:13,54,116-142): rows = work_state IN (scheduled, delayed) OR (work_state='completed' AND due_business_date=today IST). Cards: Current = rows; Overdue = work_state='delayed'; Waiting verifier = status='pending_verification'; Done today = status='completed' OR work_state='completed'. Requirement line = pc_care_task_inventory_requirements (required_doses x vaccine).
Filters->SQL: scope picker park -> park_id; date = IST today.
```sql
SELECT count(*) FILTER (WHERE work_state IN ('scheduled','delayed')) current_active,
       count(*) FILTER (WHERE work_state='delayed') overdue,
       count(*) FILTER (WHERE status='pending_verification' AND work_state IN ('scheduled','delayed')) waiting_verifier
FROM public.pc_care_tasks
WHERE tenant_id='00000000-0000-4000-8000-000000000001' AND category='inventory_vaccine';
```
stg value (24/09/2026): 0 / 0 / 0 (all 61 inventory_vaccine tasks are work_state canceled).
Q: "Any vaccine stock tasks overdue?" / "vaccine stock ke kitne kaam overdue hain?"

## Traps
- stg has items but zero stock lots and zero movements: every quantity answer is 0 / "no stock recorded", not "out of stock".
- Stock is held at park/farm; drives are shed-scoped and roll up to ancestors.
- Units: quantity_unit per lot (doses/ml); never sum across units.
