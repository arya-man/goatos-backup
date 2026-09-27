import { Children, type ReactNode } from "react";
import Grid from "@mui/material/Grid";

type GridSize = { xs: number; sm?: number; md?: number; lg?: number; xl?: number };

/** Card count drives a balanced row (no orphan card, no dead slot); phones stack one widget per row. */
function sizeFor(n: number): GridSize {
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
 * KPI widget row on the template dashboard grid: `Grid container spacing={3}` with the widgets full
 * width at xs, as the template's app/analytics/ecommerce dashboards lay out their widget summaries.
 * `min` is accepted for older callers and no longer used (the Grid breakpoints size the cards).
 * (MUI Grid only; moved out of components/minimal, which holds verbatim template files.)
 */
export function KpiGrid({ children, className }: { children: ReactNode; min?: number; className?: string }) {
  const items = Children.toArray(children).filter(Boolean);
  const size = sizeFor(items.length);
  return (
    <Grid container spacing={3} className={className}>
      {items.map((child, i) => (
        <Grid key={i} size={size} sx={{ display: "flex", minWidth: 0, "& > *": { flex: "1 1 auto", minWidth: 0 } }}>
          {child}
        </Grid>
      ))}
    </Grid>
  );
}
