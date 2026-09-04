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

import type { AdminUiTableContract } from "@/lib/admin-ui-contract";

/**
 * The shared headless table body for admin-web worklists.
 *
 * TanStack Table owns the column model, the header row and the sort state; the markup stays the
 * mock's plain `<table>` anatomy, because fidelity here is the DOM the mock declares, not a widget
 * library's default chrome. Nothing about the mock's table shape is inherited from the library.
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
}) {
  const [sorting, setSorting] = useState<SortingState>(initialSorting ?? []);

  const table = useReactTable({
    data,
    columns,
    state: { sorting },
    onSortingChange: setSorting,
    getRowId,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    // The server owns the window. TanStack must not slice, count or page these rows.
    manualPagination: true,
  });

  // Read on every render, never memoized on `table`: the table instance is identity-stable across
  // renders, so a memo keyed on it would keep an empty-state colSpan from a previous column set.
  const colCount = table.getVisibleLeafColumns().length;

  return (
    <table className={className} aria-label={ariaLabel}>
      <thead>
        <tr>
          {table.getHeaderGroups()[0]?.headers.map((header) => {
            const meta = header.column.columnDef.meta as DataTableColumnMeta | undefined;
            const canSort = header.column.getCanSort();
            const direction = header.column.getIsSorted();
            const label = flexRender(header.column.columnDef.header, header.getContext());
            return (
              <th
                key={header.id}
                style={meta?.align === "right" ? { textAlign: "right" } : undefined}
                // Announced so a screen-reader user hears the current order, not just the label.
                aria-sort={!canSort ? undefined : direction === "asc" ? "ascending" : direction === "desc" ? "descending" : "none"}
              >
                {canSort ? (
                  <button
                    type="button"
                    className="thsort"
                    onClick={header.column.getToggleSortingHandler()}
                    aria-label={`${String(header.column.columnDef.header)} — sort this page`}
                  >
                    {label}
                    <span aria-hidden="true" className="thsort-ind">
                      {direction === "asc" ? "▲" : direction === "desc" ? "▼" : "↕"}
                    </span>
                  </button>
                ) : (
                  label
                )}
              </th>
            );
          })}
        </tr>
      </thead>
      {data.length === 0 ? (
        <tbody>
          <tr>
            <td colSpan={colCount}>{empty}</td>
          </tr>
        </tbody>
      ) : (
        table.getRowModel().rows.map((row) => {
            const open = expandable ? expandable.isOpen(row.original) : false;
            return (
              <tbody key={row.id} className={open ? "xgroup open" : undefined}>
                <tr
                  className={expandable ? (open ? "xrow open" : "xrow") : undefined}
                  // The whole line is the affordance. Clicks that land on an interactive control
                  // inside a cell (an inline editor, a link) keep their own meaning and do not
                  // toggle: the control handles them and stops propagation.
                  onClick={expandable ? () => expandable.onToggle(row.original) : undefined}
                >
                  {row.getVisibleCells().map((cell) => {
                    const meta = cell.column.columnDef.meta as DataTableColumnMeta | undefined;
                    if (meta?.spanned) return null;
                    return (
                      <td
                        key={cell.id}
                        className={meta?.cellClassName}
                        colSpan={meta?.colSpan}
                        style={{
                          ...(meta?.align === "right" ? { textAlign: "right" } : null),
                          ...meta?.cellStyle,
                        }}
                      >
                        {flexRender(cell.column.columnDef.cell, cell.getContext())}
                      </td>
                    );
                  })}
                </tr>
                {expandable && open ? expandable.render(row.original) : null}
              </tbody>
            );
          })
      )}
      {footer ? <tfoot>{footer}</tfoot> : null}
    </table>
  );
}
