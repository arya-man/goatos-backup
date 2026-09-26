// Axis-scale helpers shared by the hand-rolled charts (svg-series, svg-column-bars,
// grouped-columns). Pure functions, no copy, no colour.

/**
 * A "nice" axis ceiling: the smallest 1 / 1.2 / 1.6 / 2 / 2.4 / 3 / 4 / 5 / 6 / 8 x 10^n multiple at or above the data
 * maximum, chosen so that the QUARTER gridlines the charts draw land on round figures
 * (a 2,369.8 tick is noise; 2,500 is a scale a reader can carry across cards).
 */
export function niceCeiling(max: number): number {
  if (!(max > 0) || !Number.isFinite(max)) return 1;
  const exp = Math.floor(Math.log10(max));
  const base = Math.pow(10, exp);
  const m = max / base;
  // Every candidate divides by four into a round-ish figure (.25 .3 .4 .5 .6 .75 1 1.25 1.5 2 2.5),
  // and the ladder is fine enough that the tallest mark always reaches at least ~80% of the plot.
  const step = [1, 1.2, 1.6, 2, 2.4, 3, 4, 5, 6, 8, 10].find((c) => m <= c + 1e-9) ?? 10;
  return step * base;
}

/** The quarter ticks of a nice ceiling, dropping fractional ticks on a small integer scale. */
export function quarterTicks(max: number, integer: boolean): number[] {
  return [0.25, 0.5, 0.75, 1].map((f) => max * f).filter((v) => !integer || Number.isInteger(v));
}

/**
 * niceCeiling for a chart whose values are all whole numbers. The quarter gridlines drop their
 * fractional labels on an integer scale, so a ceiling of 1, 3 or 5 would leave a single labelled
 * tick (just "3" at the top). Lift those to 2 / 4 / 8 so at least two gridlines carry a figure.
 */
export function axisCeiling(max: number, integer: boolean): number {
  const nice = niceCeiling(max);
  if (!integer || quarterTicks(nice, true).length >= 2) return nice;
  return nice < 2 ? 2 : Math.ceil(nice / 4) * 4;
}
