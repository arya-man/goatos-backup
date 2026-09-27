"use client";

import Grid from "@mui/material/Grid";

import { KpiWidget } from "@/components/app/kpi-widget";

// No per-KPI series on this read: the card shows figure + caption, the sparkline stays hidden.

export type WorkflowsKpi = { key: string; title: string; total: number | null; caption?: string };

/**
 * The Workflows KPI row: the template Ecommerce overview's EcommerceWidgetSummary cards
 * (sections/overview/e-commerce/view/overview-ecommerce-view.tsx, Grid size {xs:12, sm:6, md:3}).
 * Client leaf: the widget reads the theme for its trend colours.
 */
export function WorkflowsKpis({ items }: { items: WorkflowsKpi[] }) {
  return (
    <Grid container spacing={3}>
      {items.map((item) => (
        <Grid key={item.key} size={{ xs: 12, sm: 6, md: 3 }}>
          <KpiWidget title={item.title} total={item.total} caption={item.caption} />
        </Grid>
      ))}
    </Grid>
  );
}
