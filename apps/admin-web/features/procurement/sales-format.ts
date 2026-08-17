// Pure presentation helpers for the sales board. No copy lives here — every visible LABEL comes
// from the backend page contract; these only format backend NUMBERS and build ?farm= links.

const SALES_PATH = "/procurement/sales";

/** Indian-grouped rupee figure: 1234567 -> "₹12,34,567". Whole rupees by default. */
export function inr(value: number, fractionDigits = 0): string {
  return `₹${value.toLocaleString("en-IN", {
    minimumFractionDigits: fractionDigits,
    maximumFractionDigits: fractionDigits,
  })}`;
}

/** Indian-grouped plain number, for counts and kg. */
export function num(value: number, fractionDigits = 0): string {
  return value.toLocaleString("en-IN", {
    minimumFractionDigits: fractionDigits,
    maximumFractionDigits: fractionDigits,
  });
}

/**
 * The selected farm scope: the raw ?farm= value when it names a served option key, otherwise the
 * default key. A hand-edited URL must not take the page down or leak an unvalidated value into the
 * API call.
 */
export function resolveFarm(raw: string | undefined, optionKeys: readonly string[], defaultKey: string): string {
  if (raw && optionKeys.includes(raw)) return raw;
  return defaultKey;
}

/**
 * Server-rendered link for the sales board. The farm toggle and the deals pager share this one
 * builder so a farm switch always resets the ledger offset (the patch simply omits it) while a
 * pager click always preserves the selected farm.
 */
export function salesHref(params: { farm?: string; offset?: number; limit?: number }, defaults: { farm: string; limit: number }): string {
  const query = new URLSearchParams();
  if (params.farm && params.farm !== defaults.farm) query.set("farm", params.farm);
  if (params.limit && params.limit !== defaults.limit) query.set("limit", String(params.limit));
  if (params.offset && params.offset > 0) query.set("offset", String(params.offset));
  const qs = query.toString();
  return qs ? `${SALES_PATH}?${qs}` : SALES_PATH;
}

/**
 * Status tone for the ledger chip. The LABEL is the backend's recorded status rendered verbatim —
 * this maps only the visual tone, which is presentation and therefore the frontend's to own.
 */
export function dealStatusTone(status: string): "ok" | "info" | "warn" | "dng" | "mut" {
  switch (status.toLowerCase()) {
    case "deal closed":
      return "ok";
    case "advance paid":
      return "info";
    case "in discussion":
      return "warn";
    case "deal failed":
      return "dng";
    default:
      return "mut";
  }
}

type MonthlyLike = {
  sheep_revenue: number;
  goat_revenue: number;
  manure_revenue: number;
  sheep_count: number;
  goat_count: number;
};

/**
 * Total revenue for one month's column. The shared column chart draws ONE series, so the three
 * backend-served revenue components are shown as their sum (maintainer-approved simple chart) —
 * never as a hand-built stacked chart.
 */
export function monthlyRevenueTotal(month: MonthlyLike): number {
  return month.sheep_revenue + month.goat_revenue + month.manure_revenue;
}

/** Animals sold in one month's column: sheep plus goats. Manure is kg and never joins this axis. */
export function monthlyAnimalsTotal(month: MonthlyLike): number {
  return month.sheep_count + month.goat_count;
}

/**
 * The market-check loss per kg: how far our landed cost sits ABOVE the market's quoted price.
 * Positive means we land dearer than the market sells. Null when either side is unrecorded — a
 * missing quote is not a zero-loss claim.
 */
export function marketLossPerKg(landingCostPerKg: number | null | undefined, marketPricePerKg: number | null | undefined): number | null {
  if (landingCostPerKg == null || marketPricePerKg == null) return null;
  return landingCostPerKg - marketPricePerKg;
}
