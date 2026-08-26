// Breed spend-vs-return bars for the Economics page.
//
// The question this answers is not "how big is each breed's feed bill" but "how
// much of what a breed returns does its feed eat" — so the two series are drawn
// as ONE bar per breed, not two side by side: the track is the day's growth
// value and the fill is the day's feed cost. A fill that nearly covers its track
// is a breed barely paying for itself; a fill that overruns it is a breed losing
// money, and is toned as such.
//
// Why not the shared `SvgColumnBars`: it draws unlabelled columns (right for a
// day series, where position is the label) and puts float values in an SVG
// `<title>`, which hydration-mismatches. Six breeds need their NAMES on screen,
// so this is laid out in HTML like `hbar-list.tsx` — labels hold a readable size
// at any card width.
//
// Same rules as the shared chart primitives: a pure SERVER component, no
// "use client", no charting library, no hex literals (palette is CSS custom
// properties), and NO copy of its own — every visible string arrives already
// resolved from the backend page contract by the caller.

export type BreedBarDatum = {
  key: string;
  /** Breed name, rendered verbatim. */
  label: string;
  /** Feed cost per head per day — the fill. */
  feed: number;
  /** Growth value per head per day — the track. Null when it cannot be computed. */
  value: number | null;
  /** Pre-formatted "₹39 · ₹64" style figures, resolved by the caller. */
  feedDisplay: string;
  valueDisplay: string;
  /** True when feed exceeds return, so the row reads as losing money. */
  losing: boolean;
};

export function BreedEconomicsBars({
  data,
  chartLabel,
  feedNoun,
  valueNoun,
  emptyLabel,
}: {
  data: BreedBarDatum[];
  /** Resolved from the page contract by the caller; the chart's accessible name. */
  chartLabel: string;
  /** Resolved from the page contract by the caller; names the fill series. */
  feedNoun: string;
  /** Resolved from the page contract by the caller; names the track series. */
  valueNoun: string;
  /** Resolved from the page contract by the caller. */
  emptyLabel: string;
}) {
  if (data.length === 0) {
    return (
      <div className="muted small" style={{ padding: "12px 2px", textAlign: "center" }}>
        {emptyLabel}
      </div>
    );
  }

  // ONE scale across both series and every breed. Scaling each row to its own
  // max would draw a breed returning ₹64 and one returning ₹28 as equal bars —
  // the exact comparison this chart exists to make, silently flattened.
  const max = Math.max(1, ...data.map((d) => Math.max(d.feed, d.value ?? 0)));

  return (
    <div role="img" aria-label={chartLabel} style={{ display: "grid", gap: 8 }}>
      {data.map((datum) => {
        const trackPct = Math.max(((datum.value ?? 0) / max) * 100, 0);
        const fillPct = Math.max((datum.feed / max) * 100, 1.5);
        return (
          <div
            key={datum.key}
            style={{ display: "grid", gridTemplateColumns: "minmax(96px, 132px) 1fr auto", gap: 10, alignItems: "center" }}
            title={`${datum.label}: ${datum.feedDisplay} ${feedNoun} · ${datum.valueDisplay} ${valueNoun}`}
          >
            <span className="small" style={{ fontWeight: 600 }}>
              {datum.label}
            </span>
            {/* The rail is the full scale; the track is this breed's return; the
                fill is its feed cost sitting inside that return. */}
            <span
              style={{
                position: "relative",
                display: "block",
                height: 18,
                borderRadius: 9,
                background: "var(--line)",
              }}
            >
              {/* The return, as a filled band. Kept clearly visible rather than a
                  faint wash: it is the thing the cost is being judged against. */}
              <span
                style={{
                  position: "absolute",
                  inset: 0,
                  width: `${trackPct.toFixed(1)}%`,
                  background: "var(--ok)",
                  opacity: 0.42,
                  borderRadius: 9,
                }}
              />
              {/* The cost, sitting inside that return. */}
              <span
                style={{
                  position: "absolute",
                  top: 3,
                  bottom: 3,
                  left: 0,
                  width: `${fillPct.toFixed(1)}%`,
                  background: datum.losing ? "var(--danger)" : "var(--brand)",
                  borderRadius: 6,
                }}
              />
              {/* The break-even MARKER at the return position. Two bands of
                  similar length are indistinguishable at a glance — exactly the
                  case that matters most (a breed whose feed nearly equals its
                  return) — so the line makes "did the cost pass the return"
                  readable without measuring. */}
              {datum.value !== null ? (
                <span
                  style={{
                    position: "absolute",
                    top: -2,
                    bottom: -2,
                    left: `${trackPct.toFixed(1)}%`,
                    width: 2,
                    marginLeft: -1,
                    background: "var(--ok)",
                    borderRadius: 1,
                  }}
                />
              ) : null}
            </span>
            <span className="small muted" style={{ whiteSpace: "nowrap" }}>
              {datum.feedDisplay} / {datum.valueDisplay}
            </span>
          </div>
        );
      })}
    </div>
  );
}
