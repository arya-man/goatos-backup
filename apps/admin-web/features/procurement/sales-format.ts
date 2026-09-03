// Pure presentation helpers for the sales board. No copy lives here — every visible LABEL comes
// from the backend page contract; these only format backend NUMBERS and build ?farm= links.

const SALES_PATH = "/sales";

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

/**
 * Compact rupee figure for tight chart labels, in the farm's own units: lakh and crore.
 * 7160979 -> "₹71.6L", 42500 -> "₹42.5k", 900 -> "₹900". Full figures stay in tooltips.
 */
export function inrCompact(value: number): string {
  const abs = Math.abs(value);
  if (abs >= 1_00_00_000) return `₹${trimZero(value / 1_00_00_000)}Cr`;
  if (abs >= 1_00_000) return `₹${trimZero(value / 1_00_000)}L`;
  if (abs >= 1_000) return `₹${trimZero(value / 1_000)}k`;
  return `₹${Math.round(value)}`;
}

/**
 * Signed rupees: a profit carries a leading +, a loss a leading −. Money that can go either way
 * must SAY which it is — "₹5,02,000" beside a red cell is still ambiguous when skimmed, and the
 * minus sign is what survives a screenshot.
 */
export function signedInr(value: number, fractionDigits = 0): string {
  const sign = value < 0 ? "−" : "+";
  return `${sign}${inr(Math.abs(value), fractionDigits)}`;
}

/** Signed form of inrCompact, for chart tooltips and KPI tiles. */
export function signedInrCompact(value: number): string {
  const sign = value < 0 ? "−" : "+";
  return `${sign}${inrCompact(Math.abs(value))}`;
}

/** Compact plain number for chart labels: 219305 -> "2.2L", 12410 -> "12.4k", 528 -> "528". */
export function numCompact(value: number): string {
  const abs = Math.abs(value);
  if (abs >= 1_00_000) return `${trimZero(value / 1_00_000)}L`;
  if (abs >= 1_000) return `${trimZero(value / 1_000)}k`;
  return String(Math.round(value));
}

/** Compact chart label without decimals or units: 94800 -> "95k", 412.5 -> "413". */
export function numCompactWhole(value: number): string {
  const abs = Math.abs(value);
  if (abs >= 1_00_000) return `${Math.floor(value / 1_00_000 + 0.5)}L`;
  if (abs >= 1_000) return `${Math.floor(value / 1_000 + 0.5)}k`;
  return String(Math.floor(value + 0.5));
}

function trimZero(value: number): string {
  const fixed = value.toFixed(1);
  return fixed.endsWith(".0") ? fixed.slice(0, -2) : fixed;
}

const MONTH_SHORT = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

/** Axis label for a backend "YYYY-MM" month key: "2025-04" -> "Apr 25". */
export function monthLabel(month: string): string {
  const [y, m] = month.split("-");
  const index = Number(m) - 1;
  if (!y || index < 0 || index > 11 || Number.isNaN(index)) return month;
  return `${MONTH_SHORT[index]} ${y.slice(2)}`;
}

/** Readable date for a backend "YYYY-MM-DD" value: "2025-04-15" -> "15 Apr 2025". */
export function humanDate(date: string): string {
  const [y, m, d] = date.split("-");
  const index = Number(m) - 1;
  if (!y || !d || index < 0 || index > 11 || Number.isNaN(index)) return date;
  return `${Number(d)} ${MONTH_SHORT[index]} ${y}`;
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
 * Rupees earned from LIVE animals in one month: sheep plus goats, manure excluded. Shown as the
 * animals column's second figure so a head count carries the money it earned; it never drives the
 * bar height, which stays the head count.
 */
export function monthlyAnimalRevenueTotal(month: MonthlyLike): number {
  return month.sheep_revenue + month.goat_revenue;
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
