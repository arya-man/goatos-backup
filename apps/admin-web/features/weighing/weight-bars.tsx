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
export type WeightBar = { key: string; label: string; value: number };

export function WeightBars({
  data,
  emptyLabel,
  unit,
  chartLabel,
  size = "tall",
}: {
  data: readonly WeightBar[];
  /** Resolved from the page contract by the caller. */
  emptyLabel: string;
  unit: string;
  /** Resolved from the page contract by the caller; the list's accessible name. */
  chartLabel: string;
  /** "tall" for the shed/breed row, "short" for the sex/stage row. */
  size?: "tall" | "short";
}) {
  // A bar can only be drawn with a positive length. Non-positive values are real
  // data, so they are not silently dropped — the caller's empty copy has to explain
  // them, which is why this returns the empty state rather than an empty box.
  const bars = data.filter((bar) => bar.value > 0);
  if (bars.length === 0) {
    return (
      <div className="muted small" style={{ padding: "18px 2px", textAlign: "center" }}>
        {emptyLabel}
      </div>
    );
  }
  const max = Math.max(...bars.map((bar) => bar.value));

  return (
    <ul className={`wbars ${size === "short" ? "wbars-short" : "wbars-tall"}`} aria-label={chartLabel}>
      {bars.map((bar) => (
        <li className="wbar" key={bar.key}>
          <span className="wbl" title={bar.label}>
            {bar.label}
          </span>
          <span className="wbt">
            <i style={{ width: `${Math.max((bar.value / max) * 100, 1.5)}%` }} />
          </span>
          <span className="wbv">
            {bar.value.toLocaleString("en-IN", { maximumFractionDigits: 1 })} {unit}
          </span>
        </li>
      ))}
    </ul>
  );
}
