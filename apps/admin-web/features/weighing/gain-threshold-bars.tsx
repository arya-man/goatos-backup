// Grouped bars for the breed x daily-gain-band read on the Weights page.
//
// WHY GROUPED AND NOT STACKED. The four bands are DISJOINT (maintainer, 2026-08-24), so a
// stack would be arithmetically honest — and unreadable: every band here is a minority of
// its breed, and stacked segments cannot be compared against the SAME band of another
// breed, which is the one comparison this card exists for. One bar per band, all on a
// shared axis, keeps "how does Beetal's slowest band compare with Sojat's" a straight
// left-to-right read.
//
// The bar length is the breed's SHARE, not its head count, so a 12-kid breed and a
// 103-kid breed sit on one scale; the head count rides under the breed name and in every
// bar's hover title, so nobody reads "16.7%" off two kids without seeing the two.
//
// Server component — no client JS. Colours are theme tokens: `--gain-hi/mid/lo` is one hue
// in three ordered steps for the three growing bands, and `--gain-under` is red for the
// slowest band, which is a different KIND of fact rather than a fourth step on the ramp.
// Correct in both themes by construction. It renders no copy of its own: every string
// arrives already resolved from the page contract.
export type GainThresholdRow = {
  key: string;
  breed: string;
  /** Kids of this breed with a computable gain — the denominator of all four shares. */
  animals: number;
  marks: readonly GainThresholdMark[];
};

export type GainThresholdMark = {
  /** Ordered step: `hi` is the fastest band; `under` is the slowest and is red, not green. */
  step: "hi" | "mid" | "lo" | "under";
  /** The band's own column label from the table contract, e.g. "Above 250 g/day". */
  label: string;
  count: number;
  pct: number;
};

export function GainThresholdBars({
  rows,
  chartLabel,
  emptyLabel,
  kidsLabel,
  ofLabel,
}: {
  rows: readonly GainThresholdRow[];
  /** The list's accessible name, resolved from the page contract by the caller. */
  chartLabel: string;
  emptyLabel: string;
  /** Farm noun for the head count, e.g. "kids". */
  kidsLabel: string;
  /** Joining word for the hover title, e.g. "of". */
  ofLabel: string;
}) {
  if (rows.length === 0) {
    return (
      <div className="empty">
        <span className="muted small">{emptyLabel}</span>
      </div>
    );
  }

  // The scale is shared across breeds and rounded UP to the next 10% so the longest bar
  // never touches the edge. Taken from the widest band actually present rather than a
  // fixed 100%: every share here is a minority, and a fixed axis would squash all six
  // breeds into the first fifth of the track where nothing can be compared.
  const widest = Math.max(...rows.flatMap((row) => row.marks.map((mark) => mark.pct)), 1);
  const axis = Math.min(100, Math.ceil(widest / 10) * 10);

  return (
    <>
      {/* Four ordered bands, so identity never rests on colour alone. Labels are the
          table contract's own column labels, which keeps the chart and the table from
          drifting into two spellings of one band. */}
      <div className="gmlegend" aria-hidden="true">
        {rows[0].marks.map((mark) => (
          <span className={mark.step} key={mark.step}>
            <i /> {mark.label}
          </span>
        ))}
      </div>
      <ul className="gmarks" aria-label={chartLabel}>
        {rows.map((row) => (
          <li className="gmark" key={row.key}>
            <span className="gml">
              {row.breed}
              <span className="gml-sub">
                {row.animals.toLocaleString("en-IN")} {kidsLabel}
              </span>
            </span>
            <span className="gmrows">
              {row.marks.map((mark) => (
                <span className={`gmrow ${mark.step}`} key={mark.step}>
                  {/* Hovering a bar names the animals behind the share — "24 of 103 kids
                      · Above 250 g/day" — so a reader never has to switch to the table
                      just to see whether a percentage stands on two kids or a hundred. */}
                  <span
                    className="gmt"
                    title={`${mark.count.toLocaleString("en-IN")} ${ofLabel} ${row.animals.toLocaleString("en-IN")} ${kidsLabel} · ${mark.label}`}
                  >
                    <i style={{ width: `${Math.max((mark.pct / axis) * 100, 0.8)}%` }} />
                  </span>
                  {/* Share and count are SEPARATE cells, each right-aligned on tabular
                      figures, so the percentages line up down one column and the counts
                      down another. Side by side in one cell they ran together and read as
                      a single number. The count is parenthesised and recessive: the share
                      is what the bar encodes, the count is the evidence behind it. */}
                  <span className="gmv">{mark.pct.toFixed(1)}%</span>
                  <span className="gmn">({mark.count.toLocaleString("en-IN")})</span>
                </span>
              ))}
            </span>
          </li>
        ))}
      </ul>
    </>
  );
}
