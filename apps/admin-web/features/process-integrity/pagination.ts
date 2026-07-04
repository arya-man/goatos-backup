import { one, type RouteSearchParams } from "@/lib/search-params";
import {
  cappedTotalPages,
  maxBackendPageForPageSize,
  type VaccinationPageSize,
} from "@/features/preventive-care-vaccination";

export function backendPage(
  sp: RouteSearchParams,
  prefix: string,
  pageSizeOptions: readonly number[],
  fallbackPageSize: VaccinationPageSize,
): { page: number; pageSize: VaccinationPageSize; offset: number } {
  const requestedSize = Number(one(sp, `${prefix}_limit`));
  const pageSize = pageSizeOptions.includes(requestedSize) ? requestedSize : fallbackPageSize;
  const requestedPage = Number(one(sp, `${prefix}_page`));
  const uncappedPage = Number.isInteger(requestedPage) && requestedPage > 0 ? requestedPage : 1;
  const maxBackendPage = maxBackendPageForPageSize(pageSize);
  const page = Math.min(uncappedPage, maxBackendPage);
  return { page, pageSize, offset: (page - 1) * pageSize };
}

export function maxPageFor(total: number, pageSize: VaccinationPageSize): number {
  return cappedTotalPages(total, pageSize);
}

export function pageResult<T>(items: T[], total: number, page: number, pageSize: VaccinationPageSize) {
  const clampedPage = Math.min(page, maxPageFor(total, pageSize));
  const start = total === 0 ? 0 : (clampedPage - 1) * pageSize + 1;
  const end = total === 0 ? 0 : Math.min(total, (clampedPage - 1) * pageSize + items.length);
  return { items, page: clampedPage, pageSize, total, start, end };
}
