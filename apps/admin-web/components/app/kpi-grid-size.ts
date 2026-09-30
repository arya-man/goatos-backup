// KPI row tile sizes (pure; components/app/kpi-grid.tsx renders them, kpi-grid-size.test.mjs pins them).

export type GridSize = { xs: number; sm?: number; md?: number; lg?: number; xl?: number };

/** Card count drives a balanced row (no orphan card, no dead slot); phones stack one widget per row. */
export function sizeFor(n: number): GridSize {
  if (n <= 1) return { xs: 12, sm: 6, md: 4 };
  if (n === 2) return { xs: 12, sm: 6 };
  if (n === 3) return { xs: 12, sm: 4 };
  if (n === 4) return { xs: 12, sm: 6, lg: 3 };
  if (n === 5) return { xs: 12, sm: 6, md: 4, xl: 12 / 5 };
  // Eight (or any multiple of four) tiles: two full rows of four, never 3 + 3 + 2 (TR1-#30).
  if (n % 4 === 0) return { xs: 12, sm: 6, md: 3 };
  return { xs: 12, sm: 6, md: 4, xl: 2 };
}

/**
 * The size of tile `i` of `n`: the row size, except that a SHORT LAST ROW fills its width (the rule
 * the /feed/analytics 3 + 2 deck follows; r2 `kpi-row|empty-slot`). A data-sized deck of 7
 * (/counts/breakdown: two totals + the non-empty stage tiles) was 3 + 3 + 1 at md with two dead
 * slots (FIXJ11). Only the column counts a breakpoint really has are balanced: sm 2, md 3, xl 6.
 */
export function kpiTileSize(n: number, i: number): GridSize {
  const size = sizeFor(n);
  if (n <= 5 || n % 4 === 0) return size;
  const fill = (cols: number) => {
    const rest = n % cols;
    return rest && i >= n - rest ? 12 / rest : 12 / cols;
  };
  return { xs: 12, sm: fill(2), md: fill(3), xl: fill(6) };
}

