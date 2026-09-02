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
  /**
   * Optional second compact figure for the same month in a DIFFERENT unit (e.g. the rupees behind
   * a count or a kg column). Drawn under the axis label, never on the bar's axis — the bar height
   * stays owned by `value` alone so two units can never be read as one series. Blank hides it.
   */
  subDisplay?: string;
};

export function MonthColumns({
  data,
  chartLabel,
  valueNoun,
  subValueNoun,
  emptyLabel,
}: {
  data: MonthColumnDatum[];
  /** Resolved from the page contract by the caller; the chart's accessible name. */
  chartLabel: string;
  /** Resolved from the page contract by the caller; used in per-column tooltips. */
  valueNoun: string;
  /** Resolved from the page contract by the caller; names the `subDisplay` figure in tooltips. */
  subValueNoun?: string;
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
        const tooltip = `${datum.label}: ${datum.value.toLocaleString("en-IN")} ${valueNoun}${
          datum.subDisplay && subValueNoun ? ` · ${datum.subDisplay} ${subValueNoun}` : ""
        }`;
        const [axisMonth, axisYear] = datum.axisLabel.split(" ");
        return (
          <div className="mcol" key={datum.key} title={tooltip}>
            <span className="mcarea">
              {datum.value > 0 ? (
                <span className="mcstack">
                  <span className="mcv">{datum.display}</span>
                  <span className="mcbar" style={{ height: `${Math.max(pct, 2).toFixed(1)}%` }} />
                </span>
              ) : null}
            </span>
            <span className="mclab">
              <span>{axisMonth}</span>
              {axisYear ? <span>{axisYear}</span> : null}
            </span>
            {datum.subDisplay ? <span className="mcsub">{datum.subDisplay}</span> : null}
          </div>
        );
      })}
    </div>
  );
}
