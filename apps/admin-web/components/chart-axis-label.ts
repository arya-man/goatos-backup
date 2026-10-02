// Category x-axis labels for the shared chart wrappers (GroupedColumns, TrendChart, ColumnBars,
// ColumnChartCard, BalanceStatisticsCard). Pure functions, no copy. (The template's useChart stays
// verbatim -- guard chart-wrapper-verbatim -- so the wrappers apply this, not the base options.)
//
// WHY. Apex turns a crowded category axis 45 degrees with `text-anchor: end`, so a label's END sits
// under its tick and its START runs down-left, into a label band capped at `maxHeight`. A long label
// ran past that band and was cut at its START: the load charts showed "…Godel 2 - Part 2 +4)" and
// lost the load number, the one thing the reader came for (PR #294 B3/E1/E2; the bracket is
// lib/load-pens.ts). The fix keeps the HEAD of the label and truncates its END, under a budget that
// fits the band at 45 degrees, and the tooltip keeps the full text.

/** Characters a rotated label may keep: ~120px band at 45 degrees, ~12px type. */
export const AXIS_LABEL_MAX_CHARS = 22;

/** `text` cut at the END to `max` characters (ellipsis included); shorter text is returned as is. */
export function truncateAxisLabel(text: string, max: number = AXIS_LABEL_MAX_CHARS): string {
  const chars = [...text];
  if (chars.length <= max) return text;
  return `${chars.slice(0, Math.max(1, max - 1)).join("").trimEnd()}…`;
}

/** A category label as Apex hands it over: a string, a multi-line string array, or anything else. */
export function formatCategoryLabel(value: unknown, max: number = AXIS_LABEL_MAX_CHARS): unknown {
  if (typeof value === "string") return truncateAxisLabel(value, max);
  if (Array.isArray(value)) return value.map((line) => (typeof line === "string" ? truncateAxisLabel(line, max) : line));
  return value;
}

/** The tooltip title: the FULL category, never the truncated axis label. */
export function fullCategoryLabel(value: unknown): unknown {
  return Array.isArray(value) ? value.join(" · ") : value;
}

/**
 * Characters a rotated label may keep on a PHONE. A 390 plot gives a ~30px slot per load and ~75px
 * left of the first tick (y labels + padding), so a 22-character label at 45 degrees (~100px across)
 * still lost its head there: "E Godel 2 - Pa…" with the load number cut off (PR #294 K1). Fourteen
 * keeps the load number and the start of its first pen ("131 (CPT Castr…"); the tooltip has the rest.
 */
export const PHONE_AXIS_LABEL_MAX_CHARS = 14;

/** Below this chart width (px) a rotated label takes the phone budget. */
export const NARROW_CHART_PX = 600;

/** The label budget for a chart this wide: a phone-width plot keeps the shorter head. */
export function axisLabelBudget(chartWidthPx: number | undefined): number {
  return chartWidthPx !== undefined && chartWidthPx > 0 && chartWidthPx < NARROW_CHART_PX ? PHONE_AXIS_LABEL_MAX_CHARS : AXIS_LABEL_MAX_CHARS;
}

type ApexLabelOpts = { w?: { globals?: { svgWidth?: number } } };

/**
 * `xaxis.labels` for a category axis: end-truncated labels, sized to the chart. Apex hands a custom
 * label formatter its chart (`opts.w`), so every wrapper that uses this gets the phone budget on a
 * narrow chart without a breakpoint hook of its own (PR #294 K1).
 */
export const CATEGORY_AXIS_LABELS = {
  formatter: (value: unknown, _raw?: unknown, opts?: ApexLabelOpts) =>
    formatCategoryLabel(value, axisLabelBudget(opts?.w?.globals?.svgWidth)) as string,
};

/**
 * A tooltip title formatter that shows the FULL category at the hovered index: Apex otherwise
 * reuses the axis label formatter for the title, which would print the truncated label.
 */
export function fullCategoryTitle(categories: readonly unknown[]) {
  return (value: unknown, opts?: { dataPointIndex?: number }) => String(fullCategoryLabel(categories[opts?.dataPointIndex ?? -1] ?? value) ?? "");
}
