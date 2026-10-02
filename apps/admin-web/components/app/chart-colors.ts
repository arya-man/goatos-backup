import type { Theme } from "@mui/material/styles";
import { useSyncExternalStore } from "react";
import { useColorScheme, useTheme } from "@mui/material/styles";
import { CHART_CATEGORICAL, type ChartCategoricalKey } from "@/theme/chart-palette";

const noSubscribe = () => () => {};

/**
 * Chart colours, ALWAYS from the MUI theme palette (the template's charts read
 * `theme.palette.<key>.<shade>`: ApexCharts does colour maths on the string it gets, so it must be
 * the resolved hex of the active scheme, never a `var(--…)`). This module is the one door: every
 * chart passes its `colors` through `chartColor` / `chartRamp`, and design:guard fails a
 * `colors:` in ApexCharts options that is a raw `var(--…)`, hex or `color-mix()` literal.
 *
 * A series colour is named by a palette channel (`"primary"`, `"info.dark"`, `"grey.500"`). The
 * Mesha token names older callers still pass (`"var(--palette-primary-main)"`, `"var(--amber)"`) map onto a
 * channel here, so an entity keeps its hue on every chart while the callers move to channels.
 */

type Key = "primary" | "secondary" | "info" | "success" | "warning" | "error";
type Shade = "lighter" | "light" | "main" | "dark" | "darker";
type GreyShade = "300" | "400" | "500" | "600" | "700";

/**
 * A palette channel: `"info"` (= info.main), `"info.dark"`, `"grey.500"`; or a categorical chart
 * slot (`"series-blue"`, theme/chart-palette.ts) for a chart that names more series than the theme
 * has hues.
 */
export type ChartColorKey = Key | `${Key}.${Shade}` | `grey.${GreyShade}` | ChartCategoricalKey;

const CATEGORICAL = new Map<string, (typeof CHART_CATEGORICAL)[number]>(CHART_CATEGORICAL.map((slot) => [slot.key, slot]));

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

/** Marks a theme whose colour scheme is not resolved yet (server render + hydration pass). */
type ChartTheme = Theme & { chartSchemeUnresolved?: true };

/**
 * The theme every chart reads its colours through (N4, TR-2). On the server and during hydration
 * MUI has no colour scheme yet, so `theme.palette` is the LIGHT scheme: a legend dot painted from it
 * shows light-only greens (#54A02C) in dark mode until the client re-renders -- and forever on a
 * page that hydrates late. Until `useColorScheme()` reports a scheme, the returned theme makes
 * `chartColor` answer with the scheme-following CSS variable (`var(--palette-primary-main)`), which
 * the browser paints right from the first frame (legends, dots, bars drawn with CSS). ApexCharts
 * never renders in that pass (it loads client-side after mount), and the consumer re-renders with
 * resolved hexes as soon as it has hydrated and the scheme is known, so Apex draws with hexes.
 * guard: chart-theme-scheme (check-design-system + r2-visual-audit `ssr-scheme`).
 */
export function useChartTheme(): Theme {
  const theme = useTheme();
  const { colorScheme } = useColorScheme();
  // Per component, not per page: a streamed Suspense boundary hydrates AFTER the shell resolved the
  // scheme, so it must still render its server snapshot (variables) in its own hydration pass, or
  // React reports an attribute mismatch it never patches. useSyncExternalStore gives exactly that:
  // the server snapshot while hydrating, then an immediate re-render with the client one.
  const hydrated = useSyncExternalStore(noSubscribe, () => true, () => false);
  return hydrated && colorScheme ? theme : ({ ...theme, chartSchemeUnresolved: true } as ChartTheme);
}

/** Resolve a channel (or a legacy `var(--token)`) to the active scheme's colour. */
export function chartColor(theme: Theme, token: string): string {
  // Already a resolved theme value (a caller that read theme.palette itself).
  if (/^(#|rgb)/.test(token)) return token;
  // A categorical slot: the step for the active scheme, or its scheme-following variable
  // (theme/app-baseline emits `--chart-series-*` per scheme) until the scheme is known.
  const slot = CATEGORICAL.get(token.trim());
  if (slot) return (theme as ChartTheme).chartSchemeUnresolved ? `var(${slot.cssVar})` : slot[theme.palette.mode === "light" ? "light" : "dark"];
  // `theme.vars.palette.info.main` is `var(--palette-info-main)`: read back as its channel.
  const vars = /^var\(--palette-(primary|secondary|info|success|warning|error|grey)-(\w+)\)$/.exec(token.trim());
  const legacy = vars ? null : /^var\(--([\w-]+)\)$/.exec(token.trim());
  const key = (vars ? `${vars[1]}.${vars[2]}` : legacy ? TOKEN_CHANNEL[legacy[1]] : token) ?? "primary";
  const [name, shade] = key.split(".") as [string, string | undefined];
  if ((theme as ChartTheme).chartSchemeUnresolved) {
    const channel = name === "grey" ? "grey" : name in theme.palette ? name : "primary";
    return `var(--palette-${channel}-${shade ?? (channel === "grey" ? "500" : "main")})`;
  }
  if (name === "grey") return theme.palette.grey[(shade ?? "500") as unknown as 500];
  const channel = theme.palette[(name in theme.palette ? name : "primary") as Key];
  return channel[(shade ?? "main") as Shade] ?? channel.main;
}

/**
 * Categorical series, in order: the twelve validated hues of theme/chart-palette.ts (one hue per
 * series, never two shades of one hue side by side; no error red). Bars lead with primary.dark
 * (template AppAreaInstalled / BankingBalanceStatistics); lines and areas with the brand green.
 */
const RAMP: ChartColorKey[] = ["series-green", "series-blue", "series-orange", "series-violet", "series-magenta", "series-olive", "series-teal", "series-yellow", "series-indigo", "series-aqua", "series-plum", "series-brown"];

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
