// ONE park filter across every Sales read page (defect 2026-09-25).
//
// Sold, Farm value and Buyer analytics used to filter on their own `?farm=CBE|CPT` chips while Load
// wise read the shell's `?park=<location uuid>` and Farm born its own `?park` select. The sidebar
// carries the shell's scope (`scope_mode` / `park`) between pages and never `farm`, so a CPT picked
// on Sold was gone on Buyer analytics, and a CPT picked on Load wise was gone on Summary.
//
// Every Sales page now reads and writes the SHELL's parameter, `park`, holding a location uuid --
// the value the top bar writes on every other screen and the sidebar already carries. A Sales read
// whose endpoint is keyed by the deal's FARM CODE (sales_deals.farm is 'CBE' / 'CPT') gets the code
// resolved from that park here, server-side, against the parks the bootstrap contract serves; the
// URL never carries the code. A bookmarked `?farm=CPT` still lands on CPT: the page redirects it
// onto `park`, so the sidebar picks it up from there.
//
// Pure: no framework import, so the rules are node-testable.

import type { RouteSearchParams } from "../../lib/search-params.ts";

/** The "every farm" choice, and the key the contract's `sales_farms` group serves for it. */
export const SALES_ALL_FARMS = "all";

/** A park the caller may see: its location uuid and its code (CBE, CPT). */
export type SalesPark = { id: string; code: string };

export type SalesParkScope = {
  /** The selected park's location uuid; "" is every farm. */
  parkId: string;
  /** The deal farm code the sales reads filter by; SALES_ALL_FARMS for every farm. */
  farm: string;
  /**
   * Set when the URL named the park the LEGACY way (`farm=CPT`): the canonical URL to redirect to,
   * carrying `park` instead. Null when the URL is already canonical.
   */
  redirectTo: string | null;
};

function first(sp: RouteSearchParams, key: string): string {
  const value = sp[key];
  return (Array.isArray(value) ? value[0] : value) ?? "";
}

/** Rebuilds the current query with a patch, on the given page path. */
export function salesPageHref(pagePath: string, sp: RouteSearchParams, patch: Record<string, string | null>): string {
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

/**
 * The query patch that selects a park the way the shell does (`scopeHref`): `scope_mode=park` plus
 * `park` for one park, `scope_mode=company` and no `park` for every farm. `farm` is always cleared,
 * so a legacy parameter can never linger beside the canonical one and disagree with it.
 */
export function salesParkPatch(parkId: string): Record<string, string | null> {
  return parkId
    ? { scope_mode: "park", park: parkId, farm: null }
    : { scope_mode: "company", park: null, farm: null };
}

/**
 * Resolves the page's park from the URL.
 *
 * `park` wins. It is trusted only when it names a park the caller may see AND whose code is one of
 * the farms the page's contract serves; anything else (a hand-edited uuid, a park the caller lost
 * access to) reads as every farm rather than as a farm nobody chose. Only when `park` is absent is
 * a legacy `farm` code honoured, and then only to redirect onto the canonical URL.
 */
export function resolveSalesParkScope(
  sp: RouteSearchParams,
  parks: readonly SalesPark[],
  farmKeys: readonly string[],
  pagePath: string,
): SalesParkScope {
  const servesCode = (code: string) => code !== SALES_ALL_FARMS && farmKeys.includes(code);
  const parkParam = first(sp, "park");
  const legacyFarm = first(sp, "farm");

  if (parkParam && parkParam !== SALES_ALL_FARMS) {
    const park = parks.find((candidate) => candidate.id === parkParam);
    const scope =
      park && servesCode(park.code)
        ? { parkId: park.id, farm: park.code }
        : { parkId: "", farm: SALES_ALL_FARMS };
    // A stale `farm` beside a canonical `park` is dropped so the two cannot disagree on screen.
    return { ...scope, redirectTo: legacyFarm ? salesPageHref(pagePath, sp, salesParkPatch(scope.parkId)) : null };
  }

  if (legacyFarm) {
    const park = servesCode(legacyFarm) ? parks.find((candidate) => candidate.code === legacyFarm) : undefined;
    const parkId = park?.id ?? "";
    return {
      parkId,
      farm: park ? park.code : SALES_ALL_FARMS,
      redirectTo: salesPageHref(pagePath, sp, salesParkPatch(parkId)),
    };
  }

  return { parkId: "", farm: SALES_ALL_FARMS, redirectTo: null };
}
