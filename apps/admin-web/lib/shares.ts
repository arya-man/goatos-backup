// Shares of one whole, rounded to a tenth so they ADD UP TO 100.0.
//
// For a set of parts that PARTITION a whole -- the Weights gain bands, where every kid
// lands in exactly one band -- the percentages are a column the reader will add.
// Rounding each one independently prints 0.0 + 12.8 + 4.3 + 83.0 = 100.1% and makes a
// correct partition look like a broken one.
//
// Largest remainder (Hamilton) apportions the 1000 tenths instead: floor every share,
// then hand the leftover tenths to the parts with the largest discarded fractions. Every
// value stays within a tenth of its true share and the column totals exactly 100.0.
//
// Ties go to the LARGER count, then to the earlier part, so the same input always prints
// the same output instead of following array order by luck.
//
// Only for parts that really do partition the whole. Overlapping or partial sets must not
// use this: forcing them to 100 would state a distribution nobody measured.
export function sharesOfWhole(counts: readonly number[], whole: number): number[] {
  if (whole <= 0) return counts.map(() => 0);
  const exact = counts.map((count) => (count / whole) * 1000);
  const tenths = exact.map((value) => Math.floor(value));
  let leftover = 1000 - tenths.reduce((sum, value) => sum + value, 0);
  const order = exact
    .map((value, index) => ({
      index,
      remainder: value - Math.floor(value),
      count: counts[index],
    }))
    .sort((a, b) => b.remainder - a.remainder || b.count - a.count || a.index - b.index);
  for (let i = 0; leftover > 0 && i < order.length; i += 1, leftover -= 1) {
    tenths[order[i].index] += 1;
  }
  return tenths.map((value) => value / 10);
}
