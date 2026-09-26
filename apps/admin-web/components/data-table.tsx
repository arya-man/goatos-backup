"use client";

import { useState } from "react";
import {
  flexRender,
  getCoreRowModel,
  getSortedRowModel,
  useReactTable,
  type ColumnDef,
  type SortingState,
} from "@tanstack/react-table";

import Box from "@mui/material/Box";
import Checkbox from "@mui/material/Checkbox";
import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import TableCell from "@mui/material/TableCell";
import MuiTableFooter from "@mui/material/TableFooter";
import TableRow from "@mui/material/TableRow";

import type { AdminUiTableContract } from "@/lib/admin-ui-contract";
import { EmptyContent } from "@/components/minimal/empty-content";
import { Scrollbar } from "@/components/minimal/scrollbar";
import { TableHeadCustom, type TableHeadCellProps } from "@/components/minimal/table";

/**
 * The shared headless table body for admin-web worklists.
 *
 * TanStack Table owns the column model and the sort state; the markup is the MUI Minimal template's
 * table kit (Scrollbar, Table, TableHeadCustom, TableRow hover, TableNoData-style empty cell), as in
 * the template user list.
 *
 * THREE RULES THIS COMPONENT EXISTS TO KEEP, each of which a hand-rolled table on each page has
 * already broken at least once in this repo:
 *
 * 1. COLUMNS ARE BACKEND-OWNED. Callers build their `ColumnDef[]` from
 *    `table(pageContract, id).columns` — key, label, `visible` and `sortable` all come from the
 *    compiled contract. `columnsFromContract` below is the only supported way to do that, so a
 *    page cannot quietly introduce a local label or a sort affordance the backend never declared.
 *
 * 2. SORTING IS PAGE-LOCAL AND MUST STAY HONEST. Both current callers paginate on the SERVER
 *    (URL offset/limit), so `manualPagination` is on and TanStack never slices rows. Sorting
 *    reorders the rows already on screen and NOTHING else — it does not refetch, and it must never
 *    be used to compute a footer total, a KPI, or any figure presented as whole-result truth.
 *    Whole-result ordering needs a backend sort parameter; it does not exist on these endpoints
 *    yet, which is exactly why only the columns the contract marks `sortable` get an affordance.
 *
 * 3. A COLUMN MAY SPAN ITS NEIGHBOUR. `meta.colSpan` on one column plus `meta.spanned` on the next
 *    reproduces a merged body cell (the feed ration grid's effective window is one visual unit
 *    across the contract's `valid_from` + `valid_to` pair) while both header labels still render,
 *    so the contract's column list and the rendered header stay identical.
 *
 * 4. A ROW MAY OPEN IN PLACE. `expandable` renders detail rows directly under a body row the
 *    caller marks open. Every body row gets its own `<tbody>` so the row and its detail rows travel
 *    as one group: sorting moves them together, and the detail rows are ordinary `<tr>`s in the
 *    SAME column grid, so each detail value sits under the parent column it belongs to. Open/closed
 *    is CLIENT-LOCAL state owned by the caller (the same rule as drawers: an ordinary open/close
 *    click never navigates or refetches), and a detail row carries no toggle handler, so clicks
 *    inside it never close the parent.
 */

/**
 * The row-identity cell: the one column a reader scans down to find their row.
 *
 * Exported here rather than left to each page so "Pen A1 / Shed 2 · Park North" is one shape
 * everywhere — a bold primary line and a muted secondary line in ONE cell, instead of the two
 * columns a table grows when identity is spread out, which is exactly what makes these tables
 * scroll sideways on a phone.
 */
export function IdentityCell({ primary, secondary, lead }: { primary: React.ReactNode; secondary?: React.ReactNode; lead?: React.ReactNode }) {
  return (
    <span className="kit-idcell">
      {lead ? <span className="kit-idcell-lead">{lead}</span> : null}
      <span className="kit-idcell-copy">
        <span className="kit-idcell-primary">{primary}</span>
        {secondary === undefined || secondary === null ? null : <span className="kit-idcell-secondary">{secondary}</span>}
      </span>
    </span>
  );
}

/**
 * Row selection. The caller owns the selected set — the table never holds selection state, because
 * selection outlives the table (a bulk action lives in a toolbar above it) and a table that owned
 * it would silently drop the set on every sort.
 */
export type DataTableSelection<Row> = {
  selectedIds: ReadonlySet<string> | readonly string[];
  onToggle: (row: Row, next: boolean) => void;
  /** Header checkbox. Absent = no select-all. `rows` is the rows currently rendered. */
  onToggleAll?: (rows: Row[], next: boolean) => void;
  /** Accessible name of the header checkbox; backend copy from the caller. */
  selectAllLabel?: string;
  ariaLabel?: (row: Row) => string;
};

export type DataTableExpandable<Row> = {
  isOpen: (row: Row) => boolean;
  onToggle: (row: Row) => void;
  /**
   * One or more `<tr>` elements rendered after the open row, inside its `<tbody>`. Each must carry
   * exactly the table's visible columns (use `contract.columns.filter(c => c.visible)`) so the
   * cells align; the caller sets the DOM id its toggle names via aria-controls.
   */
  render: (row: Row) => React.ReactNode;
};
export type DataTableColumnMeta = {
  /** Body-cell colSpan. The following `meta.spanned` column renders no body cell. */
  colSpan?: number;
  /** This column's body cell is covered by the previous column's colSpan. */
  spanned?: boolean;
  /** Applied to both the header cell and every body cell in this column. */
  align?: "left" | "right";
  /** Extra className for this column's body cells. */
  cellClassName?: string;
  /**
   * Extra className for this column's HEADER cell.
   *
   * Separate from the body-cell className on purpose: most callers only want a body modifier such
   * as `num`, which would be wrong on a `<th>`. But a column that is HIDDEN at some viewport has
   * to hide its header with its body -- a `display:none` that reaches only the `<td>`s leaves the
   * header row one cell longer than every body row, so the labels after it sit over the wrong
   * column (found 2026-09-18 on /tasks at phone width, where the urgency column is dropped).
   */
  headerClassName?: string;
  /** Extra inline style for this column's body cells. */
  cellStyle?: React.CSSProperties;
};

/**
 * Builds `ColumnDef`s from the compiled table contract, in the contract's own column order.
 *
 * `cells` is keyed by the contract's column KEY, so a renderer can never drift onto a column the
 * backend did not declare: a key with no cell renderer is a hard error rather than a blank column,
 * and a cell renderer for a key the contract dropped simply never runs. Hidden columns
 * (`visible: false`) are excluded here, which is what makes the header, the body and the
 * `colSpan` on an empty-state row agree on one number.
 */
export function columnsFromContract<Row>(
  contract: AdminUiTableContract,
  cells: Record<string, { cell: (row: Row) => React.ReactNode; meta?: DataTableColumnMeta; sortValue?: (row: Row) => string | number | undefined }>,
): ColumnDef<Row>[] {
  return contract.columns
    .filter((column) => column.visible)
    .map((column) => {
      const spec = cells[column.key];
      if (!spec) {
        throw new Error(`data-table: no cell renderer for contract column "${column.key}"`);
      }
      return {
        id: column.key,
        header: column.label,
        // The sort value is separated from the rendered cell on purpose: several cells render a
        // fallback label ("No stage") or a whole component, and sorting on that rendered output
        // would order by the fallback copy instead of the underlying value.
        accessorFn: spec.sortValue ?? (() => ""),
        enableSorting: column.sortable,
        // A row whose sort value is ABSENT (no gain, never weighed) sits last whichever way the
        // column is sorted: it is missing, not zero, and must never land between two measured rows.
        sortUndefined: "last",
        meta: spec.meta,
        cell: (ctx) => spec.cell(ctx.row.original),
      } satisfies ColumnDef<Row>;
    });
}

export function DataTable<Row>({
  columns,
  data,
  getRowId,
  ariaLabel,
  className,
  empty,
  footer,
  initialSorting,
  expandable,
  selection,
  rowActions,
  dense,
  rowActionsHeader,
  serverSort,
}: {
  columns: ColumnDef<Row>[];
  data: Row[];
  getRowId: (row: Row) => string;
  ariaLabel: string;
  className?: string;
  /** Shown in a single full-width cell when `data` is empty. */
  empty: React.ReactNode;
  /** Whole-result totals row. Shown verbatim inside `<tfoot>`; never derived from `data`. */
  footer?: React.ReactNode;
  initialSorting?: SortingState;
  /** Rule 4 above: rows that open an in-place detail row. */
  expandable?: DataTableExpandable<Row>;
  /** Leading checkbox column. Selection state is the caller's (see DataTableSelection). */
  selection?: DataTableSelection<Row>;
  /**
   * Trailing per-row control column — in practice a `<RowMenu>`. Placed in its own cell after the
   * contract's columns so the contract's column list stays exactly what the backend declared, and
   * the affordance still travels with the row.
   */
  rowActions?: (row: Row) => React.ReactNode;
  /** Header label for the actions column; visually hidden by default. */
  rowActionsHeader?: React.ReactNode;
  /** Tightened row rhythm, driven by the table toolbar's dense toggle. */
  dense?: boolean;
  /**
   * WHOLE-RESULT sorting ("sort all rows", 2026-09-25). When set, a header click does NOT reorder
   * the rows this component holds -- that sorted one page out of hundreds -- but hands the new
   * order to the page, which asks the backend for it. `sorting` is the order the rows ARRIVED in;
   * `pending` marks the table busy in place while the re-ordered page is on its way.
   */
  serverSort?: {
    sorting: SortingState;
    onChange: (next: SortingState) => void;
    pending?: boolean;
    /** Screen-reader suffix for a sortable header, e.g. "sort all rows". Backend copy. */
    sortLabel: string;
  };
}) {
  const [localSorting, setLocalSorting] = useState<SortingState>(initialSorting ?? []);
  const sorting = serverSort ? serverSort.sorting : localSorting;

  const table = useReactTable({
    data,
    columns,
    state: { sorting },
    onSortingChange: (updater) => {
      const next = typeof updater === "function" ? updater(sorting) : updater;
      if (serverSort) serverSort.onChange(next);
      else setLocalSorting(next);
    },
    getRowId,
    getCoreRowModel: getCoreRowModel(),
    // The server already ordered the whole set; re-sorting its page here would only disagree.
    manualSorting: Boolean(serverSort),
    getSortedRowModel: getSortedRowModel(),
    // The server owns the window. TanStack must not slice, count or page these rows.
    manualPagination: true,
  });

  const rows = table.getRowModel().rows;
  const selectedIds = selection
    ? selection.selectedIds instanceof Set
      ? selection.selectedIds
      : new Set(selection.selectedIds as readonly string[])
    : null;
  // Read on every render, never memoized on `table`: the table instance is identity-stable across
  // renders, so a memo keyed on it would keep an empty-state colSpan from a previous column set.
  const colCount = table.getVisibleLeafColumns().length + (selection ? 1 : 0) + (rowActions ? 1 : 0);

  const direction = sorting[0] ? (sorting[0].desc ? "desc" : "asc") : undefined;
  const headCells: TableHeadCellProps[] = [
    ...(selection && !selection.onToggleAll ? [{ id: "__select", label: "", sortable: false, width: 48 }] : []),
    ...(table.getHeaderGroups()[0]?.headers ?? []).map((header) => {
      const meta = header.column.columnDef.meta as DataTableColumnMeta | undefined;
      return {
        id: header.id,
        label: flexRender(header.column.columnDef.header, header.getContext()),
        sortable: header.column.getCanSort(),
        sortLabel: `${String(header.column.columnDef.header)} — ${serverSort ? serverSort.sortLabel : "sort this page"}`,
        align: meta?.align,
        className: meta?.headerClassName,
      } satisfies TableHeadCellProps;
    }),
    ...(rowActions ? [{ id: "__actions", label: rowActionsHeader ?? <span className="sr-only">Actions</span>, sortable: false, align: "right" as const, className: "kit-actcell" }] : []),
  ];

  // Template user-list anatomy: Scrollbar > Table (size follows the dense switch) > TableHeadCustom,
  // rows as TableRow hover, TableNoData-style empty cell. TanStack still owns order and sorting.
  return (
    <Box
      className={dense ? "kit-dense" : undefined}
      tabIndex={0}
      role="region"
      aria-label={ariaLabel}
      aria-busy={serverSort?.pending || undefined}
      // Whole-result sort in flight (main d660e4f4c): the rows dim in place and take no clicks until
      // the re-ordered page arrives.
      sx={{
        position: "relative",
        minWidth: 0,
        ...(serverSort?.pending ? { "& tbody": { opacity: 0.6, transition: "opacity .12s ease-in-out", pointerEvents: "none" } } : null),
      }}
    >
      <Scrollbar>
        <Table size={dense ? "small" : "medium"} className={className} aria-label={ariaLabel}>
          <TableHeadCustom
            headCells={headCells}
            order={direction}
            orderBy={sorting[0]?.id}
            onSort={(id) => table.getColumn(id)?.toggleSorting(sorting[0]?.id === id ? !sorting[0].desc : false)}
            rowCount={rows.length}
            numSelected={selectedIds ? rows.filter((row) => selectedIds.has(row.id)).length : 0}
            onSelectAllRows={selection?.onToggleAll ? (checked) => selection.onToggleAll?.(rows.map((row) => row.original), checked) : undefined}
          />
          {data.length === 0 ? (
            <TableBody>
              <TableRow>
                <TableCell colSpan={colCount}>{typeof empty === "string" || typeof empty === "number" ? <EmptyContent filled title={String(empty)} sx={{ py: 10 }} /> : empty}</TableCell>
              </TableRow>
            </TableBody>
          ) : (
            rows.map((row) => {
              const open = expandable ? expandable.isOpen(row.original) : false;
              const selected = Boolean(selectedIds?.has(row.id));
              return (
                <TableBody key={row.id} className={open ? "xgroup open" : undefined}>
                  <TableRow
                    hover
                    selected={selected}
                    className={expandable ? (open ? "xrow open" : "xrow") : undefined}
                    // The whole line is the affordance. Clicks that land on an interactive control
                    // inside a cell (an inline editor, a link) keep their own meaning and do not
                    // toggle: the control handles them and stops propagation.
                    onClick={expandable ? () => expandable.onToggle(row.original) : undefined}
                    data-selected={selected ? "true" : undefined}
                    sx={expandable ? { cursor: "pointer" } : undefined}
                  >
                    {selection ? (
                      <TableCell
                        padding="checkbox"
                        // The checkbox is its own affordance: on an expandable table the row click
                        // opens the detail row, and ticking a box must not also do that.
                        onClick={(event) => event.stopPropagation()}
                      >
                        <Checkbox
                          checked={selected}
                          onChange={(event) => selection.onToggle(row.original, event.target.checked)}
                          slotProps={{ input: { "aria-label": selection.ariaLabel?.(row.original) ?? `Select row ${row.id}` } }}
                        />
                      </TableCell>
                    ) : null}
                    {row.getVisibleCells().map((cell) => {
                      const meta = cell.column.columnDef.meta as DataTableColumnMeta | undefined;
                      if (meta?.spanned) return null;
                      return (
                        <TableCell key={cell.id} className={meta?.cellClassName} colSpan={meta?.colSpan} align={meta?.align} style={meta?.cellStyle}>
                          {flexRender(cell.column.columnDef.cell, cell.getContext())}
                        </TableCell>
                      );
                    })}
                    {rowActions ? (
                      <TableCell className="kit-actcell" align="right" onClick={(event) => event.stopPropagation()}>
                        {rowActions(row.original)}
                      </TableCell>
                    ) : null}
                  </TableRow>
                  {expandable && open ? expandable.render(row.original) : null}
                </TableBody>
              );
            })
          )}
          {footer ? <MuiTableFooter>{footer}</MuiTableFooter> : null}
        </Table>
      </Scrollbar>
    </Box>
  );
}
