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
};

export type ChartBarLayout = {
  kind: "bar";
  bars: ChartBar[];
};

export type ChartLinePoint = {
  key: string;
  label: string;
  value: number;
  cx: number;
  cy: number;
};

export type ChartLineLayout = {
  kind: "line";
  viewWidth: number;
  viewHeight: number;
  path: string;
  points: ChartLinePoint[];
  color: string;
  baselineY: number;
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

export function chartLayout(chart: CeoAiChart): ChartLayout | null {
  if (!isRenderableChart(chart)) return null;
  const series = firstSeries(chart);
  if (!series) return null;
  const n = Math.min(chart.x.length, series.data.length);
  const labels = chart.x.slice(0, n);
  const data = series.data.slice(0, n);
  const max = Math.max(...data.map((v) => Math.abs(v)), 1);

  if (chart.type === "line") {
    return lineLayout(labels, data, max);
  }
  return barLayout(labels, data, max);
}

function barLayout(labels: string[], data: number[], max: number): ChartBarLayout {
  const bars: ChartBar[] = data.map((value, i) => {
    const pct = Math.min(100, Math.max((Math.abs(value) / max) * 100, 1));
    return {
      key: `${i}-${labels[i]}`,
      label: String(labels[i] ?? ""),
      value,
      valueLabel: formatChartValue(value),
      pct: Number(pct.toFixed(1)),
      color: CHART_PALETTE[i % CHART_PALETTE.length],
    };
  });
  return { kind: "bar", bars };
}

function lineLayout(labels: string[], data: number[], max: number): ChartLineLayout {
  const innerW = VIEW_WIDTH - LINE_PAD_X * 2;
  const innerH = LINE_HEIGHT - LINE_PAD_TOP - LINE_PAD_BOTTOM;
  const baselineY = LINE_HEIGHT - LINE_PAD_BOTTOM;
  const step = data.length > 1 ? innerW / (data.length - 1) : 0;
  const points: ChartLinePoint[] = data.map((value, i) => {
    const cx = LINE_PAD_X + step * i;
    const cy = baselineY - (Math.max(value, 0) / max) * innerH;
    return {
      key: `${i}-${labels[i]}`,
      label: String(labels[i] ?? ""),
      value,
      cx: Number(cx.toFixed(1)),
      cy: Number(cy.toFixed(1)),
    };
  });
  const path = points.map((p, i) => `${i === 0 ? "M" : "L"}${p.cx} ${p.cy}`).join(" ");
  return {
    kind: "line",
    viewWidth: VIEW_WIDTH,
    viewHeight: LINE_HEIGHT,
    path,
    points,
    color: CHART_PALETTE[0],
    baselineY,
    ticks: lineTicks(points.length, tickBudget(labels)),
  };
}

// chartAccessibleLabel builds the screen-reader summary from real values.
export function chartAccessibleLabel(chart: CeoAiChart): string {
  const series = firstSeries(chart);
  if (!series) return chart.title;
  const n = Math.min(chart.x.length, series.data.length);
  const parts = chart.x.slice(0, n).map((label, i) => `${label}: ${series.data[i]}`);
  return `${chart.title}. ${parts.join(", ")}`;
}
