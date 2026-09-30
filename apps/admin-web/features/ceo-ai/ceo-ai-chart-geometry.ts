// Pure geometry for the leadership assistant's inline answer chart.
//
// It decides WHAT an answer can draw (points, gaps, bar vs line); the drawing is
// the template ApexCharts Chart in ceo-ai-chart.tsx. Colours are CSS custom
// properties resolved by the caller, never hex literals, so the chart is
// theme-correct in light and dark.
//
// Kept side-effect free (no JSX, no React) so it is unit-testable under
// `node --test` and so the component is a thin renderer over these coordinates.

// null (or a missing entry) = no reading for that x label. It is drawn as a gap / "–", never as 0.
export type CeoAiChartSeries = { name: string; data: (number | null)[] };
export type CeoAiChart = {
  type: "bar" | "line";
  title: string;
  x: string[];
  series: CeoAiChartSeries[];
};

// Mock palette(), in order — series colours that exist in both themes.
export const CHART_PALETTE = [
  "var(--palette-primary-main)",
  "var(--palette-info-main)",
  "var(--amber)",
  "var(--palette-secondary-main)",
  "var(--teal)",
  "var(--palette-error-main)",
  "var(--palette-success-main)",
] as const;

// Bars render as HTML rows (label above a proportional track) rather than SVG
// text: real text keeps category labels at a readable 12-13px, wraps long
// names instead of truncating them ("Mesha Kids Conc…" twice looked identical),
// and never overflows a 390px phone bubble.
export type ChartBar = {
  key: string;
  label: string;
  value: number | null;
  valueLabel: string;
  // Bar length as a percentage (0-100] of the track; min 1 so zero rows show.
  pct: number;
  color: string;
  // One entry per series when the chart compares several (Coimbatore vs Channapatna); the
  // first entry is the bar itself. Absent for a single-series chart.
  parts?: { name: string; value: number | null; valueLabel: string; pct: number; color: string }[];
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
  value: number | null;
  cx: number;
  // null when the series has no reading at this x (the line breaks, no dot).
  cy: number | null;
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

// isValue: a real reading. null/undefined are missing; NaN/Infinity/strings are malformed.
const isValue = (v: unknown): v is number => typeof v === "number" && Number.isFinite(v);
const isMissing = (v: unknown) => v === null || v === undefined;

// cleanSeries: a series whose first n entries are all readings or missing (never NaN / "12"),
// with at least one real reading; missing entries are normalised to null. (A pen measured once
// still belongs in a comparison; the chart as a whole needs one series with >= 2 readings.)
function cleanSeries(s: CeoAiChartSeries | undefined, n: number, i: number): CeoAiChartSeries | null {
  if (!s || !Array.isArray(s.data)) return null;
  const data = Array.from({ length: n }, (_, k) => s.data[k]);
  if (!data.every((v) => isValue(v) || isMissing(v))) return null;
  if (data.filter(isValue).length < 1) return null;
  return { name: String(s.name || `Series ${i + 1}`), data: data.map((v) => (isValue(v) ? v : null)) };
}

// isRenderable guards the render: >= 2 x labels and a series with >= 2 real readings.
// Missing readings (null) are allowed; a malformed one (NaN, a string) rejects the chart.
export function isRenderableChart(chart: CeoAiChart | undefined | null): chart is CeoAiChart {
  if (!chart || (chart.type !== "bar" && chart.type !== "line")) return false;
  if (!Array.isArray(chart.x) || chart.x.length < 2 || !Array.isArray(chart.series)) return false;
  // Every series must be well-formed (a malformed one rejects the chart, never silently vanishes).
  const all = chart.series.map((s, i) => cleanSeries(s, chart.x.length, i));
  if (!all.length || all.some((s) => s === null)) return false;
  return all.some((s) => s!.data.filter(isValue).length >= 2);
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

// renderableSeries: every drawable series, aligned to the x labels (short data = trailing
// gaps), capped at the palette size so no two series share a colour.
function renderableSeries(chart: CeoAiChart): CeoAiChartSeries[] {
  const n = Array.isArray(chart.x) ? chart.x.length : 0;
  return (chart.series ?? [])
    .map((s, i) => cleanSeries(s, n, i))
    .filter((s): s is CeoAiChartSeries => s !== null)
    .slice(0, CHART_PALETTE.length);
}

export function chartLayout(chart: CeoAiChart): ChartLayout | null {
  if (!isRenderableChart(chart)) return null;
  const labels = chart.x.map((l) => String(l ?? ""));
  const series = renderableSeries(chart);
  if (!series.length) return null;
  const legend = series.length > 1 ? series.map((s, i) => ({ name: s.name, color: CHART_PALETTE[i] })) : [];

  if (chart.type === "line") {
    return lineLayout(labels, series, legend);
  }
  return barLayout(labels, series, legend);
}

const MISSING_LABEL = "–";
const valuesOf = (series: CeoAiChartSeries[]) => series.flatMap((s) => s.data.filter(isValue));

function barLayout(labels: string[], series: CeoAiChartSeries[], legend: ChartLegendItem[]): ChartBarLayout {
  const max = Math.max(...valuesOf(series).map((v) => Math.abs(v)), 1);
  // Missing = no bar at all (0%); a real zero still shows a 1% sliver.
  const pctOf = (value: number | null) =>
    value === null ? 0 : Number(Math.min(100, Math.max((Math.abs(value) / max) * 100, 1)).toFixed(1));
  const labelOf = (value: number | null) => (value === null ? MISSING_LABEL : formatChartValue(value));
  const multi = series.length > 1;
  const bars: ChartBar[] = labels.map((label, i) => {
    const value = series[0].data[i];
    const bar: ChartBar = {
      key: `${i}-${label}`,
      label: String(label ?? ""),
      value,
      valueLabel: labelOf(value),
      pct: pctOf(value),
      // Colour means "which series", never "which row": one series = one colour, so weeks of
      // the same measure never look like different things.
      // Exception: a single-series loss (-18) must not look like a gain the same length.
      color: !multi && value !== null && value < 0 ? "var(--palette-error-main)" : CHART_PALETTE[0],
    };
    if (multi) {
      bar.parts = series.map((s, k) => ({
        name: s.name, value: s.data[i], valueLabel: labelOf(s.data[i]), pct: pctOf(s.data[i]), color: CHART_PALETTE[k],
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
  const { lo, hi, step: yStep } = lineDomain(valuesOf(series));
  const yOf = (v: number) => baselineY - ((v - lo) / (hi - lo)) * innerH;
  const lines: ChartLine[] = series.map((s, k) => {
    const points: ChartLinePoint[] = s.data.map((value, i) => ({
      key: `${k}-${i}-${labels[i]}`,
      label: String(labels[i] ?? ""),
      value,
      cx: Number((LINE_PAD_X + step * i).toFixed(1)),
      cy: value === null ? null : Number(yOf(value).toFixed(1)),
    }));
    // A missing reading breaks the line (new "M" after the gap) instead of dipping to 0.
    const path = points
      .flatMap((p, i) => (p.cy === null ? [] : [`${points[i - 1]?.cy != null ? "L" : "M"}${p.cx} ${p.cy}`]))
      .join(" ");
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
  const series = renderableSeries(chart);
  if (!series.length) return chart.title;
  const v = (x: number | null) => (x === null ? "no data" : String(x));
  if (series.length === 1) {
    const parts = chart.x.map((label, i) => `${label}: ${v(series[0].data[i])}`);
    return `${chart.title}. ${parts.join(", ")}`;
  }
  const parts = series.map((s) => `${s.name}: ${chart.x.map((label, i) => `${label} ${v(s.data[i])}`).join(", ")}`);
  return `${chart.title}. ${parts.join("; ")}`;
}
