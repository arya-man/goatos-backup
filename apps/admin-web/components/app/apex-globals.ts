// ApexCharts global options (window.Apex), set once for every chart, template widgets included.
//
// Ravi 2026-09-27: hovering a chart showed "bar chart with 1 data series: Revenue" on screen.
// ApexCharts 5 writes that generated string into an SVG <title>, which the browser shows as a
// native hover tooltip over the chart (on top of the template tooltip). Its accessibility layer is
// switched off here, globally, so no chart carries the <title>; our chart components name
// themselves on their root instead (role="img" + aria-label from the page contract). The chart
// wrapper and useChart stay the template's bytes. Guard: r2-visual-audit chart-hover a11y-text +
// components/chart-template-anatomy.test.mjs.
//
// PR #294 K5: Apex draws value-axis (y) ticks at 11px and category (x) ticks at 12px; at 11px the
// rendered glyph box reads ~10px and every chart failed the 11px axis floor (P-chart-axis-tiny,
// mobile-axis-text-too-small). Both axes tick at 12px, globally, so no wrapper restates it.
type ApexGlobal = { chart?: Record<string, unknown>; yaxis?: Record<string, unknown> } & Record<string, unknown>;

/** Chart axis tick type size (px), both axes, every viewport. */
export const CHART_AXIS_FONT_PX = 12;

if (typeof window !== "undefined") {
  const w = window as unknown as { Apex?: ApexGlobal };
  const prev = w.Apex ?? {};
  const prevY = (prev.yaxis ?? {}) as { labels?: { style?: Record<string, unknown> } };
  w.Apex = {
    ...prev,
    chart: { ...(prev.chart ?? {}), accessibility: { enabled: false } },
    yaxis: { ...prevY, labels: { ...(prevY.labels ?? {}), style: { ...(prevY.labels?.style ?? {}), fontSize: `${CHART_AXIS_FONT_PX}px` } } },
  };
}

export {};
