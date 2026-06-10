// ── Chart color ramps ──

/** 25-shade cyberpunk gradient: neon teals → cyans → chartreuse → mints → jades */
export const GRADIENT_25 = [
  "#14F1D9", // 1  neon teal
  "#12D9C3", // 2  teal
  "#10C1AD", // 3  muted teal
  "#10FF98", // 4  neon emerald
  "#20E890", // 5  soft emerald
  "#30D188", // 6  muted green
  "#40BA80", // 7  sage green
  "#50A378", // 8  forest teal
  "#608C70", // 9  dark sage
  "#14D4E8", // 10 cyan
  "#14B8D0", // 11 muted cyan
  "#149CB8", // 12 steel cyan
  "#1480A0", // 13 deep blue teal
  "#146488", // 14 ocean blue
  "#144870", // 15 dark navy teal
  "#C6FF00", // 16 chartreuse
  "#A8D900", // 17 muted lime
  "#8AB300", // 18 olive lime
  "#6C8D00", // 19 dark lime
  "#4E6700", // 20 deep olive
  "#1AF0B0", // 21 mint teal
  "#22E0A0", // 22 seafoam
  "#2AD090", // 23 light jade
  "#32C080", // 24 jade
  "#3AB070", // 25 dark jade
] as const;

/** Pick N colors smart-spaced across the 25-shade gradient.
 *  Fewer items → wider gaps (max contrast), 16+ → consecutive (smooth gradient). */
export function pickSpacedColors(n: number): string[] {
  if (n <= 0) return [];
  if (n === 1) return [GRADIENT_25[0]]; // neon teal
  // For 2-3 items, use maximally distinct hand-picked shades
  if (n === 2) return [GRADIENT_25[0], GRADIENT_25[15]]; // neon teal + chartreuse
  if (n === 3) return [GRADIENT_25[0], GRADIENT_25[9], GRADIENT_25[15]]; // teal, cyan, chartreuse
  if (n === 4) return [GRADIENT_25[0], GRADIENT_25[6], GRADIENT_25[12], GRADIENT_25[15]];
  if (n === 5) return [GRADIENT_25[0], GRADIENT_25[3], GRADIENT_25[9], GRADIENT_25[13], GRADIENT_25[15]];
  if (n === 6) return [GRADIENT_25[0], GRADIENT_25[3], GRADIENT_25[7], GRADIENT_25[9], GRADIENT_25[13], GRADIENT_25[15]];
  if (n >= 25) return [...GRADIENT_25];
  // 7+ items: evenly spaced across gradient
  const step = (GRADIENT_25.length - 1) / (n - 1);
  return Array.from({ length: n }, (_, i) =>
    GRADIENT_25[Math.round(i * step)]
  );
}

/** Backward-compatible alias: single-series bar charts use smart spacing */
export const BAR_GRAY_RAMP = GRADIENT_25;

/** Multi-category charts use the same gradient */
export const MULTI_CATEGORY_COLORS = GRADIENT_25;

/** Pie chart colors (full gradient, cycled by index) */
export const PIE_GRAYS = GRADIENT_25;

/** Area / line chart accent */
export const AREA_ACCENT = "#14F1D9"; // neon teal from palette

/** Assign bar shades: smart-spaced across gradient based on item count */
export function assignBarShades(
  data: { name: string; value: number }[]
): { name: string; value: number; fill: string }[] {
  const colors = pickSpacedColors(data.length);
  // Rank by value (descending) to assign shades, but preserve original order
  const ranked = [...data]
    .map((d, i) => ({ index: i, value: d.value }))
    .sort((a, b) => b.value - a.value);
  const shadeByIndex = new Map<number, string>();
  ranked.forEach((r, rank) => {
    shadeByIndex.set(r.index, colors[rank % colors.length]);
  });
  return data.map((d, i) => ({
    ...d,
    fill: shadeByIndex.get(i)!,
  }));
}

// ── Indicator colors ──

export const POSITIVE = "#4ade80";
export const NEGATIVE = "#f87171";

// ── Text tokens (for chart axes / labels rendered in SVG) ──

export const TEXT_PRIMARY = "#FFFFFF";
export const TEXT_SECONDARY = "#E0E8F0";
export const TEXT_MUTED = "#8899AA";
export const TEXT_TERTIARY = "#8899AA";

// ── Surface tokens ──

export const SURFACE_BG = "#0F1115";
export const SURFACE_CARD = "#1A1D24";
export const SURFACE_MUTED = "#22262E";
export const BORDER_DEFAULT = "#334155";
export const BORDER_HOVER = "#334155";

// ── Theme accent ──

export const ACCENT = "#14F1D9";

// ── Indian currency formatter ──

export function formatINR(value: number): string {
  if (value === 0) return "\u20B90";
  const abs = Math.abs(value);
  const sign = value < 0 ? "-" : "";

  // Indian grouping: last 3 digits, then groups of 2
  const str = Math.round(abs).toString();
  if (str.length <= 3) return `${sign}\u20B9${str}`;

  const last3 = str.slice(-3);
  let remaining = str.slice(0, -3);
  const parts: string[] = [];
  while (remaining.length > 2) {
    parts.unshift(remaining.slice(-2));
    remaining = remaining.slice(0, -2);
  }
  if (remaining) parts.unshift(remaining);

  return `${sign}\u20B9${parts.join(",")},${last3}`;
}

/** Compact INR: 1.5Cr, 12.3L, 45K */
export function formatINRCompact(value: number): string {
  const abs = Math.abs(value);
  const sign = value < 0 ? "-" : "";
  if (abs >= 1_00_00_000) return `${sign}\u20B9${(abs / 1_00_00_000).toFixed(2)}Cr`;
  if (abs >= 1_00_000) return `${sign}\u20B9${(abs / 1_00_000).toFixed(2)}L`;
  if (abs >= 1_000) return `${sign}\u20B9${(abs / 1_000).toFixed(1)}K`;
  return formatINR(value);
}

// ── Number formatter ──

export function formatNumber(value: number): string {
  return value.toLocaleString("en-IN");
}

// ── Breed list (canonical order) ──

export const BREEDS = [
  "Beetal",
  "Anantapur",
  "Kenguri",
  "Sojat",
  "Malai",
  "Osmanabadi",
  "Boer",
] as const;

export type Breed = (typeof BREEDS)[number];

// ── Farm tabs ──

export const FARM_TABS = [
  { id: "overall", label: "Overall" },
  { id: "core-farms", label: "Core Farms" },
  { id: "cbe", label: "CBE" },
  { id: "cpt", label: "CPT" },
  { id: "holdings", label: "Holdings" },
] as const;

export type FarmTabId = (typeof FARM_TABS)[number]["id"];

// ── Shed capacities ──

export const SHED_CAPACITIES: Record<string, number> = {
  "Gandhi 1 - Part 1": 50,
  "Gandhi 1 - Part 2": 50,
  "Gandhi 1": 50,
  "Gandhi 2": 50,
  "Gandhi 3": 50,
  "Ho Chi Minh 1": 30,
  "Ho Chi Minh 2": 30,
  "Castro 1": 50,
  "Castro 2": 50,
  "Castro 3": 50,
  "Q1": 5,
  "Q2": 5,
  "Q3": 5,
  "Mandela 1 - Part 1": 15,
  "Mandela 1 - Part 2": 15,
  "Mandela 1 - Part 3": 15,
  "Mandela 1 - Part 4": 15,
  "Mandela 1 - Part 5": 15,
  "Mandela 1 - Part 6": 15,
  "Mandela 1 - Part 7": 30,
  "Mandela 1 - Part 8": 15,
  "Mandela 1 - Part 9": 15,
  "Mandela 1 - Part 10": 15,
  "Mandela 2 - Part 1": 15,
  "Mandela 2 - Part 2": 15,
  "Mandela 2 - Part 3": 15,
  "Mandela 2 - Part 4": 15,
  "Mandela 2 - Part 5": 15,
  "Mandela 2 - Part 6": 15,
  "Mandela 2 - Part 7": 15,
  "Mandela 2 - Part 8": 15,
  "Mandela 2 - Part 9": 15,
  "Mandela 2 - Part 10": 15,
  "Godel 1 - Part 1": 35,
  "Godel 1 - Part 2": 30,
  "Godel 1 - Part 3": 30,
  "Godel 1 - Part 4": 10,
  "Godel 1 - Part 5": 12,
  "Godel 1 - Part 6": 12,
  "Godel 1 - Part 7": 12,
  "Godel 1 - Part 8": 12,
  "Godel 2 - Part 1": 15,
  "Godel 2 - Part 2": 15,
  "Godel 2 - Part 3": 40,
  "Godel 2 - Part 4": 40,
  "Godel 2 - Part 5": 10,
  "Godel 2 - Part 6": 15,
  "Godel 2 - Part 7": 10,
  "Godel 2 - Part 8": 10,
  "Sumathi 1 - Part 1": 13,
  "Sumathi 1 - Part 2": 13,
  "Sumathi 1 - Part 3": 13,
  "Sumathi 1 - Part 4": 13,
  "Sumathi 1 - Part 5": 13,
  "Sumathi 1 - Part 6": 13,
  "Sumathi 1 - Part 7": 13,
  "Sumathi 1 - Part 8": 13,
  "Sumathi 2 - Part 1": 13,
  "Sumathi 2 - Part 2": 13,
  "Sumathi 2 - Part 3": 13,
  "Sumathi 2 - Part 4": 13,
  "Sumathi 2 - Part 5": 13,
  "Sumathi 2 - Part 6": 13,
  "Sumathi 2 - Part 7": 13,
  "Sumathi 2 - Part 8": 13,
  "Yashoda 1": 30,
  "Yashoda 2": 30,
  "Yashoda 3": 10,
  "Yashoda 4": 20,
  "Yashoda 5": 10,
  "Yashoda 6": 20,
  "Yashoda 7": 35,
  "Yashoda 8": 10,
  "Yashoda 9": 60,
  "Yashoda 10": 12,
  "Old Yashoda 1": 5,
  "Old Yashoda 2": 5,
  "Old Yashoda 3": 5,
  "Old Yashoda 4": 5,
  "Old Yashoda 5": 5,
};

export function getShedCapacity(shed: string): number {
  return SHED_CAPACITIES[shed] ?? 15;
}

// ── Feed name display overrides ──
const FEED_NAME_OVERRIDES: Record<string, string> = {
  "uht milk": "UHT Milk",
};

export function normalizeFeedName(name: string): string {
  return FEED_NAME_OVERRIDES[name.toLowerCase()] ?? name;
}
