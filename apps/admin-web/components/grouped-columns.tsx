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
  /**
   * Optional short figure printed ABOVE each bar (maintainer request 2026-09-03: numbers on the
   * chart, not only on hover). One entry per series; `null` prints nothing. When absent, the bar
   * carries the leading part of its `displays` string, up to the first " · " -- the callers put
   * the figure first and the qualifier after it, so "27.9 kg · 126 weighed" prints "27.9 kg".
   */
  barLabels?: (string | null)[];
};

function barLabelFor(datum: GroupedDatum, index: number): string | null {
  const explicit = datum.barLabels?.[index];
  if (explicit !== undefined) return explicit;
  const display = datum.displays[index];
  if (!display) return null;
  return display.split(" · ")[0];
}

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
          // The hover card is rendered in the markup and revealed by CSS, never by a title
          // attribute: the native tooltip is unstyled, slow to appear, and cannot show a value
          // per series legibly. Keeping it CSS-only leaves this a pure server component.
          return (
            <div className="gcol" key={datum.key}>
              <span className="gtip" aria-hidden="true">
                <span className="gtip-h">{datum.label}</span>
                {series.map((s, i) => (
                  <span className="gtip-r" key={s.key}>
                    <span className={`sw ${s.tone}`} />
                    <span className="gtip-l">{s.label}</span>
                    <span className="gtip-v">{datum.displays[i]}</span>
                  </span>
                ))}
                {datum.subLabel ? <span className="gtip-s">{datum.subLabel}</span> : null}
              </span>
              <span className="gcarea">
                {series.map((s, i) => {
                  const value = datum.values[i];
                  if (value === null || value === 0) {
                    // Absent or zero: no bar and no figure. The tooltip still carries the display
                    // string, so "cost not recorded" and "0" stay distinguishable where it matters.
                    return (
                      <span key={s.key} className="gcb">
                        <span className="gcbar none" />
                      </span>
                    );
                  }
                  const pct = (value / max) * 100;
                  const label = barLabelFor(datum, i);
                  // The figure sits on the bar itself, always visible; the hover card keeps the
                  // fuller wording (unit qualifiers, head counts) for whoever wants it.
                  return (
                    <span key={s.key} className="gcb">
                      {label ? <span className="gcval">{label}</span> : null}
                      <span className={`gcbar ${s.tone}`} style={{ height: `${Math.max(pct, 2).toFixed(1)}%` }} />
                    </span>
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
