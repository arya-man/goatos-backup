// Horizontal bar chart, ported from the mock's `svgHBars` helper
// (the dashboard mock) together with its `palette()` series colours.
//
// Deliberately NOT a charting library. The mock's chart anatomy is dependency-free inline SVG,
// and the mock is the UI source of truth, so porting it keeps this a pure SERVER component:
// no "use client", no client bundle, no hydration. recharts is in package.json with zero
// importers — do not make this its first use without a chart that actually needs interaction.
//
// Every colour is a CSS custom property, never a hex literal, so the series stay theme-correct
// in light and dark and the mock-fidelity banned-hex scan passes by construction.
//
// This component renders NO copy of its own. Titles, captions, legends, empty states and the
// value noun are all passed in already resolved from the backend page contract by the caller,
// so every visible string on the page stays traceable to one copy(pageContract, ...) call.
//
// RESPONSIVE: an SVG scales its viewBox UNIFORMLY to the container, so a single fixed viewBox
// cannot stay readable at both widths — a 1100-wide box inside a 330px phone card renders at
// ~30% scale, shrinking 20px rows to ~6px of unreadable text. Row height only stays constant if
// the viewBox width tracks the container width, so this renders the bars at BOTH scales and lets
// one CSS media query show the right one. Two static SVGs, no JS, no layout measurement.

export type SvgBarDatum = {
  key: string;
  label: string;
  value: number;
};

// The mock's palette(), in order. Seven series colours that exist in both themes.
const SERIES_PALETTE = [
  "var(--brand)",
  "var(--info)",
  "var(--amber)",
  "var(--purple)",
  "var(--teal)",
  "var(--danger)",
  "var(--ok)",
] as const;

// Mock geometry: 20px rows on a 6px gap, with a label gutter and a value gutter either side.
const ROW_HEIGHT = 20;
const ROW_GAP = 6;
const VALUE_GUTTER = 40;

// The two rendered scales. NARROW matches the mock's original 280-wide card; WIDE suits a
// full-page-width card. The breakpoint lives in mesha-theme.css alongside the other layout rules.
const NARROW_VIEW_WIDTH = 280;
const WIDE_VIEW_WIDTH = 1100;

// Label gutter tracks the viewBox so long shed names get proportionally more room on a wide card
// instead of being clipped at the narrow card's 78px.
function labelGutterFor(viewWidth: number): number {
  return Math.max(78, Math.round(viewWidth * 0.16));
}

// Roughly how many characters fit the gutter at font-size 9. Longer labels are clipped with an
// ellipsis; the untruncated text stays in <title> so the value is never actually lost.
function clipLabel(label: string, gutter: number): string {
  const maxChars = Math.max(8, Math.floor(gutter / 5.2));
  if (label.length <= maxChars) return label;
  return `${label.slice(0, maxChars - 1)}…`;
}

function BarsSvg({
  bars,
  max,
  viewWidth,
  className,
  valueNoun,
}: {
  bars: SvgBarDatum[];
  max: number;
  viewWidth: number;
  className: string;
  valueNoun: string;
}) {
  const labelGutter = labelGutterFor(viewWidth);
  const barMaxWidth = viewWidth - labelGutter - VALUE_GUTTER;
  const height = bars.length * (ROW_HEIGHT + ROW_GAP) + 4;

  return (
    // No height attribute: the viewBox aspect ratio sizes it, so the box never leaves dead
    // vertical space around a scaled-down drawing.
    // aria-hidden because the wrapper carries the accessible name — otherwise a screen reader
    // would announce the same chart twice, once per scale.
    <svg className={className} viewBox={`0 0 ${viewWidth} ${height}`} width="100%" aria-hidden="true">
      {bars.map((datum, index) => {
        const y = index * (ROW_HEIGHT + ROW_GAP) + 2;
        const barWidth = Math.max((datum.value / max) * barMaxWidth, 1);
        return (
          <g key={datum.key}>
            <text x="0" y={y + ROW_HEIGHT / 2 + 3} fontSize="9" fill="var(--muted)">
              {clipLabel(datum.label, labelGutter)}
            </text>
            <rect
              x={labelGutter}
              y={y}
              width={barWidth.toFixed(1)}
              height={ROW_HEIGHT}
              rx="4"
              fill={SERIES_PALETTE[index % SERIES_PALETTE.length]}
            >
              <title>{`${datum.label}: ${datum.value} ${valueNoun}`}</title>
            </rect>
            <text
              x={(labelGutter + barWidth + 4).toFixed(1)}
              y={y + ROW_HEIGHT / 2 + 3}
              fontSize="9"
              fill="var(--ink)"
              fontWeight="700"
            >
              {datum.value}
            </text>
          </g>
        );
      })}
    </svg>
  );
}

export function SvgBars({
  data,
  emptyLabel,
  valueNoun,
  chartLabel,
  maxBars = 8,
}: {
  data: SvgBarDatum[];
  /** Resolved from the page contract by the caller. */
  emptyLabel: string;
  /** Resolved from the page contract by the caller; used in per-bar tooltips. */
  valueNoun: string;
  /** Resolved from the page contract by the caller; the chart's accessible name. */
  chartLabel: string;
  maxBars?: number;
}) {
  const bars = data.filter((d) => d.value > 0).slice(0, maxBars);

  if (bars.length === 0) {
    return (
      <div className="muted small" style={{ padding: "14px 2px", textAlign: "center" }}>
        {emptyLabel}
      </div>
    );
  }

  const max = Math.max(...bars.map((d) => d.value)) || 1;

  return (
    <div className="svgbars" role="img" aria-label={chartLabel}>
      <BarsSvg bars={bars} max={max} viewWidth={WIDE_VIEW_WIDTH} className="svgbars-wide" valueNoun={valueNoun} />
      <BarsSvg bars={bars} max={max} viewWidth={NARROW_VIEW_WIDTH} className="svgbars-narrow" valueNoun={valueNoun} />
    </div>
  );
}
