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

export type ChartBar = {
  key: string;
  label: string;
  value: number;
  x: number;
  y: number;
  width: number;
  height: number;
  labelX: number;
  valueX: number;
  textY: number;
  color: string;
};

export type ChartBarLayout = {
  kind: "bar";
  viewWidth: number;
  viewHeight: number;
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
};

export type ChartLayout = ChartBarLayout | ChartLineLayout;

// Geometry constants tuned for a chat bubble (~narrow). A single viewBox scales
// uniformly to the container width.
const VIEW_WIDTH = 320;
const BAR_ROW_HEIGHT = 20;
const BAR_ROW_GAP = 8;
const BAR_LABEL_GUTTER = 92;
const BAR_VALUE_GUTTER = 34;
const LINE_HEIGHT = 150;
const LINE_PAD_X = 10;
const LINE_PAD_TOP = 12;
const LINE_PAD_BOTTOM = 26;

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

function clip(label: string, maxChars: number): string {
  if (label.length <= maxChars) return label;
  return `${label.slice(0, Math.max(1, maxChars - 1))}…`;
}

export function chartLayout(chart: CeoAiChart): ChartLayout | null {
  if (!isRenderableChart(chart)) return null;
  const series = firstSeries(chart);
  if (!series) return null;
  const n = Math.min(chart.x.length, series.data.length);
  const labels = chart.x.slice(0, n);
  const data = series.data.slice(0, n);
  const max = Math.max(...data, 1);

  if (chart.type === "line") {
    return lineLayout(labels, data, max);
  }
  return barLayout(labels, data, max);
}

function barLayout(labels: string[], data: number[], max: number): ChartBarLayout {
  const barMaxWidth = VIEW_WIDTH - BAR_LABEL_GUTTER - BAR_VALUE_GUTTER;
  const viewHeight = data.length * (BAR_ROW_HEIGHT + BAR_ROW_GAP) + 4;
  const maxChars = Math.floor(BAR_LABEL_GUTTER / 5.4);
  const bars: ChartBar[] = data.map((value, i) => {
    const y = i * (BAR_ROW_HEIGHT + BAR_ROW_GAP) + 2;
    const width = Math.max((value / max) * barMaxWidth, 1);
    return {
      key: `${i}-${labels[i]}`,
      label: clip(labels[i], maxChars),
      value,
      x: BAR_LABEL_GUTTER,
      y,
      width: Number(width.toFixed(1)),
      height: BAR_ROW_HEIGHT,
      labelX: 0,
      valueX: Number((BAR_LABEL_GUTTER + width + 4).toFixed(1)),
      textY: y + BAR_ROW_HEIGHT / 2 + 3,
      color: CHART_PALETTE[i % CHART_PALETTE.length],
    };
  });
  return { kind: "bar", viewWidth: VIEW_WIDTH, viewHeight, bars };
}

function lineLayout(labels: string[], data: number[], max: number): ChartLineLayout {
  const innerW = VIEW_WIDTH - LINE_PAD_X * 2;
  const innerH = LINE_HEIGHT - LINE_PAD_TOP - LINE_PAD_BOTTOM;
  const baselineY = LINE_HEIGHT - LINE_PAD_BOTTOM;
  const step = data.length > 1 ? innerW / (data.length - 1) : 0;
  const points: ChartLinePoint[] = data.map((value, i) => {
    const cx = LINE_PAD_X + step * i;
    const cy = baselineY - (value / max) * innerH;
    return {
      key: `${i}-${labels[i]}`,
      label: labels[i],
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
