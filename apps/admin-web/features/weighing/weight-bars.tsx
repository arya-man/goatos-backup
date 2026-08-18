// A readable horizontal bar list for the Weights page.
//
// SvgBars is not usable here. It renders a fixed-width viewBox and an SVG scales
// its viewBox UNIFORMLY to its container, so inside a half-width card the whole
// drawing — text included — shrinks to roughly a third and the labels become
// unreadable. That is fine on a full-width card and wrong on this page.
//
// This is plain HTML instead: the labels are real text at a real font size, so they
// stay legible at any card width, and the row list scrolls inside a FIXED height so
// a chart with 40 breeds occupies exactly as much page as one with 4.
//
// Server component — no client JS. Colours are theme tokens only, so it stays
// correct in both themes and passes the banned-hex scan by construction. It renders
// no copy of its own: every string is passed in already resolved from the page
// contract by the caller.
export type WeightBar = { key: string; label: string; value: number; valueLabel?: string };

export function WeightBars({
  data,
  emptyLabel,
  unit,
  chartLabel,
  size = "tall",
  wide = false,
  domain,
}: {
  data: readonly WeightBar[];
  /** Resolved from the page contract by the caller. */
  emptyLabel: string;
  unit: string;
  /** Resolved from the page contract by the caller; the list's accessible name. */
  chartLabel: string;
  /** "tall" for the shed/breed row, "short" for the sex/stage row. */
  size?: "tall" | "short";
  /**
   * Widen the label column. For a full-width card whose labels carry two facts —
   * the load number AND its supplier — the half-width column clips the supplier off,
   * which is the one thing that label exists to show.
   */
  wide?: boolean;
  /**
   * Shared scale for lists rendered side by side. Two columns of the same chart must
   * draw the same value at the same length, or the eye reads the shorter column's
   * best pen as slower than the other column's mid-pack. Unioned with this list's own
   * values so a value outside the caller's span can never overflow the track.
   */
  domain?: { lo: number; hi: number };
}) {
  // A bar can only be drawn with a positive length. Non-positive values are real
  // data, so they are not silently dropped — the caller's empty copy has to explain
  // them, which is why this returns the empty state rather than an empty box.
  // EVERY value renders, gain or loss. Filtering to value > 0 silently dropped every
  // losing shed and every losing kid — which is precisely the row a reader is looking
  // for. Only a genuinely empty series shows the empty state.
  const bars = data.filter((bar) => Number.isFinite(bar.value));
  if (bars.length === 0) {
    // Same fixed box as the populated list, so toggling a chart between weight and
    // gain never makes the row jump.
    return (
      <div className={`wbars-empty ${size === "short" ? "wbars-short" : "wbars-tall"}`} role="note">
        <span className="muted small">{emptyLabel}</span>
      </div>
    );
  }

  // Bars are drawn from a ZERO baseline, not from the smallest value: a loss has to
  // read as crossing zero, not as a short positive bar. The axis spans min..max with
  // zero always inside it, so the baseline sits where zero actually falls — hard left
  // when everything is positive, mid-track when the series straddles zero.
  const lo = Math.min(0, domain?.lo ?? 0, ...bars.map((bar) => bar.value));
  const hi = Math.max(0, domain?.hi ?? 0, ...bars.map((bar) => bar.value));
  const span = hi - lo || 1;
  const zeroPct = ((0 - lo) / span) * 100;
  const geometry = (value: number) => {
    const width = (Math.abs(value) / span) * 100;
    return {
      left: value >= 0 ? zeroPct : zeroPct - width,
      width: Math.max(width, 0.6),
      negative: value < 0,
    };
  };

  return (
    <ul
      className={`wbars ${size === "short" ? "wbars-short" : "wbars-tall"}${wide ? " wbars-wide" : ""}`}
      aria-label={chartLabel}
    >
      {bars.map((bar) => (
        <li className="wbar" key={bar.key}>
          <span className="wbl" title={bar.label}>
            {bar.label}
          </span>
          <span className="wbt">
            {/* The zero rule only appears when the series actually straddles zero;
                on an all-positive chart it would sit on the axis and read as noise. */}
            {lo < 0 && hi > 0 ? <b className="wbzero" style={{ left: `${zeroPct}%` }} /> : null}
            <i
              className={geometry(bar.value).negative ? "neg" : undefined}
              style={{
                marginLeft: `${geometry(bar.value).left}%`,
                width: `${geometry(bar.value).width}%`,
              }}
            />
          </span>
          <span className={`wbv${bar.value < 0 ? " neg" : ""}`}>
            {bar.valueLabel ?? `${bar.value.toLocaleString("en-IN", { maximumFractionDigits: 1 })} ${unit}`}
          </span>
        </li>
      ))}
    </ul>
  );
}
