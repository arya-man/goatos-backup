import type { AdminUiTableContract } from "@/lib/admin-ui-contract";
import { WeekGainTable, type WeekGainCell, type WeekGainLabels, type WeekGainRow } from "./week-gain-table";

/**
 * One load-week cell as the backend served it (maintainer request 2026-09-14, "Time-wise ADG for
 * each shed/load"): the farm's own load number and supplier, rendered verbatim, and the gain as a
 * number so the cell can round it.
 */
export type LoadWeekGainPoint = {
  loadRef: string;
  source: string;
  weekStart: string;
  animals: number;
  gainGPerDay: number;
};

export type LoadWeekGainLabels = WeekGainLabels;

/**
 * One row per purchased load, one column per calendar week -- the shared week pivot, keyed on the
 * load reference. The two FIXED columns (load, source) come from the page's `load-week-gain`
 * contract; the load number is the emphasised name column. Rows are ordered by load, as served.
 */
export function LoadWeekGainTable({
  contract,
  points,
  labels,
}: {
  contract: AdminUiTableContract;
  points: readonly LoadWeekGainPoint[];
  labels: LoadWeekGainLabels;
}) {
  const [loadKey, sourceKey] = contract.columns.filter((column) => column.visible).map((column) => column.key);
  const rows: WeekGainRow[] = [];
  const seen = new Set<string>();
  const cells: WeekGainCell[] = points.map((point) => {
    const rowKey = `${point.loadRef}\u0000${point.source}`;
    if (!seen.has(rowKey)) {
      seen.add(rowKey);
      rows.push({ key: rowKey, fixed: { [loadKey ?? "load"]: point.loadRef, [sourceKey ?? "source"]: point.source } });
    }
    return { rowKey, weekStart: point.weekStart, animals: point.animals, gainGPerDay: point.gainGPerDay };
  });
  return <WeekGainTable contract={contract} rows={rows} cells={cells} labels={labels} className="wt-loadweek" />;
}
