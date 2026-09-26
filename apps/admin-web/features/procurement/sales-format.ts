import { fmtDate } from "../../lib/format.ts";
// Pure presentation helpers for the sales board. No copy lives here — every visible LABEL comes
// from the backend page contract; these only format backend NUMBERS. The park filter's links live
// in sales-park-scope.ts.

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
 * A line priced by the unit, as the phone writes it: "20 kg at ₹40/kg", "2,000 kg at ₹21/kg",
 * "3 number at ₹50/number". Empty when the line has no quantity (an animal line), so the cell shows
 * absence rather than a made-up figure. Mirrors Android quantityAtRate so both surfaces read alike.
 */
export function quantityAtRate(quantity: number | null | undefined, unit: string | null | undefined, rate: number | null | undefined): string {
  if (quantity == null) return "";
  const u = (unit ?? "").trim();
  const amount = u ? `${num1(quantity)} ${u}` : num1(quantity);
  if (rate == null) return amount;
  return `${amount} at ${inr(rate, Number.isInteger(rate) ? 0 : 2)}${u ? `/${u}` : ""}`;
}

/**
 * The breed worth showing beside a product, or null when there is none. A manure line's "breed" is
 * only its own name again, so the drawer, the ledger and the price chart read "Manure · Manure"
 * (seen 2026-09-26). The phone says it once (Android productAndBreed); so does the web.
 */
export function breedBeyondProduct(product: string | null | undefined, breed: string | null | undefined): string | null {
  const b = (breed ?? "").trim();
  if (b === "") return null;
  if (b.toLowerCase() === (product ?? "").trim().toLowerCase()) return null;
  return b;
}

/** Indian-grouped, one decimal at most, whole numbers without a fraction: 2000 -> "2,000", 12.5 -> "12.5". */
function num1(value: number): string {
  return value.toLocaleString("en-IN", { minimumFractionDigits: 0, maximumFractionDigits: 1 });
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
  const sign = value < 0 ? "−" : "";
  if (abs >= 1_00_000) return `${sign}${Math.floor(abs / 1_00_000 + 0.5)}L`;
  if (abs >= 1_000) return `${sign}${Math.floor(abs / 1_000 + 0.5)}k`;
  return `${sign}${Math.floor(abs + 0.5)}`;
}

function trimZero(value: number): string {
  const fixed = value.toFixed(1);
  return fixed.endsWith(".0") ? fixed.slice(0, -2) : fixed;
}

const MONTH_SHORT = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

/**
 * Axis label for a backend "YYYY-MM" month key: "2025-04" -> "Apr 2025".
 *
 * A month heading has no day component, so it is NOT a date and keeps its own form
 * (maintainer decision 2026-09-10). The year is spelled in full because the old two-digit
 * "Apr 25" reads as April 25th on a page where every real date is numeric.
 */
export function monthLabel(month: string): string {
  const [y, m] = month.split("-");
  const index = Number(m) - 1;
  if (!y || index < 0 || index > 11 || Number.isNaN(index)) return month;
  return `${MONTH_SHORT[index]} ${y}`;
}

/** Readable date for a backend "YYYY-MM-DD" value: "2025-04-15" -> "15/04/2025". */
export function humanDate(date: string): string {
  return fmtDate(date);
}

type MonthlyLike = {
  revenue?: number;
  live_revenue?: number;
  animals?: number;
  feed_revenue?: number;
  other_revenue?: number;
  sheep_revenue: number;
  goat_revenue: number;
  manure_revenue: number;
  sheep_count: number;
  goat_count: number;
};

/**
 * Backend-owned total for one month, including every authored product. Legacy payloads
 * without totals retain their component-based rendering during rollout.
 */
export function monthlyRevenueTotal(month: MonthlyLike): number {
  return month.revenue ?? (
    monthlyAnimalRevenueTotal(month) + month.manure_revenue +
    (month.feed_revenue ?? 0) + (month.other_revenue ?? 0)
  );
}

/** All animals sold in one month, including custom products. */
export function monthlyAnimalsTotal(month: MonthlyLike): number {
  return month.animals ?? (month.sheep_count + month.goat_count);
}

/**
 * Rupees earned from all live animal products in one month. Shown as the
 * animals column's second figure so a head count carries the money it earned; it never drives the
 * bar height, which stays the head count.
 */
export function monthlyAnimalRevenueTotal(month: MonthlyLike): number {
  return month.live_revenue ?? (month.sheep_revenue + month.goat_revenue);
}

/** Drops leading zero-only months for a chart-specific metric while preserving the current tail. */
export function trimEmptyMonthlyStart<T>(months: T[], valueOf: (month: T) => number): T[] {
  let first = 0;
  while (first < months.length && valueOf(months[first]) <= 0) first += 1;
  return first < months.length ? months.slice(first) : [];
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

/** Mirrors the backend's MaxDealLines; the record drawer stops offering "Add another" at this count. */
export const MAX_SALE_LINES = 20;

/**
 * The day an open sale was PLANNED for, but only when it differs from the sale date shown beside
 * it (2026-09-25: closing a sale stamps the close date and keeps the planned one). A deal recorded
 * already closed has no planned date, and one that closed on its planned day would only repeat the
 * sale date, so both return null and nothing extra is rendered. Wire values are ISO business dates.
 */
export function plannedSaleDateIfDifferent(deal: {
  sale_date: string;
  planned_sale_date?: string | null;
}): string | null {
  const planned = deal.planned_sale_date?.trim() ?? "";
  if (planned === "" || planned === deal.sale_date) return null;
  return planned;
}
