"use client";
import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";

import { useState, type ReactNode } from "react";

import { TableFooter } from "@/components/app/table-footer";

/**
 * Client-side paging over rows a SERVER component already rendered: the caller hands over the
 * whole array of `<TableRow>` nodes (kept as they are, links and chips included), this shows one page
 * of them and the kit TableFooter below. Presentation only — nothing is refetched, the numbers
 * and the row order are the server's.
 */
export function PagedRows({
  rows,
  head,
  wrapClassName = "tablewrap",
  tableClassName,
  ariaLabel,
  rowsPerPageOptions = [10, 25, 50],
  initialRowsPerPage = 10,
  empty,
}: {
  rows: ReactNode[];
  head: ReactNode;
  wrapClassName?: string;
  tableClassName?: string;
  ariaLabel: string;
  rowsPerPageOptions?: number[];
  initialRowsPerPage?: number;
  /** Shown inside the table when there are no rows (a full-width cell, say). */
  empty?: ReactNode;
}) {
  const [page, setPage] = useState(1);
  const [rowsPerPage, setRowsPerPage] = useState(initialRowsPerPage);
  const total = rows.length;
  const pages = Math.max(1, Math.ceil(total / rowsPerPage));
  const current = Math.min(page, pages);
  const slice = rows.slice((current - 1) * rowsPerPage, current * rowsPerPage);
  return (
    <>
      <div className={wrapClassName} tabIndex={0} role="group" aria-label={ariaLabel}>
        <Table className={tableClassName ? `kit-paged-table ${tableClassName}` : "kit-paged-table"} aria-label={ariaLabel}>
          <TableHead>{head}</TableHead>
          <TableBody>{total === 0 ? empty : slice}</TableBody>
        </Table>
      </div>
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
