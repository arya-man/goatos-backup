"use client";

import Grid from "@mui/material/Grid";

import { KpiWidget } from "@/components/app/kpi-widget";
import type { PaletteColorKey } from "@/theme/core";

export type OversightKpi = {
  key: string;
  title: string;
  /** Leads the visible sub-line ("h · …"): the template widget prints a bare number. */
  unit?: string;
  total: number | null;
  hint?: string;
  color: PaletteColorKey;
  icon?: string;
};

/**
 * The oversight KPI strip: template CourseWidgetSummary tiles (KpiWidget adapter) on the template
 * Grid. The hint is the card tooltip.
 */
export function OversightKpis({ items }: { items: OversightKpi[] }) {
  return (
    <Grid container spacing={3}>
      {items.map((item) => (
        <Grid key={item.key} size={{ xs: 12, sm: 6 }}>
          <KpiWidget title={item.title} total={item.total} caption={[item.total == null ? "—" : item.unit, item.hint].filter(Boolean).join(" · ") || undefined} color={item.color} sx={{ height: 1 }} />
        </Grid>
      ))}
    </Grid>
  );
}
