import { Children, type ReactNode } from "react";
import Grid from "@mui/material/Grid";

import { kpiTileSize } from "./kpi-grid-size";

/**
 * KPI widget row on the template dashboard grid: `Grid container spacing={3}` with the widgets full
 * width at xs, as the template's app/analytics/ecommerce dashboards lay out their widget summaries.
 * `min` is accepted for older callers and no longer used (the Grid breakpoints size the cards).
 * (MUI Grid only; moved out of components/minimal, which holds verbatim template files.)
 */
export function KpiGrid({ children, className }: { children: ReactNode; min?: number; className?: string }) {
  const items = Children.toArray(children).filter(Boolean);
  return (
    <Grid container spacing={3} className={className}>
      {items.map((child, i) => (
        <Grid key={i} size={kpiTileSize(items.length, i)} sx={{ display: "flex", minWidth: 0, "& > *": { flex: "1 1 auto", minWidth: 0 } }}>
          {child}
        </Grid>
      ))}
    </Grid>
  );
}
