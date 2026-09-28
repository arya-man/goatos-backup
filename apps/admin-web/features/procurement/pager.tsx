import { TablePaginationLinks } from "@/components/app/table/table-pagination-links";
import { DenseToggleAuto } from "@/components/app/dense-toggle-auto";

// Mock-styled cursor pager shared by the procurement row surfaces (Source Entry Board, Action Center,
// Protocol Adherence). Prev/Next are real server-navigation links built from the backend's next_cursor +
// the cursor-stack helpers in lib/search-params; disabled affordance when there is no page in a direction.
//
// It is the template table pagination (TablePaginationLinks) so a paged board ends the same way every
// other table on the redesign does. What it CANNOT wear is the kit `TableFooter` component itself: these lists
// are cursor-paged, so there is no row total and no page count to offer — a rows-per-page control and an
// "x-y of N" range would have to be invented. The range text below stays what the backend can actually
// support: the page number and the rows on this page.
//
// TR2 P1-6: a caller that pages by a URL `limit` passes `rowsPerPage` + `rowsPerPageHrefs` (each href
// drops the cursor chain, so a page always starts at (page-1)*limit) and gets the template footer in
// full: Dense switch left, rows-per-page, and MUI's own cursor range wording ("1–25 of more than 25"
// while a next cursor exists, "1–6 of 6" on the last page). guard: source-entry-table-template.
export function ProcurementPager({
  prevHref,
  nextHref,
  page,
  count,
  noun,
  forceVisible = false,
  rangeLabel,
  rowsPerPage,
  rowsPerPageHrefs,
  labelRowsPerPage,
  dense = false,
}: {
  prevHref: string | null;
  nextHref: string | null;
  page: number;
  count: number;
  noun: string;
  forceVisible?: boolean;
  /** Kit wording ("1–25 of 97") when the caller knows the whole-filter total; else the page/count line. */
  rangeLabel?: string;
  /** The URL page size the rows were read with (limit/cursor lists). */
  rowsPerPage?: number;
  /** Rows-per-page choice -> href (cursor chain dropped). */
  rowsPerPageHrefs?: { value: number; href: string }[];
  labelRowsPerPage?: string;
  /** The template Dense switch in the footer's left slot. */
  dense?: boolean;
}) {
  if (!forceVisible && !prevHref && !nextHref && page <= 1) return null;

  const from = rowsPerPage && count > 0 ? (page - 1) * rowsPerPage + 1 : 0;
  const to = from > 0 ? from + count - 1 : 0;
  const cursorRange = rowsPerPage ? `${from}–${to} of ${nextHref ? `more than ${to}` : to}` : null;
  return (
    <TablePaginationLinks
      page={Math.max(0, page - 1)}
      rowsPerPage={rowsPerPage ?? Math.max(count, 1)}
      rowsPerPageHrefs={rowsPerPageHrefs}
      labelRowsPerPage={labelRowsPerPage}
      left={dense ? <DenseToggleAuto /> : undefined}
      count={-1}
      prevHref={prevHref}
      nextHref={nextHref}
      rangeLabel={rangeLabel ?? cursorRange ?? `Page ${page} · ${count} ${noun}${count === 1 ? "" : "s"} on this page`}
      prevLabel="Previous page"
      nextLabel="Next page"
    />
  );
}
