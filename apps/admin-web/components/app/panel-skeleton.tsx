import Stack from "@mui/material/Stack";

import { Skeleton, SkeletonChart, SkeletonKpiRow, SkeletonTable, skeletonClasses } from "@/components/app/page-skeletons";

/**
 * The skeleton of ONE data panel under a page's header/tabs/filters (the `UrlSuspense` fallback).
 * Only the panel's own blocks: KPI deck, chart cards, a table card. No header, no tabs, no filters:
 * those stay on screen. Renders no copy. Composed from the shared skeleton blocks so the skeleton
 * owner can reshape every panel in one place.
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
  tableWidths?: string[];
  spark?: boolean;
}) {
  return (
    <Stack spacing={3} aria-busy="true" data-panel-skeleton="" sx={{ mb: 3 }}>
      {kpis > 0 ? <SkeletonKpiRow count={kpis} spark={spark} /> : null}
      {Array.from({ length: charts }, (_, index) => (
        <SkeletonChart key={index} bars={14} height={220} />
      ))}
      {table > 0 ? (
        <div className={skeletonClasses.card}>
          <Skeleton width="30%" height={18} />
          <SkeletonTable rows={table} widths={tableWidths} />
        </div>
      ) : null}
    </Stack>
  );
}
