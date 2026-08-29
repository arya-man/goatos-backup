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
// Room at the right for the value label. A bare count needs 40; a count with its share
// ("1,061 · 63%") needs roughly twice that. Sized per chart rather than globally so a chart
// that shows no share keeps its bars exactly as long as they were.
const VALUE_GUTTER = 40;
const VALUE_GUTTER_WITH_SHARE = 84;
// Type size at scale 1, and the baseline nudge that centres it in a row.
const BASE_FONT_SIZE = 9;
const BASE_BASELINE_OFFSET = 3;

// The two rendered scales. NARROW matches the mock's original 280-wide card; WIDE suits a
// full-page-width card. The breakpoint lives in mesha-theme.css alongside the other layout rules.
const NARROW_VIEW_WIDTH = 280;
const WIDE_VIEW_WIDTH = 1100;

/**
 * How many bars stand in the card before the rest scroll (maintainer, 2026-08-12).
 *
 * The window is sized by ASPECT RATIO, not a pixel height, and that is not a stylistic choice: the
 * SVG carries no height attribute, so its rendered height is `containerWidth × viewBoxHeight /
 * viewBoxWidth`. A fixed `max-height` would therefore show ten rows at one card width and six at
 * another. Ratio `viewWidth : barsHeight(VISIBLE_BARS)` holds exactly ten rows at every width, and
 * because the two scales have different viewBox widths each needs its own ratio — both are exported
 * so mesha-theme.css cannot drift from the geometry that produced them.
 */
export const VISIBLE_BARS = 10;

/** Height of the SVG viewBox for `count` rows — the one place row geometry is turned into height. */
export function barsViewHeight(count: number): number {
  return count * (ROW_HEIGHT + ROW_GAP) + 4;
}

export const SCROLL_ASPECT_WIDE = `${WIDE_VIEW_WIDTH} / ${barsViewHeight(VISIBLE_BARS)}`;
export const SCROLL_ASPECT_NARROW = `${NARROW_VIEW_WIDTH} / ${barsViewHeight(VISIBLE_BARS)}`;

// Approximate advance width of one glyph at the rendered size. Shared by the gutter and the
// clip so the two cannot disagree about how much text fits.
const CHAR_WIDTH = 5.2;

/**
 * Label gutter sized to the labels ACTUALLY PRESENT, not to a fixed fraction of the viewBox.
 *
 * A flat fraction reserved the same column for "Anantapur Sheep" as it would for a shed name
 * three times longer, which left an obvious canyon between the names and the bars on a
 * short-label chart. Measuring the longest label closes that gap and hands the space back to
 * the bars, where it carries meaning.
 *
 * The ceiling is what keeps one very long label from squeezing every bar into nothing; past
 * it, clipLabel truncates and the full text stays in the tooltip.
 */
function labelGutterFor(viewWidth: number, textScale: number, bars: SvgBarDatum[]): number {
  const longest = bars.reduce((width, bar) => Math.max(width, bar.label.length), 0);
  const needed = Math.round(longest * CHAR_WIDTH * textScale) + 10;
  const floor = Math.round(40 * textScale);
  const ceiling = Math.round(viewWidth * 0.32);
  return Math.min(Math.max(needed, floor), ceiling);
}

/**
 * A bar's share of the series, as the label beside its count.
 *
 * The denominator is the sum of EVERY datum handed in, not of the bars actually drawn:
 * with `maxBars` truncating a long tail, sharing against the visible bars would inflate
 * each one and the column would stop adding to 100.
 *
 * A non-zero value that rounds to nothing renders "<1%" rather than "0%", because a bar
 * that is visibly there while its label says zero reads as a bug. Whole percentages
 * otherwise — a herd census does not support a decimal place, and a column of "45.8%"
 * invites a precision the source data has not got.
 */
function shareLabel(value: number, total: number): string {
  if (total <= 0) return "";
  const share = (value / total) * 100;
  if (share > 0 && share < 0.5) return "<1%";
  return `${Math.round(share)}%`;
}

// Roughly how many characters fit the gutter at the rendered font size. Longer labels are clipped
// with an ellipsis; the untruncated text stays in <title> so the value is never actually lost.
function clipLabel(label: string, gutter: number, textScale: number): string {
  const maxChars = Math.max(8, Math.floor(gutter / (CHAR_WIDTH * textScale)));
  if (label.length <= maxChars) return label;
  return `${label.slice(0, maxChars - 1)}…`;
}

function BarsSvg({
  bars,
  max,
  total,
  viewWidth,
  className,
  valueNoun,
  textScale,
}: {
  bars: SvgBarDatum[];
  max: number;
  /** Sum of the whole series; 0 turns the share off. */
  total: number;
  viewWidth: number;
  className: string;
  valueNoun: string;
  /** Multiplier on the type size and the gutters that hold it. */
  textScale: number;
}) {
  const labelGutter = labelGutterFor(viewWidth, textScale, bars);
  const valueGutter = Math.round((total > 0 ? VALUE_GUTTER_WITH_SHARE : VALUE_GUTTER) * textScale);
  const barMaxWidth = viewWidth - labelGutter - valueGutter;
  const fontSize = (BASE_FONT_SIZE * textScale).toFixed(1);
  // Baseline sits on the row's optical centre; the +3 at base size scales with the glyphs.
  const baselineOffset = BASE_BASELINE_OFFSET * textScale;
  const height = barsViewHeight(bars.length);

  return (
    // No height attribute: the viewBox aspect ratio sizes it, so the box never leaves dead
    // vertical space around a scaled-down drawing.
    // aria-hidden because the wrapper carries the accessible name — otherwise a screen reader
    // would announce the same chart twice, once per scale.
    <svg className={className} viewBox={`0 0 ${viewWidth} ${height}`} width="100%" aria-hidden="true">
      {bars.map((datum, index) => {
        const y = index * (ROW_HEIGHT + ROW_GAP) + 2;
        const barWidth = Math.max((datum.value / max) * barMaxWidth, 1);
        const share = shareLabel(datum.value, total);
        const count = datum.value.toLocaleString("en-IN");
        const tip = share
          ? `${datum.label}: ${count} ${valueNoun} · ${share}`
          : `${datum.label}: ${count} ${valueNoun}`;
        return (
          <g key={datum.key}>
            <text x="0" y={y + ROW_HEIGHT / 2 + baselineOffset} fontSize={fontSize} fill="var(--muted)">
              {clipLabel(datum.label, labelGutter, textScale)}
            </text>
            <rect
              x={labelGutter}
              y={y}
              width={barWidth.toFixed(1)}
              height={ROW_HEIGHT}
              rx="4"
              fill={SERIES_PALETTE[index % SERIES_PALETTE.length]}
              data-tip={tip}
            >
              <title>{tip}</title>
            </rect>
            <text
              x={(labelGutter + barWidth + 4).toFixed(1)}
              y={y + ROW_HEIGHT / 2 + baselineOffset}
              fontSize={fontSize}
              fill="var(--ink)"
              fontWeight="700"
            >
              {count}
              {share ? (
                // The share is the secondary reading: same row, lighter weight and colour, so
                // the count stays the figure the eye lands on.
                <tspan fill="var(--muted)" fontWeight="600">{` · ${share}`}</tspan>
              ) : null}
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
  showShare = false,
  textScale = 1,
}: {
  data: SvgBarDatum[];
  /** Resolved from the page contract by the caller. */
  emptyLabel: string;
  /** Resolved from the page contract by the caller; used in per-bar tooltips. */
  valueNoun: string;
  /** Resolved from the page contract by the caller; the chart's accessible name. */
  chartLabel: string;
  maxBars?: number;
  /**
   * Renders each bar's share of the series beside its count.
   *
   * OPT-IN, because a share is only meaningful where the series PARTITIONS one whole —
   * every live animal falls in exactly one breed, so "46%" means something. On a series of
   * unrelated magnitudes, or one capped by `maxBars` so the visible bars are a sample
   * rather than the set, a percentage would invent a denominator the chart cannot back.
   */
  showShare?: boolean;
  /**
   * Multiplier on the type size, applied to the WIDE scale only.
   *
   * Only the wide drawing needs it. The narrow one packs the same rows into a 280-unit
   * viewBox, so a phone already scales those glyphs up far more than a desktop card scales
   * the 1100-unit one — and enlarging the type there would eat the gutters until there was
   * no room left to draw the bar. Default 1 leaves every existing chart untouched.
   */
  textScale?: number;
}) {
  const bars = data.filter((d) => d.value > 0).slice(0, maxBars);
  // Shared against EVERY datum handed in, including any the cap or the zero filter dropped,
  // so the visible percentages never add to more than the whole they came from.
  const total = showShare ? data.reduce((sum, d) => sum + d.value, 0) : 0;

  if (bars.length === 0) {
    return (
      <div className="muted small" style={{ padding: "14px 2px", textAlign: "center" }}>
        {emptyLabel}
      </div>
    );
  }

  const max = Math.max(...bars.map((d) => d.value)) || 1;
  // The window only appears once there is something to scroll to. Applied unconditionally it would
  // stretch a three-bar chart to ten rows of empty card.
  const scrolls = bars.length > VISIBLE_BARS;

  return (
    <div
      className={`svgbars${scrolls ? " svgbars-scroll" : ""}`}
      role="img"
      aria-label={chartLabel}
      // Keyboard-reachable when it scrolls: a scroll region a keyboard user cannot focus is one they
      // cannot read past row ten. Left alone when everything fits, so a short chart does not add a
      // pointless tab stop.
      tabIndex={scrolls ? 0 : undefined}
    >
      <BarsSvg
        bars={bars}
        max={max}
        total={total}
        viewWidth={WIDE_VIEW_WIDTH}
        className="svgbars-wide"
        valueNoun={valueNoun}
        textScale={textScale}
      />
      <BarsSvg
        bars={bars}
        max={max}
        total={total}
        viewWidth={NARROW_VIEW_WIDTH}
        className="svgbars-narrow"
        valueNoun={valueNoun}
        textScale={1}
      />
    </div>
  );
}
