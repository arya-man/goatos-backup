// Grouped (multi-series) column chart for full-width report cards — the load-wise counts and
// money charts on the sales board.
//
// Sibling of `month-columns.tsx` and under the same rules: a pure SERVER component, no
// "use client", no charting library, no hex literals (series colors are THEME TOKEN tones mapped
// to classes in mesha-theme.css), and NO copy of its own — every visible string arrives already
// resolved from the backend page contract by the caller. Laid out in HTML so labels hold a fixed
// readable size; wide load lists scroll inside the chart's own overflow container, never the page.

// `okHatch` is the sold green, striped: a figure that is sold-LIKE but not realised -- animals
// tagged to a sale that has not closed, or stock carried at an assumed price. The palette has four
// chart colours and two of them (warn, teal) already share one, so a fifth solid colour would
// read as one of the others; the stripe is what keeps it distinct at a glance.
export type GroupedSeriesTone = "brand" | "info" | "ok" | "warn" | "danger" | "teal" | "okHatch";

export type GroupedSeries = {
  key: string;
  /** Resolved from the page contract by the caller; names the series in the legend and tooltips. */
  label: string;
  tone: GroupedSeriesTone;
  /**
   * The key of an EARLIER series this one stacks on top of, in the same slot (e.g. assumed value
   * on top of sold value). A stacked series draws no slot of its own; the pair is one column whose
   * height is their sum, and the scale is computed over those sums.
   */
  stackOn?: string;
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
  /** Extra lines shown ONLY in the hover card (e.g. the day the load reached the farm). */
  tipLines?: string[];
};

function toneClass(tone: GroupedSeriesTone): string {
  return tone === "okHatch" ? "ok-hatch" : tone;
}

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

  // Series stacked on another draw inside that series' slot; everything else gets its own slot.
  const stackedOnto = new Map<number, number[]>();
  series.forEach((s, i) => {
    if (!s.stackOn) return;
    const base = series.findIndex((b) => b.key === s.stackOn);
    if (base < 0 || base >= i) return;
    stackedOnto.set(base, [...(stackedOnto.get(base) ?? []), i]);
  });
  const isStacked = (i: number) => {
    const s = series[i];
    if (!s.stackOn) return false;
    const base = series.findIndex((b) => b.key === s.stackOn);
    return base >= 0 && base < i;
  };
  const positive = (v: number | null | undefined) => (v != null && v > 0 ? v : 0);
  const slotTotal = (d: GroupedDatum, i: number) =>
    positive(d.values[i]) + (stackedOnto.get(i) ?? []).reduce((sum, j) => sum + positive(d.values[j]), 0);
  const max = Math.max(
    1,
    ...data.flatMap((d) =>
      series.map((_, i) => (isStacked(i) ? 0 : stackedOnto.has(i) ? slotTotal(d, i) : d.values[i] ?? 0)),
    ),
  );

  return (
    <div>
      <div className="gleg" aria-hidden="true">
        {series.map((s) => (
          <span key={s.key}>
            <span className={`sw ${toneClass(s.tone)}`} />
            {s.label}
          </span>
        ))}
      </div>
      <div className="gcols" role="img" aria-label={chartLabel} tabIndex={0}>
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
                    <span className={`sw ${toneClass(s.tone)}`} />
                    <span className="gtip-l">{s.label}</span>
                    <span className="gtip-v">{datum.displays[i]}</span>
                  </span>
                ))}
                {datum.tipLines?.map((line) => (
                  <span className="gtip-s gtip-wrap" key={line}>
                    {line}
                  </span>
                ))}
                {datum.subLabel ? <span className="gtip-s">{datum.subLabel}</span> : null}
              </span>
              <span className="gcarea">
                {series.map((s, i) => {
                  if (isStacked(i)) return null;
                  const stack = stackedOnto.get(i);
                  if (stack && slotTotal(datum, i) > 0) {
                    // One column, segments bottom-up in series order, each sized by its share of
                    // the column. The figure above is the base series' bar label (the caller
                    // states the total there when it wants one).
                    const total = slotTotal(datum, i);
                    const pct = (total / max) * 100;
                    const label = barLabelFor(datum, i);
                    const parts = [i, ...stack].filter((j) => positive(datum.values[j]) > 0);
                    return (
                      <span key={s.key} className="gcb">
                        {label ? <span className="gcval">{label}</span> : null}
                        <span className="gcstack" style={{ height: `${Math.max(pct, 2).toFixed(1)}%` }}>
                          {[...parts].reverse().map((j) => (
                            <span
                              key={series[j].key}
                              className={`gcbar ${toneClass(series[j].tone)}`}
                              style={{ flex: `${positive(datum.values[j])} 1 0` }}
                            />
                          ))}
                        </span>
                      </span>
                    );
                  }
                  const value = datum.values[i];
                  if (value === null || value === 0) {
                    // No bar either way -- there is no height to draw. A MEASURED zero still prints
                    // its figure on the baseline, because a slot with nothing above it reads as a
                    // number that failed to load rather than as "none". An ABSENT value keeps the
                    // hidden placeholder, so the slot still holds its place while "cost not
                    // recorded" and "0" stay distinguishable -- the tooltip carries the wording.
                    const zeroLabel = value === 0 ? barLabelFor(datum, i) : null;
                    if (!zeroLabel) {
                      return (
                        <span key={s.key} className="gcb gcempty" aria-hidden="true">
                          <span className="gcbar none" />
                        </span>
                      );
                    }
                    return (
                      <span key={s.key} className="gcb">
                        <span className="gcval">{zeroLabel}</span>
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
                      <span className={`gcbar ${toneClass(s.tone)}`} style={{ height: `${Math.max(pct, 2).toFixed(1)}%` }} />
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
