# Sales DB snapshot — 2026-08-17

`sales-db.json` is a ONE-TIME export of the maintainer's "Sales DB" Google Sheet
(sheet id `1ACQJIQIZRkoQx76HtO_vORsGCVekHsI6TFFH-vohT8I`), exported 2026-08-17.
After import, Postgres is canonical and the sheet is not read again — this is a
bootstrap snapshot, not a sync.

## What it contains

| Envelope key | Rows | Lands in |
|---|---|---|
| `deals` | 63 | `sales_deals` — the sales ledger (Sheep/Goat/Manure across CBE and CPT) |
| `buyer_leads` | 208 | `sales_buyer_leads` — buyer demand pipeline |
| `fpo_leads` | 53 | `sales_fpo_leads` — FPO demand pipeline |
| `sold_animal_tags` | 131 | `sales_sold_animal_tags` — per-animal tag evidence behind sold deals |
| `weight_audit` | 81 | `sales_weight_audit` — video weight vs book weight evidence |
| `market_benchmarks` | 10 | `sales_market_benchmarks` — comparable market per-kg quotes |

Buyer and lead **phone numbers are deliberately omitted** from the export; the
snapshot carries names and places only.

## How to (re-)import

```bash
cd backend
go run ./cmd/import-sales-db \
  -database-url "postgres://..." \
  -tenant <tenant-uuid> \
  -fixture ../fixtures/sales-db-2026-08-17/sales-db.json
```

Re-running is safe: every row is keyed on `(tenant_id, source_row_no)` — its
1-based position in the exported tab — and updated in place, never duplicated.
The loader uses `DisallowUnknownFields`, so a drifted fixture fails loudly
instead of importing partially. `market_price_per_kg` is parsed out of the
market prose (`'Chennai -Sheep - 370 Rs Per kg'` → `370`) at import time.

Use `-dry-run` to parse and validate without a database.
