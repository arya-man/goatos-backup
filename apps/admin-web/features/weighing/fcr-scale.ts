// The FCR-by-pen chart's value-axis ceiling. One pen that barely gained (a ratio of 468 against a
// farm of 6-10) set the axis to 500 and drew every other pen as a sliver; the reader could compare
// nothing. The axis is therefore capped at the bulk of the pens -- the larger of 1.5x the 90th
// percentile and 1.5x break-even -- and a pen past the cap runs to the edge with its true ratio in
// the tooltip ("off scale"). With no outlier the axis is left to the chart (undefined).

/** A "nice" ceiling at or above `value`: 1, 2, 2.5 or 5 times a power of ten. */
function niceCeil(value: number): number {
  if (!(value > 0)) return value;
  const power = 10 ** Math.floor(Math.log10(value));
  for (const step of [1, 2, 2.5, 5, 10]) {
    if (step * power >= value) return step * power;
  }
  return 10 * power;
}

/** The axis max to clamp to, or undefined when no pen stands far enough out to need it. */
export function fcrAxisCeiling(values: readonly number[], breakEven: number | null | undefined): number | undefined {
  const finite = values.filter((value) => Number.isFinite(value) && value > 0).sort((a, b) => a - b);
  if (finite.length < 3) return undefined;
  const p90 = finite[Math.min(finite.length - 1, Math.floor(0.9 * (finite.length - 1)))];
  const floor = Math.max(p90 * 1.5, breakEven != null && breakEven > 0 ? breakEven * 1.5 : 0);
  const max = finite[finite.length - 1];
  if (max <= floor) return undefined;
  return niceCeil(floor);
}

/** True when a pen's ratio is drawn cut at the ceiling. */
export function isOffScale(value: number, ceiling: number | undefined): boolean {
  return ceiling !== undefined && value > ceiling;
}
