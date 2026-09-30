import type { ReactNode } from "react";
import { TablePaginationLinks } from "@/components/app/table/table-pagination-links";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { pagerNoun } from "@/features/preventive-care-vaccination/pager-noun";

/**
 * The shared worklist table footer.
 *
 * The template's table pagination (TablePaginationLinks: left slot · rows-per-page · range ·
 * arrows), but every control is a LINK, because these worklists page on the SERVER through the URL: the footer has to
 * survive with JavaScript off and has to produce a real, shareable address for page 3. That is why
 * this is not simply `<TableFooter>` — that one is callback-driven for client-paged tables.
 *
 * Two things the old row got wrong and this one must keep right:
 *  - The page-size control was a ROW OF BUTTONS (10 25 50 100). Four buttons plus two arrows plus
 *    the range text does not fit 390px, so the bar wrapped to three lines and the arrows ended up
 *    somewhere under the table. It is now one styled select.
 *  - Prev/Next were text buttons of unbounded width in two languages. They are icon buttons with
 *    accessible names, which are the same size whatever the copy says.
 */
export function WorklistPager({
  pageContract,
  offset,
  limit,
  rowCount,
  hasMore,
  noun,
  pageSizeOptions,
  hrefForOffset,
  hrefForLimit,
  left,
  pageUnits,
}: {
  pageContract: AdminUiPageContract;
  offset: number;
  limit: number;
  rowCount: number;
  hasMore: boolean;
  noun: string;
  pageSizeOptions: readonly number[];
  hrefForOffset: (offset: number) => string;
  hrefForLimit: (limit: number) => string;
  /**
   * Leading slot, pinned left: the table's dense toggle or a selection count. Passed in rather than
   * owned here because "dense" is client state that belongs to whatever renders the table.
   */
  left?: ReactNode;
  /**
   * How many of the `limit` units this page holds, when a unit is not one table row: the feed
   * worklists page by PEN (`items` is a page of sheds) and draw one row per grain, so "Rows: 10"
   * sat beside "1–40 rows" and page 2 read "11–64" (J3B P2-4). The range counts the same unit the
   * rows-per-page select and the offset count. Defaults to `rowCount`. Test `pager-range-units`.
   */
  pageUnits?: number;
}) {
  const units = pageUnits ?? rowCount;
  const start = units === 0 ? 0 : offset + 1;
  const end = units === 0 ? 0 : offset + units;
  const pluralNoun = pagerNoun(noun, units);
  const previousOffset = Math.max(0, offset - limit);
  const previousLabel = copy(pageContract, "action.previous");
  const nextLabel = copy(pageContract, "action.next");

  return (
    <TablePaginationLinks
      page={Math.floor(offset / Math.max(limit, 1))}
      rowsPerPage={limit}
      count={-1}
      rowsPerPageHrefs={pageSizeOptions.map((size) => ({ value: size, href: hrefForLimit(size) }))}
      prevHref={offset > 0 ? hrefForOffset(previousOffset) : null}
      nextHref={hasMore ? hrefForOffset(offset + limit) : null}
      labelRowsPerPage={`${copy(pageContract, "pager.rows")}:`}
      rangeLabel={
        units === 0
          ? `0 ${pluralNoun}`
          : `${start}–${end} ${pluralNoun} · ${copy(pageContract, "pager.page")} ${Math.floor(offset / limit) + 1}`
      }
      prevLabel={previousLabel}
      nextLabel={nextLabel}
      left={left}
    />
  );
}
