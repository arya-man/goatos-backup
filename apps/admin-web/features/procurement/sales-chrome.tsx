import Link from "@/components/no-prefetch-link";
import { LinkPending } from "@/components/link-pending";
import { copy, optionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { redirect } from "next/navigation";
import { getAdminWebBootstrap } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";
import {
  SALES_ALL_FARMS,
  resolveSalesParkScope,
  salesPageHref,
  salesParkPatch,
  type SalesPark,
} from "./sales-park-scope";

/**
 * Chrome the three read pages under Sales share (the board, Sold and Farm value, split
 * 2026-09-11): the page header and the farm toggle. Presentation only -- every string comes from
 * the page contract, and each page passes its own path so the toggle stays on the page it is on.
 */

export const SALES_DEFAULT_FARM = SALES_ALL_FARMS;

/** Rebuilds the current query with a patch, on the given page path. */
export function hrefWithQuery(pagePath: string, sp: RouteSearchParams, patch: Record<string, string | null>): string {
  return salesPageHref(pagePath, sp, patch);
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
 * The park the page is scoped to, read from the SHELL's `park` parameter (sales-park-scope.ts) and
 * resolved against the parks the bootstrap contract serves this caller -- the same list the top bar
 * offers, cached for the request. A legacy `?farm=CPT` URL is redirected onto `park` here, before
 * the page reads anything, so the sidebar carries the choice from then on.
 */
export async function readSalesParkScope(
  sp: RouteSearchParams,
  pageContract: AdminUiPageContract,
  pagePath: string,
): Promise<{ parkId: string; farm: string; parks: SalesPark[] }> {
  const bootstrap = await getAdminWebBootstrap();
  const parks: SalesPark[] = bootstrap.ok
    ? bootstrap.data.top_bar.park_selector.options.map((option) => ({ id: option.key, code: option.label }))
    : [];
  const farmKeys = optionGroup(pageContract, "sales_farms").map((option) => option.key);
  const scope = resolveSalesParkScope(sp, parks, farmKeys, pagePath);
  if (scope.redirectTo) redirect(scope.redirectTo);
  return { parkId: scope.parkId, farm: scope.farm, parks };
}

/**
 * Farm scope toggle: server-rendered links, so the selection survives a reload and a shared URL.
 *
 * It writes the SHELL's `park` (a location uuid) and `scope_mode`, exactly as the top bar does, and
 * keeps every other parameter on the page, so the sidebar carries the choice to the next Sales page
 * and the page's own filters survive the switch. `clears` names what a park change invalidates
 * (a ledger offset, a pen that belongs to the other park). The labels are the contract's
 * `sales_farms` options; a farm the caller has no park for is not offered.
 */
export function SalesFarmToggle({
  pageContract,
  pagePath,
  searchParams,
  parkId,
  parks,
  clears = [],
}: {
  pageContract: AdminUiPageContract;
  pagePath: string;
  searchParams: RouteSearchParams;
  /** The selected park's uuid; "" is every farm. */
  parkId: string;
  parks: readonly SalesPark[];
  /** Parameters a park switch drops, beyond the always-dropped legacy `farm`. */
  clears?: readonly string[];
}) {
  const cleared = Object.fromEntries(clears.map((key) => [key, null]));
  const choices = optionGroup(pageContract, "sales_farms").flatMap((option) => {
    if (option.key === SALES_ALL_FARMS) return [{ option, id: "" }];
    const park = parks.find((candidate) => candidate.code === option.key);
    return park ? [{ option, id: park.id }] : [];
  });
  return (
    <div className="chips" role="group" aria-label={copy(pageContract, "filter.farm")} style={{ marginBottom: 14 }}>
      <span className="muted small" style={{ marginRight: 6 }}>
        {copy(pageContract, "filter.farm")}
      </span>
      {choices.map(({ option, id }) => (
        <Link
          key={option.key}
          href={salesPageHref(pagePath, searchParams, { ...cleared, ...salesParkPatch(id) })}
          scroll={false}
          className={id === parkId ? "btn sm p" : "btn sm"}
          aria-current={id === parkId ? "true" : undefined}
        >
          {option.label}
          <LinkPending />
        </Link>
      ))}
    </div>
  );
}
