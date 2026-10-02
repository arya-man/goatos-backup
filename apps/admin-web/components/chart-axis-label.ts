// Category x-axis labels for every ApexCharts chart (components/minimal/chart/use-chart.ts installs
// these as the base axis options). Pure functions, no copy.
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

type AxisOptions = {
  xaxis?: { type?: string; categories?: unknown; labels?: { formatter?: unknown } };
  tooltip?: { x?: { formatter?: unknown } };
};

/**
 * Whether a chart's x axis is a plain CATEGORY axis this module should format: categories given,
 * not a datetime/numeric axis, and the caller has not supplied its own label formatter.
 */
export function wantsCategoryLabels(options: AxisOptions | undefined): boolean {
  const xaxis = options?.xaxis;
  if (!xaxis || xaxis.type === "datetime" || xaxis.type === "numeric") return false;
  if (xaxis.labels?.formatter !== undefined) return false;
  return Array.isArray(xaxis.categories) && xaxis.categories.length > 0;
}

/**
 * The axis/tooltip options to layer under a category chart's own: end-truncated axis labels, and a
 * tooltip title that shows the whole category (Apex otherwise reuses the axis formatter for it).
 * A caller's own tooltip.x.formatter still wins.
 */
export function categoryAxisOptions(options: AxisOptions | undefined) {
  if (!wantsCategoryLabels(options)) return null;
  return {
    xaxis: { labels: { formatter: (value: unknown) => formatCategoryLabel(value) } },
    ...(options?.tooltip?.x?.formatter === undefined ? { tooltip: { x: { formatter: (value: unknown) => fullCategoryLabel(value) } } } : null),
  };
}
