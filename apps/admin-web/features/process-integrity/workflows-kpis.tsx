"use client";

import Grid from "@mui/material/Grid";

import { EcommerceWidgetSummary } from "@/components/minimal/sections/overview/e-commerce/ecommerce-widget-summary";

// No per-KPI series on this read: the card shows figure + caption, the sparkline stays hidden.
const NO_TREND = { categories: [], series: [] };

export type WorkflowsKpi = { key: string; title: string; total: number | string; caption?: string };

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
          <EcommerceWidgetSummary title={item.title} total={item.total} caption={item.caption} chart={NO_TREND} />
        </Grid>
      ))}
    </Grid>
  );
}
