import Link from "@/components/no-prefetch-link";
import { copy, optionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { RouteSearchParams } from "@/lib/search-params";
import { salesHref } from "./sales-format";

/**
 * Chrome the three read pages under Sales share (the board, Sold and Farm value, split
 * 2026-09-11): the page header and the farm toggle. Presentation only -- every string comes from
 * the page contract, and each page passes its own path so the toggle stays on the page it is on.
 */

export const SALES_DEFAULT_FARM = "all";

/** Rebuilds the current query with a patch, on the given page path. */
export function hrefWithQuery(pagePath: string, sp: RouteSearchParams, patch: Record<string, string | null>): string {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(sp)) {
    const single = Array.isArray(value) ? value[0] : value;
    if (single) query.set(key, single);
  }
  for (const [key, value] of Object.entries(patch)) {
    if (value === null || value === "") query.delete(key);
    else query.set(key, value);
  }
  const qs = query.toString();
  return qs ? `${pagePath}?${qs}` : pagePath;
}

export function SalesPageHeader({ pageContract }: { pageContract: AdminUiPageContract }) {
  return (
    <div className="phead" style={{ marginTop: 12, alignItems: "flex-end", paddingBottom: 6 }}>
      <div>
        <div className="crumb">
          {/* The crumb names the VERTICAL and the title names the page. The board carries the
              vertical's own name, so appending the title there would repeat one word on both
              sides of the separator. The dedupe is presentation only -- both strings stay
              backend-owned and neither is composed here. */}
          <b>{copy(pageContract, "crumb")}</b>
          {copy(pageContract, "crumb") === pageContract.title ? null : <> · {pageContract.title}</>}
        </div>
        <h1>{pageContract.title}</h1>
        <div className="sub">{pageContract.subtitle}</div>
      </div>
    </div>
  );
}

/**
 * Farm scope toggle: server-rendered links, so the selection survives a reload and a shared URL.
 * A farm switch drops any ledger offset by construction (salesHref omits it), and keeps the
 * sale-ready error margin on Farm value, where it is the page's own filter.
 */
export function SalesFarmToggle({
  pageContract,
  pagePath,
  farm,
  limit,
  defaultLimit,
  saleReadyToleranceG,
}: {
  pageContract: AdminUiPageContract;
  pagePath: string;
  farm: string;
  /** The ledger page size to keep across the switch; pages without a ledger pass nothing. */
  limit?: number;
  defaultLimit?: number;
  /** Farm value's applied error margin, carried across the switch. */
  saleReadyToleranceG?: number;
}) {
  const farmOptions = optionGroup(pageContract, "sales_farms");
  return (
    <div className="chips" role="group" aria-label={copy(pageContract, "filter.farm")} style={{ marginBottom: 14 }}>
      <span className="muted small" style={{ marginRight: 6 }}>
        {copy(pageContract, "filter.farm")}
      </span>
      {farmOptions.map((option) => (
        <Link
          key={option.key}
          href={salesHref(
            { farm: option.key, limit, saleReadyToleranceG },
            { farm: SALES_DEFAULT_FARM, limit: defaultLimit ?? 0 },
            pagePath,
          )}
          scroll={false}
          className={option.key === farm ? "btn sm p" : "btn sm"}
          aria-current={option.key === farm ? "true" : undefined}
        >
          {option.label}
        </Link>
      ))}
    </div>
  );
}
