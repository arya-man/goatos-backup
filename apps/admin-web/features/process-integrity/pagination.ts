import { one, type RouteSearchParams } from "@/lib/search-params";
import type { VaccinationPageSize } from "@/features/preventive-care-vaccination";

export function backendPage(
  sp: RouteSearchParams,
  prefix: string,
  pageSizeOptions: readonly number[],
  fallbackPageSize: VaccinationPageSize,
): { page: number; pageSize: VaccinationPageSize; offset: number } {
  const requestedSize = Number(one(sp, `${prefix}_limit`));
  const pageSize = pageSizeOptions.includes(requestedSize) ? requestedSize : fallbackPageSize;
  const requestedPage = Number(one(sp, `${prefix}_page`));
  const page = Number.isInteger(requestedPage) && requestedPage > 0 ? requestedPage : 1;
  return { page, pageSize, offset: (page - 1) * pageSize };
}

export function maxPageFor(total: number, pageSize: VaccinationPageSize): number {
  if (total <= 0) return 1;
  return Math.max(1, Math.ceil(total / pageSize));
}

export function pageResult<T>(items: T[], total: number, page: number, pageSize: VaccinationPageSize) {
  const clampedPage = Math.min(page, maxPageFor(total, pageSize));
  const start = total === 0 ? 0 : (clampedPage - 1) * pageSize + 1;
  const end = total === 0 ? 0 : Math.min(total, (clampedPage - 1) * pageSize + items.length);
  return { items, page: clampedPage, pageSize, total, start, end };
}
