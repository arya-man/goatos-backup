// Labeled month-by-month column chart for full-width report cards (sales board).
//
// The sibling `svg-column-bars.tsx` is the mock's compact tooltip-only column strip; that shape
// answers "is there a pulse" but a monthly business chart must be readable without hovering:
// every column carries its month on the axis and its value on top. Laid out in HTML so the
// labels hold a fixed readable size at every card width.
//
// Same rules as the SVG siblings: a pure SERVER component, no "use client", no charting library,
// no hex literals, and NO copy of its own — every visible string arrives already resolved from
// the backend page contract by the caller.

export type MonthColumnDatum = {
  key: string;
  /** Short axis label, e.g. "Apr 25". */
  axisLabel: string;
  /** Full tooltip label, e.g. "Apr 2025". */
  label: string;
  value: number;
  /** Compact value label drawn above the column (e.g. "₹19.4L"); blank hides it. */
  display: string;
};

export function MonthColumns({
  data,
  chartLabel,
  valueNoun,
  emptyLabel,
}: {
  data: MonthColumnDatum[];
  /** Resolved from the page contract by the caller; the chart's accessible name. */
  chartLabel: string;
  /** Resolved from the page contract by the caller; used in per-column tooltips. */
  valueNoun: string;
  /** Resolved from the page contract by the caller. */
  emptyLabel: string;
}) {
  if (data.length === 0 || data.every((d) => d.value === 0)) {
    return (
      <div className="muted small" style={{ padding: "12px 2px", textAlign: "center" }}>
        {emptyLabel}
      </div>
    );
  }

  const max = Math.max(1, ...data.map((d) => d.value));

  return (
    <div className="mcols" role="img" aria-label={chartLabel}>
      {data.map((datum) => {
        const pct = (datum.value / max) * 100;
        return (
          <div className="mcol" key={datum.key} title={`${datum.label}: ${datum.value.toLocaleString("en-IN")} ${valueNoun}`}>
            <span className="mcarea">
              {datum.value > 0 ? (
                <span className="mcstack">
                  <span className="mcv">{datum.display}</span>
                  <span className="mcbar" style={{ height: `${Math.max(pct, 2).toFixed(1)}%` }} />
                </span>
              ) : null}
            </span>
            <span className="mclab">{datum.axisLabel}</span>
          </div>
        );
      })}
    </div>
  );
}
