// ApexCharts global options (window.Apex), set once for every chart, template widgets included.
//
// Ravi 2026-09-27: hovering a chart showed "bar chart with 1 data series: Revenue" on screen.
// ApexCharts 5 writes that generated string into an SVG <title>, which the browser shows as a
// native hover tooltip over the chart (on top of the template tooltip). Its accessibility layer is
// switched off here, globally, so no chart carries the <title>; our chart components name
// themselves on their root instead (role="img" + aria-label from the page contract). The chart
// wrapper and useChart stay the template's bytes. Guard: r2-visual-audit chart-hover a11y-text +
// components/chart-template-anatomy.test.mjs.
type ApexGlobal = { chart?: Record<string, unknown> } & Record<string, unknown>;

if (typeof window !== "undefined") {
  const w = window as unknown as { Apex?: ApexGlobal };
  const prev = w.Apex ?? {};
  w.Apex = { ...prev, chart: { ...(prev.chart ?? {}), accessibility: { enabled: false } } };
}

export {};
