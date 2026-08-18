// Readable horizontal bar list for full/half-width report cards (sales board).
//
// The sibling `svg-bars.tsx` ports the mock's inline-SVG `svgHBars` anatomy, whose text scales
// WITH the drawing — right for the mock's compact 280px chart cards (Counts Breakdown), wrong for
// a wide report card where a 9px SVG label renders at ~5px. This variant keeps the same visual
// language (label · bar · value, the mock's palette tokens, rounded track) but lays it out in
// HTML so the labels hold a fixed readable size at every card width.
//
// Same rules as the SVG siblings: a pure SERVER component, no "use client", no charting library,
// no hex literals (palette is CSS custom properties), and NO copy of its own — every visible
// string arrives already resolved from the backend page contract by the caller.

export type HBarDatum = {
  key: string;
  label: string;
  value: number;
  /** Optional pre-formatted value label (e.g. "₹467 per kg"); falls back to the raw value. */
  display?: string;
};

// The mock's palette(), in order — same series colours as svg-bars.tsx.
const SERIES_PALETTE = [
  "var(--brand)",
  "var(--info)",
  "var(--amber)",
  "var(--purple)",
  "var(--teal)",
  "var(--danger)",
  "var(--ok)",
] as const;

export function HBarList({
  data,
  emptyLabel,
  chartLabel,
  valueNoun,
  maxBars = 10,
  singleTone = false,
}: {
  data: HBarDatum[];
  /** Resolved from the page contract by the caller. */
  emptyLabel: string;
  /** Resolved from the page contract by the caller; the chart's accessible name. */
  chartLabel: string;
  /** Resolved from the page contract by the caller; used in per-bar tooltips. */
  valueNoun: string;
  maxBars?: number;
  /** One brand-toned series instead of the rotating palette (for ranked same-kind rows). */
  singleTone?: boolean;
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
    <div className="hbarlist" role="img" aria-label={chartLabel}>
      {bars.map((datum, index) => (
        <div className="hbrow" key={datum.key} title={`${datum.label}: ${datum.value} ${valueNoun}`}>
          <span className="hblab">{datum.label}</span>
          <span className="hbtrack">
            <span
              className="hbfill"
              style={{
                width: `${Math.max((datum.value / max) * 100, 1.5).toFixed(1)}%`,
                background: singleTone ? "var(--brand)" : SERIES_PALETTE[index % SERIES_PALETTE.length],
              }}
            />
          </span>
          <span className="hbval">{datum.display ?? datum.value.toLocaleString("en-IN")}</span>
        </div>
      ))}
    </div>
  );
}
