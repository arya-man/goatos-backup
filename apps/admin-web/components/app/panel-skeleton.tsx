import Stack from "@mui/material/Stack";

import { ChartCardSkeleton, KpiRowSkeleton, TableSkeleton } from "@/components/app/skeletons";

/**
 * The skeleton of ONE data panel under a page's header/tabs/filters (the `UrlSuspense` fallback).
 * Only the panel's own blocks: KPI deck, chart cards, a table card. No header, no tabs, no filters:
 * those stay on screen. Renders no copy. Composed from the shared skeleton blocks
 * (components/app/skeletons) so the skeleton owner can reshape every panel in one place.
 */
export function PanelSkeleton({
  kpis = 0,
  charts = 0,
  table = 0,
  tableWidths,
  spark = false,
}: {
  kpis?: number;
  charts?: number;
  table?: number;
  /** Column count source for the table card (one entry per column). */
  tableWidths?: string[];
  spark?: boolean;
}) {
  return (
    <Stack spacing={3} aria-busy="true" data-panel-skeleton="" sx={{ mb: 3 }}>
      {kpis > 0 ? <KpiRowSkeleton count={kpis} spark={spark} trend={spark} /> : null}
      {Array.from({ length: charts }, (_, index) => (
        <ChartCardSkeleton key={index} height={220} action />
      ))}
      {table > 0 ? <TableSkeleton columns={tableWidths?.length ?? 6} rows={table} /> : null}
    </Stack>
  );
}
