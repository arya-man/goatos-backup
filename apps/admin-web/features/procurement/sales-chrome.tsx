import { PageHeader } from "@/components/app/page-header";
import type { ReactNode } from "react";
import { LiveQueryTabs } from "./live-query-link";
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
import Box from "@mui/material/Box";

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

export function SalesPageHeader({ pageContract, actions, tabs }: { pageContract: AdminUiPageContract; subtitle?: boolean; actions?: ReactNode; tabs?: ReactNode }) {
  // Kit PageHeader: eyebrow = the vertical, title = the page, crumbs "Sales • Page". No description
  // slot on purpose (the page explains itself with its labels). Both strings stay backend-owned.
  const crumb = copy(pageContract, "crumb");
  return (
    <PageHeader
      title={pageContract.title}
      crumbs={crumb === pageContract.title ? [{ label: crumb }] : [{ label: crumb, href: "/sales" }, { label: pageContract.title }]}
      actions={actions}
      tabs={tabs}
    />
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
    // The chips' query follows the LIVE URL, so a parameter the page moved in place (Farm value's
    // applied Over 35 kg margin) survives a farm switch (defect 2026-09-25). Template pill tabs.
    <Box sx={{ mb: 1.75 }}>
      <LiveQueryTabs
        pagePath={pagePath}
        ariaLabel={copy(pageContract, "filter.farm")}
        value={choices.find(({ id }) => id === parkId)?.option.key ?? ""}
        items={choices.map(({ option, id }) => ({
          value: option.key,
          label: option.label,
          patch: { ...cleared, ...salesParkPatch(id) },
          fallbackHref: salesPageHref(pagePath, searchParams, { ...cleared, ...salesParkPatch(id) }),
        }))}
      />
    </Box>
  );
}
