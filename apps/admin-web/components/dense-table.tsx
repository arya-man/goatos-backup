"use client";

import { useState, type ReactNode } from "react";

import { DenseToggle } from "@/components/app/dense-toggle";
import { cx } from "@/lib/tone";
import Box from "@mui/material/Box";
import { TablePaginationLinks, type TablePaginationLinksProps } from "@/components/app/table/table-pagination-links";

/**
 * Table wrapper + footer for SERVER-rendered tables.
 *
 * The dense switch is the only piece of a table footer that needs client state, and the tables on
 * the Feed and Counts routes are server components whose rows, sort and paging all ride on the URL.
 * Rather than making those pages client components for one toggle, the wrapper takes the finished
 * table as `children` — a server subtree passed as a prop — and owns nothing but the `kit-dense`
 * class it puts on the wrapper. The footer is the template table pagination (TablePaginationLinks)
 * with the Dense switch in its left slot; a route with no paging passes no `pagination`.
 */
export function DenseTable({
  children,
  pagination,
  denseLabel,
  className,
}: {
  children: ReactNode;
  pagination?: Omit<TablePaginationLinksProps, "left">;
  /** Omitted falls back to the kit's own label, as elsewhere in the kit. */
  denseLabel?: string;
  className?: string;
}) {
  const [dense, setDense] = useState(false);
  const toggle = <DenseToggle checked={dense} onChange={setDense} label={denseLabel} />;
  return (
    <>
      <div className={cx("tablewrap", dense && "kit-dense", className)}>{children}</div>
      {pagination ? <TablePaginationLinks {...pagination} left={toggle} /> : <Box sx={{ pl: 2, py: 1.5 }}>{toggle}</Box>}
    </>
  );
}
