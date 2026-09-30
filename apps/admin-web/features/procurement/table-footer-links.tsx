"use client";

import { useCallback, useEffect, useState } from "react";
import { DenseToggle } from "@/components/app/dense-toggle";
import { TablePaginationLinks } from "@/components/app/table/table-pagination-links";

/**
 * Table footer for the SERVER-paged sales / procurement tables.
 *
 * The template table pagination (TablePaginationLinks: dense · rows-per-page · range · arrows),
 * every control a link, because these pages carry their page state in the URL. It is not `TableFooter` (which
 * is callback-driven) and not `WorklistPager` (which reads its labels from the worklist page
 * contracts, whose keys these routes do not have): every label is passed in by the caller from its
 * own contract, so no route needs a new copy key to get a real footer.
 *
 * Dense is client state that belongs to the table, so the toggle puts `kit-dense` on the element
 * with id `denseTargetId` — the table's own `.tablewrap`. The choice is remembered per table id.
 */
export function ProcurementTableFooter({
  page,
  pageCount,
  prevHref,
  nextHref,
  rangeLabel,
  rowsLabel,
  rowsValue,
  rowsOptions,
  prevLabel,
  nextLabel,
  denseTargetId,
  denseLabel,
}: {
  /** Page-number mode: the current 1-based page and the page count. Cursor lists pass 1/1. */
  page: number;
  pageCount: number;
  /**
   * Explicit prev/next targets, `null`/omitted when that direction does not exist. Always strings:
   * this is a client component rendered from server pages, so an `hrefForPage` callback would be
   * a function crossing the RSC boundary ("Functions cannot be passed to Client Components").
   */
  prevHref?: string | null;
  nextHref?: string | null;
  /** e.g. "1–25 of 312 deals" — the caller owns the wording and the plural. */
  rangeLabel: string;
  rowsLabel: string;
  rowsValue?: number;
  rowsOptions?: readonly { size: number; href: string }[];
  prevLabel: string;
  nextLabel: string;
  denseTargetId?: string;
  /** Backend copy from the caller's page contract (e.g. `copy(pageContract, "action.dense", "Dense")`). */
  denseLabel: string;
}) {
  const [dense, setDense] = useState(false);

  // Dense is presentational, so it is applied to the table wrapper rather than threaded back into
  // the server render. It is deliberately NOT persisted: reading localStorage during mount means a
  // setState in an effect and a first paint at the wrong density.
  useEffect(() => {
    if (!denseTargetId) return;
    document.getElementById(denseTargetId)?.classList.toggle("kit-dense", dense);
  }, [dense, denseTargetId]);

  const onDense = useCallback((next: boolean) => setDense(next), []);

  const prev = page > 1 ? (prevHref ?? null) : null;
  const next = page < pageCount ? (nextHref ?? null) : null;

  return (
    <TablePaginationLinks
      className="table-footer-pager"
      page={Math.max(0, page - 1)}
      rowsPerPage={rowsValue ?? 0}
      count={-1}
      rowsPerPageHrefs={rowsOptions && rowsOptions.length > 1 && rowsValue != null ? rowsOptions.map((option) => ({ value: option.size, href: option.href })) : undefined}
      prevHref={prev}
      nextHref={next}
      labelRowsPerPage={`${rowsLabel}:`}
      rangeLabel={rangeLabel}
      prevLabel={prevLabel}
      nextLabel={nextLabel}
      left={denseTargetId ? <DenseToggle checked={dense} onChange={onDense} label={denseLabel} /> : null}
    />
  );
}
