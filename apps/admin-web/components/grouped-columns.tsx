// Grouped (multi-series) column chart for full-width report cards — the load-wise counts and
// money charts on the sales board.
//
// Sibling of `month-columns.tsx` and under the same rules: a pure SERVER component, no
// "use client", no charting library, no hex literals (series colors are THEME TOKEN tones mapped
// to classes in mesha-theme.css), and NO copy of its own — every visible string arrives already
// resolved from the backend page contract by the caller. Laid out in HTML so labels hold a fixed
// readable size; wide load lists scroll inside the chart's own overflow container, never the page.

export type GroupedSeriesTone = "brand" | "info" | "ok" | "warn" | "danger" | "teal";

export type GroupedSeries = {
  key: string;
  /** Resolved from the page contract by the caller; names the series in the legend and tooltips. */
  label: string;
  tone: GroupedSeriesTone;
};

export type GroupedDatum = {
  key: string;
  /** Short axis label, e.g. "12 Aug". */
  axisLabel: string;
  /** Full tooltip label, e.g. "Nutriplus · 12 Aug 2026". */
  label: string;
  /**
   * One entry per series, in series order. `null` means the figure is NOT RECORDED — the bar is
   * absent (not zero-height) and the tooltip shows the caller's display string for that state.
   */
  values: (number | null)[];
  /** One display string per series, shown in tooltips (e.g. "₹5.2L" or "Cost not recorded"). */
  displays: string[];
  /** Optional second line under the axis label (e.g. the vendor, or "97 of 100 sold"). */
  subLabel?: string;
};

export function GroupedColumns({
  series,
  data,
  chartLabel,
  emptyLabel,
}: {
  series: GroupedSeries[];
  data: GroupedDatum[];
  /** Resolved from the page contract by the caller; the chart's accessible name. */
  chartLabel: string;
  /** Resolved from the page contract by the caller. */
  emptyLabel: string;
}) {
  const hasAnyValue = data.some((d) => d.values.some((v) => v !== null && v !== 0));
  if (data.length === 0 || !hasAnyValue) {
    return (
      <div className="muted small" style={{ padding: "12px 2px", textAlign: "center" }}>
        {emptyLabel}
      </div>
    );
  }

  const max = Math.max(1, ...data.flatMap((d) => d.values.filter((v): v is number => v !== null)));

  return (
    <div>
      <div className="gleg" aria-hidden="true">
        {series.map((s) => (
          <span key={s.key}>
            <span className={`sw ${s.tone}`} />
            {s.label}
          </span>
        ))}
      </div>
      <div className="gcols" role="img" aria-label={chartLabel}>
        {data.map((datum) => {
          const tooltip = `${datum.label}: ${series
            .map((s, i) => `${s.label} ${datum.displays[i]}`)
            .join(" · ")}`;
          return (
            <div className="gcol" key={datum.key} title={tooltip}>
              <span className="gcarea">
                {series.map((s, i) => {
                  const value = datum.values[i];
                  if (value === null || value === 0) {
                    // Absent or zero: no bar. The tooltip still carries the display string, so
                    // "cost not recorded" and "0" stay distinguishable where it matters.
                    return <span key={s.key} className="gcbar none" />;
                  }
                  const pct = (value / max) * 100;
                  return (
                    <span
                      key={s.key}
                      className={`gcbar ${s.tone}`}
                      style={{ height: `${Math.max(pct, 2).toFixed(1)}%` }}
                    />
                  );
                })}
              </span>
              <span className="gclab">{datum.axisLabel}</span>
              {datum.subLabel ? <span className="gcsub">{datum.subLabel}</span> : null}
            </div>
          );
        })}
      </div>
    </div>
  );
}
