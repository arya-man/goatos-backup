import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import type { AdminUiTableContract } from "@/lib/admin-ui-contract";
import { fmtDate } from "@/lib/format";

/**
 * One cell of a week pivot as the backend served it: which row it belongs to, which week, how
 * many animals stand behind the figure, and the figure itself as a number so the cell can round it.
 */
export type WeekGainCell = {
  rowKey: string;
  weekStart: string;
  animals: number;
  gainGPerDay: number;
};

/** One pivot row: its key plus the value of each FIXED (contract-declared) column, by column key. */
export type WeekGainRow = {
  key: string;
  fixed: Record<string, string>;
};

export type WeekGainLabels = {
  ariaLabel: string;
  /** The cell for a week the row was not weighed twice: absence, never a zero. */
  blank: string;
  unit: string;
  animals: string;
  empty: React.ReactNode;
};

/**
 * PIVOT: one row per subject (a pen, a purchased load), one column per calendar week of the
 * selected period, daily gain in each cell. The FIXED columns come from the page's table contract,
 * so their labels are backend copy; the week columns are DATA -- the weeks the response actually
 * carries, ascending -- and are headed by the week's Monday, which is a date, not a label. The
 * first fixed column is the row's name and is emphasised; the others are context.
 *
 * A row with no gain in a week has NO cell value: the backend leaves the point out, and this table
 * renders the backend's blank marker rather than 0 g/day, which would claim the subject stopped
 * growing in a week nobody looked. Rows render in the order given, which is the order served.
 */
export function WeekGainTable({
  contract,
  rows,
  cells,
  labels,
  className,
  emphasisKey,
}: {
  contract: AdminUiTableContract;
  rows: readonly WeekGainRow[];
  cells: readonly WeekGainCell[];
  labels: WeekGainLabels;
  className: string;
  emphasisKey?: string;
}) {
  const fixed = contract.columns.filter((column) => column.visible);
  const weeks = [...new Set(cells.map((cell) => cell.weekStart))].sort();
  const byRow = new Map<string, Map<string, WeekGainCell>>();
  for (const cell of cells) {
    let row = byRow.get(cell.rowKey);
    if (!row) {
      row = new Map();
      byRow.set(cell.rowKey, row);
    }
    row.set(cell.weekStart, cell);
  }
  const columnCount = fixed.length + weeks.length;
  const nameKey = emphasisKey ?? fixed[0]?.key;

  return (
    <Table className={`tbl ${className}`} aria-label={labels.ariaLabel}>
      <TableHead>
        <TableRow>
          {fixed.map((column) => (
            <TableCell component="th" key={column.key} scope="col">
              {column.label}
            </TableCell>
          ))}
          {weeks.map((week) => (
            <TableCell component="th" key={week} scope="col" style={{ textAlign: "right", whiteSpace: "nowrap" }}>
              {fmtDate(week)}
            </TableCell>
          ))}
        </TableRow>
      </TableHead>
      <TableBody>
        {rows.length === 0 ? (
          <TableRow>
            <TableCell colSpan={Math.max(columnCount, 1)}>{labels.empty}</TableCell>
          </TableRow>
        ) : (
          rows.map((row) => {
            const rowCells = byRow.get(row.key);
            return (
              <TableRow key={row.key}>
                {fixed.map((column) => (
                  <TableCell key={column.key}>
                    {column.key === nameKey ? <b>{row.fixed[column.key] ?? ""}</b> : (row.fixed[column.key] ?? "")}
                  </TableCell>
                ))}
                {weeks.map((week) => {
                  const cell = rowCells?.get(week);
                  return (
                    <TableCell key={week} style={{ textAlign: "right", whiteSpace: "nowrap" }}>
                      {cell ? (
                        <span title={`${cell.animals.toLocaleString("en-IN")} ${labels.animals} · ${labels.unit}`}>
                          {Math.round(cell.gainGPerDay).toLocaleString("en-IN")} g
                        </span>
                      ) : (
                        <span className="muted">{labels.blank}</span>
                      )}
                    </TableCell>
                  );
                })}
              </TableRow>
            );
          })
        )}
      </TableBody>
    </Table>
  );
}
