# Plan-proof backlog

Statements changed by #415 that carry `scale-guard:plan-proof-exempt: PENDING`.
They are NOT proven plan-neutral. Each was measured under 500 ms on STG-size
data, but has no ~500k-row at-scale plan test yet. Add a
`Test*QueryPlanUsesIndexesAtScale` for each, then delete its PENDING comment
and its row here.

| Package | Statement |
|---|---|
| calendar | `calendarCanonicalEventsCTE` (canonical_read.go) |
| calendar | `calendarCanonicalListSQL` (canonical_read.go) |
| calendar | `calendarHistorySQL` (repository.go) |
| herdsignals | `GetBatteryHistory` inline SQL, 2 statements (repository.go) |
| procurement | `loadStockWeightSQL` (loadwise_stock_weight.go) |
| procurement | `saleLineShareCTEs` (sale_line_share.go) |
| vaccinationexecution | `commandBoardShedDoseSQL` (commandboard_sql.go) |
| vaccinationexecution | `executionClassifiedCTE` (repository.go) |
| vaccinationexecution | `vaccinationExecutionSQL` (repository.go) |
| vaccinationexecution | `cardSummariesSQL` (repository.go) |
| weighing | `listAlertsSQL` (alerts.go) |
| weighing | `shedPartitionWeightDemographicsSQL` (weight_demographics.go) |
| weighing | `getWeightDemographicsUncached` inline SQL (weight_demographics.go) |

Seeding note: a 60k-goat bulk insert is slow because of the per-row trigger on
`goats`. Load goats in chunks.
