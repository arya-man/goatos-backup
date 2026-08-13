// Compact day-by-day column chart ("did we keep up?"), ported from the mock's `svgBars` column
// anatomy: rounded rects on a baseline rule, a `<title>` per column, and CSS-custom-property fills.
//
// Same rules as its sibling `svg-bars.tsx`: a pure SERVER component, no "use client", no charting
// library (recharts is in package.json with zero importers -- do not make this its first use), no
// hex literals, and NO copy of its own. Every visible string -- the accessible chart name, the
// tooltip nouns, the first/last axis labels, the empty state -- is passed in already resolved from
// the backend page contract by the caller.
//
// Two series per day on purpose: one bar alone answers "how fast are we going" and cannot answer
// "are we keeping up". The comparison series is the WIDE, faint column behind and the primary series
// the solid column in front, so the reading is "how much of what arrived did we get through". A thin
// top-marker was tried first and failed the real data: with 1,672 arrivals against 65 verdicts the
// marker sat at the top of an empty column and the verdict bars were invisible.

export type SvgColumnDatum = {
  key: string;
  /** Human label for this column's tooltip, resolved by the caller. */
  label: string;
  value: number;
  /** Optional comparison value drawn as an overlay marker on the same column. */
  compareValue?: number;
};

// Geometry mirrors the mock's svgBars: a fixed viewBox scaled to the container width.
const VIEW_W = 560;
const VIEW_H = 96;
const PAD_X = 6;
const PAD_TOP = 10;
const BASELINE = VIEW_H - 16;

export function SvgColumnBars({
  data,
  chartLabel,
  valueNoun,
  compareNoun,
  emptyLabel,
}: {
  data: SvgColumnDatum[];
  /** Resolved from the page contract by the caller; the chart's accessible name. */
  chartLabel: string;
  /** Resolved from the page contract by the caller; used in per-column tooltips. */
  valueNoun: string;
  /** Resolved from the page contract by the caller; names the comparison series in tooltips. */
  compareNoun?: string;
  /** Resolved from the page contract by the caller. */
  emptyLabel: string;
}) {
  if (data.length === 0) {
    return (
      <div className="muted small" style={{ padding: "12px 2px", textAlign: "center" }}>
        {emptyLabel}
      </div>
    );
  }
  // The two series share ONE scale. Scaling each to its own max would draw a day with 3 verdicts
  // against 40 arrivals as two equal-height marks -- the exact comparison this chart exists to make,
  // silently inverted.
  const max = Math.max(1, ...data.map((d) => Math.max(d.value, d.compareValue ?? 0)));
  const slot = (VIEW_W - 2 * PAD_X) / data.length;
  const barWidth = Math.min(slot * 0.62, 26);
  const plotHeight = BASELINE - PAD_TOP;

  return (
    <svg
      viewBox={`0 0 ${VIEW_W} ${VIEW_H}`}
      width="100%"
      height={VIEW_H}
      preserveAspectRatio="none"
      role="img"
      aria-label={chartLabel}
      style={{ display: "block" }}
    >
      <line x1={PAD_X} y1={BASELINE} x2={VIEW_W - PAD_X} y2={BASELINE} stroke="var(--line)" strokeWidth={1} />
      {data.map((datum, index) => {
        const compare = datum.compareValue ?? 0;
        const backWidth = Math.min(slot * 0.86, 38);
        const backX = PAD_X + index * slot + (slot - backWidth) / 2;
        const backHeight = Math.max((compare / max) * plotHeight, compare > 0 ? 2 : 0);
        const x = PAD_X + index * slot + (slot - barWidth) / 2;
        const height = Math.max((datum.value / max) * plotHeight, datum.value > 0 ? 2 : 0);
        return (
          <g key={datum.key}>
            {datum.compareValue == null ? null : (
              <rect
                x={backX}
                y={BASELINE - backHeight}
                width={backWidth}
                height={backHeight}
                rx={3}
                fill="var(--amber)"
                opacity={0.32}
              />
            )}
            <rect x={x} y={BASELINE - height} width={barWidth} height={height} rx={3} fill="var(--brand)" />
            {/* One hit area per day spanning the full slot, so a day with zero verdicts still has a
                tooltip -- a zero day is exactly the one a reader wants to interrogate. */}
            <rect x={PAD_X + index * slot} y={PAD_TOP} width={slot} height={BASELINE - PAD_TOP} fill="transparent">
              <title>
                {datum.label}: {datum.value} {valueNoun}
                {compareNoun ? ` · ${compare} ${compareNoun}` : ""}
              </title>
            </rect>
          </g>
        );
      })}
    </svg>
  );
}
