import type { ReactNode } from "react";

import { TablePaginationLinks } from "@/components/minimal/table/table-pagination-links";
import { DenseToggleAuto } from "@/components/app/dense-toggle-auto";
import { boundedInt, one, type RouteSearchParams } from "@/lib/search-params";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";

export const VACCINATION_PAGE_SIZE_OPTIONS = [5, 10, 25, 50] as const;
export const MAX_BACKEND_OFFSET = 50_000;

export type VaccinationPageSize = number;

export interface PagedRows<T> {
  items: T[];
  page: number;
  pageSize: VaccinationPageSize;
  total: number;
  totalPages: number;
  start: number;
  end: number;
}

export function paginationFromParams(
  params: RouteSearchParams | undefined,
  prefix: string,
  total: number,
  fallbackPageSize: VaccinationPageSize = 10,
  pageSizeOptions: readonly number[] = VACCINATION_PAGE_SIZE_OPTIONS,
): Omit<PagedRows<never>, "items"> {
  const requestedSize = boundedInt(one(params ?? {}, `${prefix}_limit`), fallbackPageSize, 1, 100);
  const pageSize = pageSizeOptions.find((size) => size === requestedSize) ?? fallbackPageSize;
  const totalPages = cappedTotalPages(total, pageSize);
  const page = boundedInt(one(params ?? {}, `${prefix}_page`), 1, 1, totalPages);
  const start = total === 0 ? 0 : (page - 1) * pageSize + 1;
  const end = total === 0 ? 0 : Math.min(total, page * pageSize);
  return { page, pageSize, total, totalPages, start, end };
}

export function maxBackendPageForPageSize(pageSize: VaccinationPageSize): number {
  return Math.max(1, Math.floor(MAX_BACKEND_OFFSET / pageSize) + 1);
}

export function cappedTotalPages(total: number, pageSize: VaccinationPageSize): number {
  const totalPages = total <= 0 ? 1 : Math.ceil(total / pageSize);
  return Math.min(totalPages, maxBackendPageForPageSize(pageSize));
}

export function paginateRows<T>(
  rows: T[],
  params: RouteSearchParams | undefined,
  prefix: string,
  fallbackPageSize: VaccinationPageSize = 10,
  pageSizeOptions: readonly number[] = VACCINATION_PAGE_SIZE_OPTIONS,
): PagedRows<T> {
  const pageInfo = paginationFromParams(params, prefix, rows.length, fallbackPageSize, pageSizeOptions);
  return {
    ...pageInfo,
    items: rows.slice((pageInfo.page - 1) * pageInfo.pageSize, pageInfo.page * pageInfo.pageSize),
  };
}

/**
 * The shared vaccination table footer.
 *
 * Same anatomy as the kit `TableFooter` (left slot · rows-per-page select · range · arrow icon
 * buttons), but every control is a LINK because these tables page on the SERVER through the URL.
 * Three things the previous hand-rolled `.pager2` got wrong and this one keeps right:
 *  - It returned `null` at `totalPages <= 1`, so most tables in the module had NO footer. The
 *    footer now always renders: the range is information even when there is one page.
 *  - Rows-per-page was a row of link chips (5 10 25 50) which does not fit 390px; it is one
 *    styled `LinkSelect` (no native <select>).
 *  - Prev/Next were unbounded-width text buttons; they are icon buttons with accessible names.
 * `left` carries the dense toggle / selection count, owned by whatever renders the table.
 */
export function VaccinationTablePager({
  pageContract,
  pageSizeOptions,
  page,
  pageSize,
  total,
  start,
  end,
  noun = "row",
  hrefForPage,
  hrefForPageSize,
  left,
}: {
  pageContract: AdminUiPageContract;
  pageSizeOptions: readonly number[];
  page: number;
  pageSize: VaccinationPageSize;
  total: number;
  start: number;
  end: number;
  noun?: string;
  hrefForPage: (page: number) => string;
  hrefForPageSize: (pageSize: VaccinationPageSize) => string;
  left?: ReactNode;
}) {
  const totalPages = cappedTotalPages(total, pageSize);
  const hasPrevious = page > 1;
  const hasNext = page < totalPages;
  const pluralNoun = total === 1 ? noun : `${noun}s`;
  const previousLabel = copy(pageContract, "action.previous");
  const nextLabel = copy(pageContract, "action.next");

  return (
    <TablePaginationLinks
      className="pager2"
      replace
      page={Math.max(0, page - 1)}
      rowsPerPage={pageSize}
      count={total}
      rowsPerPageHrefs={pageSizeOptions.map((size) => ({ value: size, href: hrefForPageSize(size) }))}
      prevHref={hasPrevious ? hrefForPage(page - 1) : null}
      nextHref={hasNext ? hrefForPage(page + 1) : null}
      labelRowsPerPage={`${copy(pageContract, "pager.rows")}:`}
      rangeLabel={
        total === 0
          ? `0 ${pluralNoun}`
          : `${start}\u2013${end} ${pluralNoun} \u00b7 ${copy(pageContract, "pager.page")} ${page} ${copy(pageContract, "pager.of")} ${totalPages}`
      }
      prevLabel={previousLabel}
      nextLabel={nextLabel}
      /* Density switch on every table with more than a page of rows (frame spec). */
      left={left ?? (total > 10 ? <DenseToggleAuto label={copy(pageContract, "pager.dense", "Dense")} /> : null)}
    />
  );
}
