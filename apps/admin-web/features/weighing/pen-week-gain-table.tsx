import type { AdminUiTableContract } from "@/lib/admin-ui-contract";
import { WeekGainTable, type WeekGainCell, type WeekGainLabels, type WeekGainRow } from "./week-gain-table";

/**
 * One pen-week cell as the backend served it (maintainer request 2026-09-08). The server
 * component hands the rows down already resolved: the pen label is the backend-composed
 * operational location, rendered verbatim, and the gain is a number so the cell can round it.
 */
export type PenWeekGainPoint = {
  locationId: string;
  partitionLabel: string;
  park: string;
  pen: string;
  weekStart: string;
  animals: number;
  gainGPerDay: number;
};

export type PenWeekGainLabels = WeekGainLabels;

/**
 * One row per pen, one column per calendar week -- the shared week pivot, keyed on the pen's
 * (location, partition) identity rather than its name, because a name repeats across parks. The
 * two FIXED columns (park, pen) come from the page's `pen-week-gain` contract; the first is the
 * park and the second, the pen, is the emphasised name column. Rows are grouped by park and
 * ordered by pen, as served.
 */
export function PenWeekGainTable({
  contract,
  points,
  labels,
}: {
  contract: AdminUiTableContract;
  points: readonly PenWeekGainPoint[];
  labels: PenWeekGainLabels;
}) {
  const [parkKey, penKey] = contract.columns.filter((column) => column.visible).map((column) => column.key);
  const rows: WeekGainRow[] = [];
  const seen = new Set<string>();
  const cells: WeekGainCell[] = points.map((point) => {
    const key = `${point.locationId}::${point.partitionLabel}`;
    if (!seen.has(key)) {
      seen.add(key);
      rows.push({ key, fixed: { [parkKey ?? "park"]: point.park, [penKey ?? "shed"]: point.pen } });
    }
    return { rowKey: key, weekStart: point.weekStart, animals: point.animals, gainGPerDay: point.gainGPerDay };
  });
  return <WeekGainTable contract={contract} rows={rows} cells={cells} labels={labels} className="wt-penweek" emphasisKey={penKey} />;
}
