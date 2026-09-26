"use client";

import Grid from "@mui/material/Grid";

import type { IconifyName } from "@/components/minimal/iconify";
import { BankingWidgetSummary } from "@/components/minimal/widgets/banking-widget-summary";
import type { PaletteColorKey } from "@/theme/core";

export type OversightKpi = {
  key: string;
  title: string;
  total: string;
  hint?: string;
  color: PaletteColorKey;
  icon: IconifyName;
};

/**
 * The oversight KPI strip: template BankingWidgetSummary tiles (round tone badge, subtitle2 title
 * with the info tooltip, h4 figure) on the template Grid. Client leaf because the widget reads the
 * theme; the numbers and copy arrive pre-formatted from the server component.
 */
export function OversightKpis({ items }: { items: OversightKpi[] }) {
  return (
    <Grid container spacing={3}>
      {items.map((item) => (
        <Grid key={item.key} size={{ xs: 12, sm: 6 }}>
          <BankingWidgetSummary title={item.title} total={item.total} hint={item.hint} color={item.color} icon={item.icon} />
        </Grid>
      ))}
    </Grid>
  );
}
