// Pure geometry for the leadership assistant's inline answer chart.
//
// This is the SANCTIONED mesha viz pattern — dependency-free inline SVG, the
// same anatomy as `components/svg-bars.tsx` (mock `svgHBars`), ported here and
// extended with a line mode for trend answers. recharts stays UNUSED. Colours
// are CSS custom properties resolved by the caller, never hex literals, so the
// chart is theme-correct in light and dark.
//
// Kept side-effect free (no JSX, no React) so it is unit-testable under
// `node --test` and so the component is a thin renderer over these coordinates.

export type CeoAiChartSeries = { name: string; data: number[] };
export type CeoAiChart = {
  type: "bar" | "line";
  title: string;
  x: string[];
  series: CeoAiChartSeries[];
};

// Mock palette(), in order — series colours that exist in both themes.
export const CHART_PALETTE = [
  "var(--brand)",
  "var(--info)",
  "var(--amber)",
  "var(--purple)",
  "var(--teal)",
  "var(--danger)",
  "var(--ok)",
] as const;

// Bars render as HTML rows (label above a proportional track) rather than SVG
// text: real text keeps category labels at a readable 12-13px, wraps long
// names instead of truncating them ("Mesha Kids Conc…" twice looked identical),
// and never overflows a 390px phone bubble.
export type ChartBar = {
  key: string;
  label: string;
  value: number;
  valueLabel: string;
  // Bar length as a percentage (0-100] of the track; min 1 so zero rows show.
  pct: number;
  color: string;
  // One entry per series when the chart compares several (Coimbatore vs Channapatna); the
  // first entry is the bar itself. Absent for a single-series chart.
  parts?: { name: string; value: number; valueLabel: string; pct: number; color: string }[];
};

export type ChartLegendItem = { name: string; color: string };

export type ChartBarLayout = {
  kind: "bar";
  bars: ChartBar[];
  legend: ChartLegendItem[];
};

export type ChartLinePoint = {
  key: string;
  label: string;
  value: number;
  cx: number;
  cy: number;
};

export type ChartLine = { name: string; color: string; path: string; points: ChartLinePoint[] };

// A y-axis label: `pct` is its position from the top of the plot (0-100).
export type ChartYTick = { value: number; label: string; pct: number };

export type ChartLineLayout = {
  kind: "line";
  viewWidth: number;
  viewHeight: number;
  // path/points/color are the first series (kept for callers that draw one line).
  path: string;
  points: ChartLinePoint[];
  color: string;
  // Every series, each with its own colour; the legend names them when there is more than one.
  lines: ChartLine[];
  legend: ChartLegendItem[];
  yTicks: ChartYTick[];
  baselineY: number;
  // y of the zero line when the values cross zero (a week that LOST weight), else null.
  zeroY: number | null;
  // Indexes of x labels to show (thinned so they never collide on a phone).
  ticks: number[];
};

export type ChartLayout = ChartBarLayout | ChartLineLayout;

// Geometry constants tuned for a chat bubble (~narrow). A single viewBox scales
// uniformly to the container width.
const VIEW_WIDTH = 320;
const LINE_HEIGHT = 130;
const MAX_LINE_TICKS = 5;
const LINE_PAD_X = 10;
const LINE_PAD_TOP = 12;
const LINE_PAD_BOTTOM = 8;

// firstSeries returns the single rendered series (the composer emits one).
function firstSeries(chart: CeoAiChart): CeoAiChartSeries | null {
  const s = chart.series?.[0];
  if (!s || !Array.isArray(s.data) || s.data.length === 0) return null;
  return s;
}

// isRenderable guards the render: at least two aligned, finite points.
export function isRenderableChart(chart: CeoAiChart | undefined | null): chart is CeoAiChart {
  if (!chart || (chart.type !== "bar" && chart.type !== "line")) return false;
  const s = chart.series?.[0];
  if (!s || !Array.isArray(s.data) || s.data.length < 2) return false;
  if (!Array.isArray(chart.x) || chart.x.length < 2) return false;
  const n = Math.min(chart.x.length, s.data.length);
  if (n < 2) return false;
  return s.data.slice(0, n).every((v) => Number.isFinite(v));
}

// formatChartValue keeps value labels short and readable (1,234 / 12.5).
export function formatChartValue(value: number): string {
  const rounded = Math.abs(value) >= 100 ? Math.round(value) : Math.round(value * 10) / 10;
  return rounded.toLocaleString("en-US");
}

// lineTicks picks at most `max` evenly spaced label indexes, always including
// the first and last point.
export function lineTicks(n: number, max = MAX_LINE_TICKS): number[] {
  if (n <= 0) return [];
  if (n <= max) return Array.from({ length: n }, (_, i) => i);
  const out = new Set<number>();
  for (let k = 0; k < max; k += 1) out.add(Math.round((k * (n - 1)) / (max - 1)));
  return [...out].sort((a, b) => a - b);
}

// tickBudget shows fewer x labels when they are long ("W27 (01/07)"), so a
// phone-width chart keeps them readable instead of ellipsised or colliding.
export function tickBudget(labels: string[]): number {
  const longest = Math.max(0, ...labels.map((l) => String(l).length));
  return longest > 10 ? 3 : longest > 6 ? 4 : MAX_LINE_TICKS;
}

// renderableSeries: every series with a finite value for each of the first n x labels (the first
// series is already guaranteed by isRenderableChart). Capped at the palette size.
function renderableSeries(chart: CeoAiChart, n: number): CeoAiChartSeries[] {
  return (chart.series ?? [])
    .filter((s) => Array.isArray(s?.data) && s.data.length >= n && s.data.slice(0, n).every((v) => Number.isFinite(v)))
    .slice(0, CHART_PALETTE.length)
    .map((s, i) => ({ name: String(s.name || `Series ${i + 1}`), data: s.data.slice(0, n) }));
}

export function chartLayout(chart: CeoAiChart): ChartLayout | null {
  if (!isRenderableChart(chart)) return null;
  const first = firstSeries(chart);
  if (!first) return null;
  const n = Math.min(chart.x.length, first.data.length);
  const labels = chart.x.slice(0, n);
  const series = renderableSeries(chart, n);
  if (!series.length) return null;
  const legend = series.length > 1 ? series.map((s, i) => ({ name: s.name, color: CHART_PALETTE[i] })) : [];

  if (chart.type === "line") {
    return lineLayout(labels, series, legend);
  }
  return barLayout(labels, series, legend);
}

function barLayout(labels: string[], series: CeoAiChartSeries[], legend: ChartLegendItem[]): ChartBarLayout {
  const max = Math.max(...series.flatMap((s) => s.data.map((v) => Math.abs(v))), 1);
  const pctOf = (value: number) => Number(Math.min(100, Math.max((Math.abs(value) / max) * 100, 1)).toFixed(1));
  const multi = series.length > 1;
  const bars: ChartBar[] = labels.map((label, i) => {
    const value = series[0].data[i];
    const bar: ChartBar = {
      key: `${i}-${label}`,
      label: String(label ?? ""),
      value,
      valueLabel: formatChartValue(value),
      pct: pctOf(value),
      // One series: a colour per row (as before). Several: a colour per series.
      color: multi ? CHART_PALETTE[0] : CHART_PALETTE[i % CHART_PALETTE.length],
    };
    if (multi) {
      bar.parts = series.map((s, k) => ({
        name: s.name, value: s.data[i], valueLabel: formatChartValue(s.data[i]), pct: pctOf(s.data[i]), color: CHART_PALETTE[k],
      }));
    }
    return bar;
  });
  return { kind: "bar", bars, legend };
}

// niceStep rounds a raw tick gap to 1/2/2.5/5 x 10^k so axis labels read cleanly.
function niceStep(raw: number): number {
  const p = 10 ** Math.floor(Math.log10(raw));
  const f = raw / p;
  return (f <= 1 ? 1 : f <= 2 ? 2 : f <= 2.5 ? 2.5 : f <= 5 ? 5 : 10) * p;
}

// lineDomain: the y range the plot spans. Counts and gains that sit near zero start at zero; a
// narrow band far from zero (weights 24-39 kg) is not squashed flat against a zero baseline, and a
// negative value (a week that lost weight) is drawn below zero, never clamped up to it.
export function lineDomain(values: number[]): { lo: number; hi: number; step: number } {
  let lo = Math.min(...values);
  let hi = Math.max(...values);
  if (lo >= 0 && lo <= hi * 0.5) lo = 0;
  if (hi <= 0 && hi >= lo * 0.5) hi = 0;
  if (hi === lo) { hi += Math.abs(hi) || 1; lo -= lo === 0 ? 0 : Math.abs(lo) * 0.1; }
  const step = niceStep((hi - lo) / 3);
  return { lo: Math.floor(lo / step) * step, hi: Math.ceil(hi / step) * step, step };
}

function lineLayout(labels: string[], series: CeoAiChartSeries[], legend: ChartLegendItem[]): ChartLineLayout {
  const innerW = VIEW_WIDTH - LINE_PAD_X * 2;
  const innerH = LINE_HEIGHT - LINE_PAD_TOP - LINE_PAD_BOTTOM;
  const baselineY = LINE_HEIGHT - LINE_PAD_BOTTOM;
  const step = labels.length > 1 ? innerW / (labels.length - 1) : 0;
  const { lo, hi, step: yStep } = lineDomain(series.flatMap((s) => s.data));
  const yOf = (v: number) => baselineY - ((v - lo) / (hi - lo)) * innerH;
  const lines: ChartLine[] = series.map((s, k) => {
    const points: ChartLinePoint[] = s.data.map((value, i) => ({
      key: `${k}-${i}-${labels[i]}`,
      label: String(labels[i] ?? ""),
      value,
      cx: Number((LINE_PAD_X + step * i).toFixed(1)),
      cy: Number(yOf(value).toFixed(1)),
    }));
    const path = points.map((p, i) => `${i === 0 ? "M" : "L"}${p.cx} ${p.cy}`).join(" ");
    return { name: s.name, color: CHART_PALETTE[k], path, points };
  });
  const yTicks: ChartYTick[] = [];
  for (let v = hi; v >= lo - yStep / 2; v -= yStep) {
    const value = Number(v.toFixed(6));
    yTicks.push({ value, label: formatChartValue(value), pct: Number(((yOf(value) / LINE_HEIGHT) * 100).toFixed(1)) });
  }
  return {
    kind: "line",
    viewWidth: VIEW_WIDTH,
    viewHeight: LINE_HEIGHT,
    path: lines[0].path,
    points: lines[0].points,
    color: lines[0].color,
    lines,
    legend,
    yTicks,
    baselineY,
    zeroY: lo < 0 && hi > 0 ? Number(yOf(0).toFixed(1)) : null,
    ticks: lineTicks(labels.length, tickBudget(labels)),
  };
}

// chartAccessibleLabel builds the screen-reader summary from real values.
export function chartAccessibleLabel(chart: CeoAiChart): string {
  const first = firstSeries(chart);
  if (!first) return chart.title;
  const n = Math.min(chart.x.length, first.data.length);
  const series = renderableSeries(chart, n);
  if (series.length <= 1) {
    const parts = chart.x.slice(0, n).map((label, i) => `${label}: ${first.data[i]}`);
    return `${chart.title}. ${parts.join(", ")}`;
  }
  const parts = series.map((s) => `${s.name}: ${chart.x.slice(0, n).map((label, i) => `${label} ${s.data[i]}`).join(", ")}`);
  return `${chart.title}. ${parts.join("; ")}`;
}
