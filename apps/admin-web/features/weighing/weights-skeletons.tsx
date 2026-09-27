import Stack from "@mui/material/Stack";
import { ChartCardSkeleton, FilterCardSkeleton, GridSkeleton, KpiCardSkeleton, TableSkeleton } from "@/components/app/skeletons";
import { GENERAL_GRID, GENERAL_KPI_COUNT, KIDS_GRID, KIDS_KPI_COUNT, PENS_FILTER_FIELDS, WEIGHTS_DEFAULT_LIMIT, WEIGHTS_FILTER_FIELDS } from "./weights-analytics-layout";

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
 * two lines there), the sale-ready ring (lg 4) beside breed-wise daily gain (lg 8), then breed (lg 6)
 * beside sex + stage stacked (lg 6).
 */
export function WeightsKidsPanelSkeleton() {
  return (
    <GridSkeleton
      items={[
        ...Array.from({ length: KIDS_KPI_COUNT }, () => ({ size: KIDS_GRID.kpi, node: <KpiCardSkeleton hint hintLines={{ xs: 1, lg: 2 }} /> })),
        { size: KIDS_GRID.ring, node: <ChartCardSkeleton height={420} subheader /> },
        { size: KIDS_GRID.breedGain, node: <ChartCardSkeleton height={420} subheader legend /> },
        { size: KIDS_GRID.breed, node: <ChartCardSkeleton height={360} action /> },
        {
          size: KIDS_GRID.sexStage,
          node: (
            <Stack spacing={3} direction={{ xs: "column", sm: "row", lg: "column" }}>
              <ChartCardSkeleton height={120} action />
              <ChartCardSkeleton height={120} action />
            </Stack>
          ),
        },
      ]}
    />
  );
}

/**
 * /weighing/analytics General tab (GeneralTab's Grid): three KpiWidget cards (md 4), the sale-ready
 * ring (lg 4) beside weekly growth (lg 8), pen gain ranking (lg 8) beside the park gain balance card
 * (lg 4), then the pens table card (count Label, the staged pens WorklistFilters, 9 columns).
 */
export function WeightsGeneralPanelSkeleton() {
  return (
    <GridSkeleton
      items={[
        ...Array.from({ length: GENERAL_KPI_COUNT }, () => ({ size: GENERAL_GRID.kpi, node: <KpiCardSkeleton hint /> })),
        { size: GENERAL_GRID.ring, node: <ChartCardSkeleton height={{ xs: 358, lg: 382 }} subheader /> },
        { size: GENERAL_GRID.weekly, node: <ChartCardSkeleton height={382} subheader /> },
        { size: GENERAL_GRID.rank, node: <ChartCardSkeleton height={320} subheader /> },
        { size: GENERAL_GRID.parkGain, node: <ChartCardSkeleton height={320} /> },
        { size: 12, node: <TableSkeleton columns={9} rows={WEIGHTS_DEFAULT_LIMIT} headerAction toolbar={<FilterCardSkeleton inCard fold fields={PENS_FILTER_FIELDS} />} /> },
      ]}
    />
  );
}
