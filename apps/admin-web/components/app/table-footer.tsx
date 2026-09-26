"use client";

import type { ReactNode } from "react";
import Box from "@mui/material/Box";

import { TablePaginationCustom } from "@/components/minimal/table";

const n = (v: number) => v.toLocaleString("en-IN");

export type TableFooterProps = {
  /** 1-based page index. */
  page: number;
  rowsPerPage: number;
  total: number;
  onPageChange: (page: number) => void;
  onRowsPerPageChange?: (rows: number) => void;
  rowsPerPageOptions?: number[];
  /** Left slot before the rows-per-page control (e.g. a DenseToggle or selection count). */
  left?: ReactNode;
  className?: string;
};

/**
 * Standard table footer: the template's TablePaginationCustom (rows-per-page, range, prev/next;
 * `left` sits where the template puts its Dense switch). `page` stays 1-based for callers. An empty
 * table shows "0 of 0" rather than the nonsense "1–0 of 0". Thin behaviour wrapper: MUI Box + the
 * template pagination, no CSS module.
 */
export function TableFooter({
  page,
  rowsPerPage,
  total,
  onPageChange,
  onRowsPerPageChange,
  rowsPerPageOptions = [10, 25, 50, 100],
  left,
  className,
}: TableFooterProps) {
  const pages = Math.max(1, Math.ceil(total / Math.max(rowsPerPage, 1)));
  const current = Math.min(Math.max(page, 1), pages);
  return (
    <Box className={className} sx={{ position: "relative" }}>
      <TablePaginationCustom
        count={total}
        page={current - 1}
        rowsPerPage={rowsPerPage}
        rowsPerPageOptions={onRowsPerPageChange ? rowsPerPageOptions : [rowsPerPage]}
        onPageChange={(_event, next) => onPageChange(next + 1)}
        onRowsPerPageChange={(event) => onRowsPerPageChange?.(Number(event.target.value))}
        labelDisplayedRows={({ from, to, count }) => (count === 0 ? "0 of 0" : `${n(from)}–${n(to)} of ${n(count)}`)}
        sx={onRowsPerPageChange ? undefined : { "& .MuiTablePagination-selectLabel, & .MuiTablePagination-input": { display: "none" } }}
      />
      {left ? (
        <Box sx={{ pl: 2, py: 1.5, top: 0, position: { sm: "absolute" }, display: "flex", alignItems: "center" }}>{left}</Box>
      ) : null}
    </Box>
  );
}
