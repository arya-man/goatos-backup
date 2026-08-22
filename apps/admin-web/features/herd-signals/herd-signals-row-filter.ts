import type { HerdSignalItem } from "@/lib/api/herd-signals";
import type { KpiFilterKey } from "./params";

// weak_signal / missing_signal / low_battery have no dedicated query parameter on GET
// /herd-signals/live (the fixed contract exposes only movement_state, mapping_state, pattern and
// q). "moving" and "quiet" DO route through movement_state server-side (see herd-signals-board.tsx)
// and never reach this function. This filters only the rows already returned for the CURRENT page —
// it never recomputes a KPI number, which stays the server's `summary` aggregate everywhere it is
// displayed (herd-signals-kpis.tsx). A reader who wants every weak-signal tag across the whole
// fleet, not just this page, still has the option to sort/page manually; that gap is a real
// limitation of the current contract, not something this file should paper over by inventing a
// client-side full-table scan (which admin-web-request-reads-guard exists to catch).
export function matchesResidualKpi(item: HerdSignalItem, kpi: KpiFilterKey | undefined): boolean {
  if (kpi === "weak_signal") return item.signal_state === "weak";
  if (kpi === "missing_signal") return item.movement_state === "stale";
  if (kpi === "low_battery") return item.battery_state === "low";
  return true;
}
