import type { Theme } from "@mui/material/styles";

/**
 * Chart colours, ALWAYS from the MUI theme palette (the template's charts read
 * `theme.palette.<key>.<shade>`: ApexCharts does colour maths on the string it gets, so it must be
 * the resolved hex of the active scheme, never a `var(--…)`). This module is the one door: every
 * chart passes its `colors` through `chartColor` / `chartRamp`, and design:guard fails a
 * `colors:` in ApexCharts options that is a raw `var(--…)`, hex or `color-mix()` literal.
 *
 * A series colour is named by a palette channel (`"primary"`, `"info.dark"`, `"grey.500"`). The
 * Mesha token names older callers still pass (`"var(--brand)"`, `"var(--amber)"`) map onto a
 * channel here, so an entity keeps its hue on every chart while the callers move to channels.
 */

type Key = "primary" | "secondary" | "info" | "success" | "warning" | "error";
type Shade = "lighter" | "light" | "main" | "dark" | "darker";
type GreyShade = "300" | "400" | "500" | "600" | "700";

/** A palette channel: `"info"` (= info.main), `"info.dark"`, `"grey.500"`. */
export type ChartColorKey = Key | `${Key}.${Shade}` | `grey.${GreyShade}`;

/** Mesha token name (inside `var(--…)`) -> palette channel. */
const TOKEN_CHANNEL: Record<string, ChartColorKey> = {
  brand: "primary",
  primary: "primary",
  ok: "primary",
  success: "success",
  "chart-2": "primary",
  "brand-d": "primary.dark",
  "brand-l": "primary.light",
  "brand-soft": "primary.light",
  "success-ink": "success.dark",
  "gain-hi": "success.light",
  info: "info",
  "chart-1": "info",
  "info-ink": "info.dark",
  teal: "info.light",
  amber: "warning",
  warn: "warning",
  warning: "warning",
  "chart-4": "warning",
  "warning-ink": "warning.dark",
  purple: "secondary",
  violet: "secondary",
  "violet-ink": "secondary.dark",
  danger: "error",
  error: "error",
  "chart-3": "error",
  "gain-under": "error.light",
  "danger-tag-ink": "error.dark",
  "error-ink": "error.dark",
  muted: "grey.500",
  "fg-muted": "grey.500",
  line: "grey.400",
};

/** Resolve a channel (or a legacy `var(--token)`) to the active scheme's colour. */
export function chartColor(theme: Theme, token: string): string {
  // Already a resolved theme value (a caller that read theme.palette itself).
  if (/^(#|rgb)/.test(token)) return token;
  // `theme.vars.palette.info.main` is `var(--palette-info-main)`: read back as its channel.
  const vars = /^var\(--palette-(primary|secondary|info|success|warning|error|grey)-(\w+)\)$/.exec(token.trim());
  const legacy = vars ? null : /^var\(--([\w-]+)\)$/.exec(token.trim());
  const key = (vars ? `${vars[1]}.${vars[2]}` : legacy ? TOKEN_CHANNEL[legacy[1]] : token) ?? "primary";
  const [name, shade] = key.split(".") as [string, string | undefined];
  if (name === "grey") return theme.palette.grey[(shade ?? "500") as unknown as 500];
  const channel = theme.palette[(name in theme.palette ? name : "primary") as Key];
  return channel[(shade ?? "main") as Shade] ?? channel.main;
}

/**
 * Categorical series, in order. No error red and no second green: an ordinary category is never
 * painted in the colours that mean "at risk" elsewhere. Bars lead with primary.dark (template
 * AppAreaInstalled / BankingBalanceStatistics); lines and areas with primary.main
 * (EcommerceYearlySales).
 */
const RAMP: ChartColorKey[] = ["primary", "warning", "info", "secondary", "grey.500", "primary.darker", "warning.light", "info.dark", "secondary.light"];

export function chartRamp(theme: Theme, kind: "bar" | "line" = "line"): string[] {
  return RAMP.map((key, i) => chartColor(theme, i === 0 && kind === "bar" ? "primary.dark" : key));
}

/** Series colour by index (wraps past the ramp). */
export function seriesColor(theme: Theme, index: number, kind: "bar" | "line" = "line"): string {
  const ramp = chartRamp(theme, kind);
  return ramp[index % ramp.length];
}

/** The ramp, for non-Apex marks (progress bars in stories). */
export function chartColors(theme: Theme): string[] {
  return chartRamp(theme);
}
