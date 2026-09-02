// Grouped horizontal bars: several named groups, each holding one or more comparable bars.
//
// The Weights analytics page asks the same shape three times and it is NOT what `WeightBars`
// draws. That one is a flat list on a single scale — right for "every shed, ranked". These
// three tabs are comparisons WITHIN a group and the group is the unit of reading:
//
//   Breed-wise  one group per breed, two bars: daily gain and average weight
//   Birth-wise  one group per breed, up to two bars: farm born and purchased
//   Shed-wise   one group per breed, one bar per shed holding that breed
//
// Flattening those into one list ("Sheep · Farm born", "Sheep · Purchased", "Goat · Farm born")
// technically shows the same numbers and answers none of the questions: the eye has to
// re-collect the pairs itself, and with ~40 sheds under six breeds it cannot.
//
// TWO SCALES, DELIBERATELY. Breed-wise puts g/day beside kg, which are not comparable lengths,
// so every bar carries the unit of its OWN series and is drawn against that series' own domain.
// Sharing one axis between them would draw a 210 g/day bar and a 28 kg bar at lengths whose
// comparison means nothing. Within a series the domain IS shared across every group, so a
// shed in one breed is directly comparable to a shed in another — which is the whole point of
// the shed tab.
//
// Server component: no client JS, theme tokens only, and every string arrives already resolved
// from the page contract. It renders no copy of its own.
import { Tag, type Tone } from "@/components/ui-primitives";

/** One measured bar inside a group. `seriesKey` selects which scale and unit it is drawn on. */
export type GroupedBar = {
  key: string;
  label: string;
  value: number;
  seriesKey: string;
  /** Optional right-hand chip: head count, capture mode, park. Already resolved copy. */
  noteLabel?: string;
  noteTone?: Tone;
  /**
   * Optional "what is in THIS bar" note, shown behind a small `i` beside the bar's own label.
   *
   * It exists for a bar whose MEMBERSHIP is decided by a rule the chart cannot show -- the
   * shed-type split classifies pens from shed metadata and pen names, so a bar appears with no way
   * to tell which sheds it counted. It sits on the BAR and not on the legend (maintainer,
   * 2026-09-02): a pen holds one breed, so a per-series list beside one breed's bar would name
   * mostly other breeds' pens.
   *
   * Every string here is already resolved from the page contract; this component composes no copy.
   * Hover AND focus open it, and it is a plain server-rendered panel: no client JS, so the card
   * stays a server component.
   */
  hint?: {
    /** Accessible name for the `i` control, e.g. "Which sheds count as this". */
    ariaLabel: string;
    /** Heading inside the panel. */
    title: string;
    /**
     * The members, in the backend's order, optionally split into headed sections.
     *
     * SECTIONS EXIST FOR THE PARK. A shed name is not unique across parks -- this farm has a
     * "Castro 1" in each -- so a flat list of names would show one pen twice with nothing to tell
     * them apart. A single-section list renders with no heading, which is what a park-filtered
     * page wants.
     */
    sections: readonly { heading?: string; items: readonly string[] }[];
    /** Shown INSTEAD of an empty list -- an absent list must say so, never render blank. */
    emptyLabel: string;
  };
};

export type GroupedBarSeries = {
  key: string;
  /** Already resolved from the page contract; shown in the legend. */
  label: string;
  unit: string;
  /** Decimals for the printed figure. Gain is whole grams; a weight reads to a tenth. */
  fractionDigits?: number;
  /**
   * Which SCALE this series is drawn against; defaults to its own key.
   *
   * Two series measuring the SAME thing over different cohorts -- farm born against purchased,
   * both in g/day -- must share one domain, or the longest bar in each half is drawn at full
   * width and the two cohorts look identical however far apart they really are. That is the
   * entire comparison, so it cannot be left to the default.
   */
  scaleKey?: string;
};

export type BarGroup = {
  key: string;
  /** Already resolved; the breed, or whatever the groups are cut by. */
  heading: string;
  /** Optional sub-line under the heading — a head count, a caveat. Already resolved. */
  subheading?: string;
  bars: readonly GroupedBar[];
};

export function GroupedBars({
  groups,
  series,
  emptyLabel,
  chartLabel,
}: {
  groups: readonly BarGroup[];
  /** In legend order. A series with no bar in any group is still listed, so a missing half reads as absent rather than unmentioned. */
  series: readonly GroupedBarSeries[];
  emptyLabel: string;
  chartLabel: string;
}) {
  const drawable = groups.filter((group) => group.bars.some((bar) => Number.isFinite(bar.value)));
  if (drawable.length === 0) {
    return (
      <div className="wbars-empty wbars-bands" role="note">
        <span className="muted small">{emptyLabel}</span>
      </div>
    );
  }

  // ONE DOMAIN PER SERIES, computed across every group before drawing, so a bar's length means
  // the same thing in every group. Zero is always inside the span: a loss has to read as
  // crossing the baseline, not as a short positive bar.
  const seriesByKey = new Map(series.map((s) => [s.key, s]));
  const scaleOf = (seriesKey: string) => seriesByKey.get(seriesKey)?.scaleKey ?? seriesKey;
  const domains = new Map(
    [...new Set(series.map((s) => s.scaleKey ?? s.key))].map((scale) => {
      const values = drawable
        .flatMap((group) => group.bars)
        .filter((bar) => scaleOf(bar.seriesKey) === scale && Number.isFinite(bar.value))
        .map((bar) => bar.value);
      const lo = Math.min(0, ...values);
      const hi = Math.max(0, ...values);
      return [scale, { lo, hi, span: hi - lo || 1 }];
    }),
  );

  return (
    <div className="wgrouped" role="group" aria-label={chartLabel}>
      <div className="wgrouped-legend">
        {series.map((s, index) => (
          <span key={s.key}>
            <i className={`wgl s${index}`} aria-hidden /> {s.label}
          </span>
        ))}
      </div>
      {drawable.map((group) => (
        <section className="wgrouped-group" key={group.key} aria-label={group.heading}>
          <h3 className="wgrouped-heading">
            {group.heading}
            {group.subheading ? <span className="muted small"> · {group.subheading}</span> : null}
          </h3>
          <ul className="wbars wbars-bands">
            {group.bars
              .filter((bar) => Number.isFinite(bar.value))
              .map((bar) => {
                const s = seriesByKey.get(bar.seriesKey);
                const domain = domains.get(scaleOf(bar.seriesKey));
                if (!s || !domain) return null;
                const seriesIndex = series.findIndex((entry) => entry.key === bar.seriesKey);
                const zeroPct = ((0 - domain.lo) / domain.span) * 100;
                const width = (Math.abs(bar.value) / domain.span) * 100;
                const negative = bar.value < 0;
                return (
                  <li className="wbar" key={bar.key}>
                    <span className="wbl" title={bar.label}>
                      <span className="wbl-text">{bar.label}</span>
                      {bar.hint ? (
                        <span className="wgl-hint">
                          {/* Focusable so the panel is reachable without a pointer; `note` because
                              it describes the bar rather than doing anything. */}
                          <span className="wgl-i" tabIndex={0} role="note" aria-label={bar.hint.ariaLabel}>
                            i
                          </span>
                          <span className="wgl-pop">
                            <b>{bar.hint.title}</b>
                            {bar.hint.sections.some((section) => section.items.length > 0) ? (
                              bar.hint.sections.map((section) => (
                                <span className="wgl-pop-list" key={section.heading ?? "all"}>
                                  {section.heading ? <i className="wgl-pop-park">{section.heading}</i> : null}
                                  {section.items.map((item) => (
                                    <span key={item}>{item}</span>
                                  ))}
                                </span>
                              ))
                            ) : (
                              <span className="muted small">{bar.hint.emptyLabel}</span>
                            )}
                          </span>
                        </span>
                      ) : null}
                      {bar.noteLabel ? (
                        <span className="wbar-mode">
                          <Tag tone={bar.noteTone ?? "mut"}>{bar.noteLabel}</Tag>
                        </span>
                      ) : null}
                    </span>
                    <span className="wbt">
                      {domain.lo < 0 && domain.hi > 0 ? (
                        <b className="wbzero" style={{ left: `${zeroPct}%` }} />
                      ) : null}
                      <i
                        className={`s${seriesIndex}${negative ? " neg" : ""}`}
                        style={{
                          marginLeft: `${negative ? zeroPct - width : zeroPct}%`,
                          width: `${Math.max(width, 0.6)}%`,
                        }}
                      />
                    </span>
                    <span className={`wbv${negative ? " neg" : ""}`}>
                      {bar.value.toLocaleString("en-IN", {
                        maximumFractionDigits: s.fractionDigits ?? 0,
                        minimumFractionDigits: s.fractionDigits ?? 0,
                      })}{" "}
                      {s.unit}
                    </span>
                  </li>
                );
              })}
          </ul>
        </section>
      ))}
    </div>
  );
}
