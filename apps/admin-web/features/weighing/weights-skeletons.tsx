import Stack from "@mui/material/Stack";
import { ChartCardSkeleton, FilterCardSkeleton, GridSkeleton, KpiCardSkeleton, TableSkeleton } from "@/components/app/skeletons";
import {
  GENERAL_CHART_PLOT,
  GENERAL_GRID,
  GENERAL_KPI_COUNT,
  KIDS_CHART_PLOT,
  KIDS_GRID,
  KIDS_KPI_CAPTION_LINES,
  KIDS_KPI_COUNT,
  PENS_FILTER_FIELDS,
  PENS_TABLE_COLUMNS,
  WEIGHTS_DEFAULT_LIMIT,
  WEIGHTS_FILTER_FIELDS,
} from "./weights-analytics-layout";

// Loading twins for /weighing/weights and /weighing/analytics, composed ONLY from the shared skeleton
// blocks. The route loading.tsx files and the pages' in-page UrlSuspense fallbacks render the SAME
// components, so a hard load, a sidebar click and a filter change paint one shape. Grid sizes, card
// counts and field lists come from weights-analytics-layout.ts, which the pages import too.

/** The page WorklistFilters card: five fields from md up, folded into its "Filters" button below md. */
export function WeightsFilterSkeleton() {
  return <FilterCardSkeleton fold fields={WEIGHTS_FILTER_FIELDS} />;
}

/**
 * /weighing/weights panel (the Grid under the filters): four KpiWidget cards (lg 3, captions wrap to
 * two lines there), breed-wise daily gain across the row, the sale-ready ring (lg 4) beside breed
 * (lg 8), then sex + stage side by side.
 */
export function WeightsKidsPanelSkeleton() {
  return (
    <GridSkeleton
      items={[
        ...Array.from({ length: KIDS_KPI_COUNT }, () => ({ size: KIDS_GRID.kpi, node: <KpiCardSkeleton hint hintLines={KIDS_KPI_CAPTION_LINES} /> })),
        { size: KIDS_GRID.breedGain, node: <ChartCardSkeleton height={KIDS_CHART_PLOT.breedGain} subheader legend /> },
        { size: KIDS_GRID.ring, node: <ChartCardSkeleton height={KIDS_CHART_PLOT.ring} subheader /> },
        { size: KIDS_GRID.breed, node: <ChartCardSkeleton height={KIDS_CHART_PLOT.breed} action /> },
        {
          size: KIDS_GRID.sexStage,
          node: (
            <Stack spacing={3} direction={{ xs: "column", sm: "row" }}>
              <ChartCardSkeleton height={KIDS_CHART_PLOT.sexStage} action />
              <ChartCardSkeleton height={KIDS_CHART_PLOT.sexStage} action />
            </Stack>
          ),
        },
      ]}
    />
  );
}

/**
 * /weighing/analytics General tab (GeneralTab's Grid): three KpiWidget cards (md 4), the sale-ready
 * ring (lg 4), weekly growth (lg 5) and the park gain balance card (lg 3), the pen gain ranking
 * across the row, then the pens table card (count Label, the staged pens WorklistFilters, 9 columns).
 */
export function WeightsGeneralPanelSkeleton() {
  return (
    <GridSkeleton
      items={[
        ...Array.from({ length: GENERAL_KPI_COUNT }, () => ({ size: GENERAL_GRID.kpi, node: <KpiCardSkeleton hint /> })),
        { size: GENERAL_GRID.ring, node: <ChartCardSkeleton height={GENERAL_CHART_PLOT.ring} subheader /> },
        { size: GENERAL_GRID.weekly, node: <ChartCardSkeleton height={GENERAL_CHART_PLOT.weekly} subheader /> },
        { size: GENERAL_GRID.parkGain, node: <ChartCardSkeleton height={GENERAL_CHART_PLOT.parkGain} /> },
        { size: GENERAL_GRID.rank, node: <ChartCardSkeleton height={GENERAL_CHART_PLOT.rank} subheader /> },
        { size: GENERAL_GRID.pens, node: <TableSkeleton columns={PENS_TABLE_COLUMNS} rows={WEIGHTS_DEFAULT_LIMIT} headerAction toolbar={<FilterCardSkeleton inCard fold fields={PENS_FILTER_FIELDS} />} /> },
      ]}
    />
  );
}
