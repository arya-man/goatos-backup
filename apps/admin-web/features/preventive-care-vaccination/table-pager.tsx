import Link from "next/link";
import { ChevronLeft, ChevronRight } from "lucide-react";
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

// Mock `.pager2` footer for bounded vaccination read models. These endpoints still do not expose a backend
// cursor/has_more, so this is honest front-end pagination over the returned bounded set. The UI shape matches
// the mock and prevents one giant long table while backend cursor work can land behind the same component later.
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
}) {
  const totalPages = cappedTotalPages(total, pageSize);
  const hasPrevious = page > 1;
  const hasNext = page < totalPages;
  const pluralNoun = total === 1 ? noun : `${noun}s`;
  const range =
    total === 0
      ? `0 ${pluralNoun}`
      : `${start}-${end} ${copy(pageContract, "pager.of")} ${total} ${pluralNoun}`;

  return (
    <div className="pager2">
      <span className="muted small">
        {range} · {copy(pageContract, "pager.page")} {page} {copy(pageContract, "pager.of")} {totalPages}
      </span>
      <span className="sp" style={{ flex: 1 }} />
      <span className="muted small">{copy(pageContract, "pager.rows")}</span>
      <span className="chipset" style={{ gap: 4 }}>
        {pageSizeOptions.map((size) => (
          <Link
            key={size}
            href={hrefForPageSize(size)}
            replace
            scroll={false}
            className={`chip pgsize${pageSize === size ? " on" : ""}`}
            style={{ padding: "5px 8px", fontSize: 11 }}
          >
            {size}
          </Link>
        ))}
      </span>
      {hasPrevious ? (
        <Link href={hrefForPage(page - 1)} replace scroll={false} className="btn sm">
          <ChevronLeft className="ic" style={{ width: 13 }} aria-hidden="true" /> {copy(pageContract, "action.previous")}
        </Link>
      ) : (
        <span className="btn sm" aria-disabled="true" style={{ opacity: 0.45, cursor: "not-allowed" }}>
          <ChevronLeft className="ic" style={{ width: 13 }} aria-hidden="true" /> {copy(pageContract, "action.previous")}
        </span>
      )}
      {hasNext ? (
        <Link href={hrefForPage(page + 1)} replace scroll={false} className="btn sm">
          {copy(pageContract, "action.next")} <ChevronRight className="ic" style={{ width: 13 }} aria-hidden="true" />
        </Link>
      ) : (
        <span className="btn sm" aria-disabled="true" style={{ opacity: 0.45, cursor: "not-allowed" }}>
          {copy(pageContract, "action.next")} <ChevronRight className="ic" style={{ width: 13 }} aria-hidden="true" />
        </span>
      )}
    </div>
  );
}
