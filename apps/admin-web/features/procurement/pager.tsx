import { TablePaginationLinks } from "@/components/minimal/table/table-pagination-links";

// Mock-styled cursor pager shared by the procurement row surfaces (Source Entry Board, Action Center,
// Protocol Adherence). Prev/Next are real server-navigation links built from the backend's next_cursor +
// the cursor-stack helpers in lib/search-params; disabled affordance when there is no page in a direction.
//
// It is the template table pagination (TablePaginationLinks) so a paged board ends the same way every
// other table on the redesign does. What it CANNOT wear is the kit `TableFooter` component itself: these lists
// are cursor-paged, so there is no row total and no page count to offer — a rows-per-page control and an
// "x-y of N" range would have to be invented. The range text below stays what the backend can actually
// support: the page number and the rows on this page.
export function ProcurementPager({
  prevHref,
  nextHref,
  page,
  count,
  noun,
  forceVisible = false,
  rangeLabel,
}: {
  prevHref: string | null;
  nextHref: string | null;
  page: number;
  count: number;
  noun: string;
  forceVisible?: boolean;
  /** Kit wording ("1–25 of 97") when the caller knows the whole-filter total; else the page/count line. */
  rangeLabel?: string;
}) {
  if (!forceVisible && !prevHref && !nextHref && page <= 1) return null;

  return (
    <TablePaginationLinks
      page={Math.max(0, page - 1)}
      rowsPerPage={Math.max(count, 1)}
      count={-1}
      prevHref={prevHref}
      nextHref={nextHref}
      rangeLabel={rangeLabel ?? `Page ${page} · ${count} ${noun}${count === 1 ? "" : "s"} on this page`}
      prevLabel="Previous page"
      nextLabel="Next page"
    />
  );
}
