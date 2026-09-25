import type { AdminUiTableContract } from "@/lib/admin-ui-contract";
import type { RouteSearchParams } from "@/lib/search-params";

/** The whole-result order a Sales table was asked for ("sort all rows", 2026-09-25). */
export type TableOrder = { sort: string; dir: "asc" | "desc" };

/**
 * Reads `sort` / `dir` from the URL and keeps them only when they name a column the table's
 * contract marks sortable -- the same set the backend orders by -- so a hand-edited URL falls back
 * to the default order instead of a refused read taking the page down.
 */
export function tableOrderFromParams(sp: RouteSearchParams, contract: AdminUiTableContract): TableOrder {
  const first = (key: string) => {
    const value = sp[key];
    return (Array.isArray(value) ? value[0] : value) ?? "";
  };
  const sort = first("sort");
  const sortable = contract.columns.some((column) => column.key === sort && column.sortable);
  if (!sortable) return { sort: "", dir: "desc" };
  return { sort, dir: first("dir") === "desc" ? "desc" : "asc" };
}
