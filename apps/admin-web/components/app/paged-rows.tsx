"use client";
import Box from "@mui/material/Box";
import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";

import type { SxProps, Theme } from "@mui/material/styles";
import { useState, type ReactNode } from "react";

const TABLE_SCROLL_SX = { maxWidth: 1, minWidth: 0, overflowX: "auto", overscrollBehaviorX: "contain" } as const;

import { TableFooter } from "@/components/app/table-footer";
import { Scrollbar } from "@/components/minimal/scrollbar";
import { TableHeadCustom, type TableHeadCellProps } from "@/components/app/table/table-head-custom";

/**
 * Client-side paging over rows a SERVER component already rendered: the caller hands over the
 * whole array of `<TableRow>` nodes (kept as they are, links and chips included), this shows one page
 * of them and the kit TableFooter below. Presentation only — nothing is refetched, the numbers
 * and the row order are the server's.
 */
export function PagedRows({
  rows,
  head,
  headCells,
  tableSx,
  ariaLabel,
  rowsPerPageOptions = [10, 25, 50],
  initialRowsPerPage = 10,
  empty,
  scrollbar = false,
  tableMinWidth,
}: {
  rows: ReactNode[];
  /** Raw header row (older callers). Prefer `headCells`. */
  head?: ReactNode;
  /** Template TableHeadCustom cells (the template list header: one line, sort-ready, head padding). */
  headCells?: TableHeadCellProps[];
  /** Extra sx for the Table (per-column floors, nowrap cells). */
  tableSx?: SxProps<Theme>;
  ariaLabel: string;
  rowsPerPageOptions?: number[];
  initialRowsPerPage?: number;
  /** Shown inside the table when there are no rows (a full-width cell, say). */
  empty?: ReactNode;
  /** Template table card: the table scrolls sideways inside the template Scrollbar, the pager stays put. */
  scrollbar?: boolean;
  /** Table min width in px so a wide table scrolls instead of squeezing its columns. */
  tableMinWidth?: number;
}) {
  const [page, setPage] = useState(1);
  const [rowsPerPage, setRowsPerPage] = useState(initialRowsPerPage);
  const total = rows.length;
  const pages = Math.max(1, Math.ceil(total / rowsPerPage));
  const current = Math.min(page, pages);
  const slice = rows.slice((current - 1) * rowsPerPage, current * rowsPerPage);
  const table = (
    <Table aria-label={ariaLabel} sx={[tableMinWidth ? { minWidth: tableMinWidth } : {}, ...(Array.isArray(tableSx) ? tableSx : tableSx ? [tableSx] : [])]}>
      {headCells ? <TableHeadCustom headCells={headCells.map((cell) => ({ sortable: false, ...cell }))} /> : <TableHead>{head}</TableHead>}
      <TableBody>{total === 0 ? empty : slice}</TableBody>
    </Table>
  );
  return (
    <>
      {/* The table's own sideways scroller (FIXJ6: was the legacy `.tablewrap` class). */}
      <Box tabIndex={0} role="group" aria-label={ariaLabel} sx={scrollbar ? undefined : TABLE_SCROLL_SX}>
        {scrollbar ? <Scrollbar>{table}</Scrollbar> : table}
      </Box>
      {total > 0 ? (
        <TableFooter
          page={current}
          rowsPerPage={rowsPerPage}
          total={total}
          rowsPerPageOptions={rowsPerPageOptions}
          onPageChange={setPage}
          onRowsPerPageChange={(n) => {
            setRowsPerPage(n);
            setPage(1);
          }}
        />
      ) : null}
    </>
  );
}
