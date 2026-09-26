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
// Each bar is the template LinearProgress (kit ProgressBar). Colours are theme tokens: `--gain-hi/mid/lo` is one hue
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

export const GAIN_STEP_COLOR: Record<GainThresholdMark["step"], string> = {
  hi: "var(--gain-hi)",
  mid: "var(--gain-mid)",
  lo: "var(--gain-lo)",
  under: "var(--gain-under)",
};
