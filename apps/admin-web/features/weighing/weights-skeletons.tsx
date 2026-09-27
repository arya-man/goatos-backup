import Stack from "@mui/material/Stack";
import { ChartCardSkeleton, FilterCardSkeleton, GridSkeleton, KpiCardSkeleton, TableSkeleton } from "@/components/app/skeletons";
import { WEIGHTS_DEFAULT_LIMIT } from "./weights-analytics-layout";

// Loading twins for /weighing/weights and /weighing/analytics, composed ONLY from the shared skeleton
// blocks. The route loading.tsx files and the pages' in-page UrlSuspense fallbacks render the SAME
// components, so a hard load, a sidebar click and a filter change paint one shape.

/** The page WorklistFilters card: five fields from md up, folded into its "Filters" button below md. */
export function WeightsFilterSkeleton() {
  return <FilterCardSkeleton fold fields={[160, 300, 160, 160, 160]} />;
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
        ...Array.from({ length: 4 }, () => ({ size: { xs: 12, sm: 6, lg: 3 }, node: <KpiCardSkeleton hint hintLines={{ xs: 1, lg: 2 }} /> })),
        { size: { xs: 12, md: 6, lg: 4 }, node: <ChartCardSkeleton height={420} subheader /> },
        { size: { xs: 12, md: 6, lg: 8 }, node: <ChartCardSkeleton height={420} subheader legend /> },
        { size: { xs: 12, lg: 6 }, node: <ChartCardSkeleton height={360} action /> },
        {
          size: { xs: 12, lg: 6 },
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
        ...Array.from({ length: 3 }, () => ({ size: { xs: 12, md: 4 }, node: <KpiCardSkeleton hint /> })),
        { size: { xs: 12, md: 6, lg: 4 }, node: <ChartCardSkeleton height={{ xs: 358, lg: 382 }} subheader /> },
        { size: { xs: 12, md: 6, lg: 8 }, node: <ChartCardSkeleton height={382} subheader /> },
        { size: { xs: 12, md: 6, lg: 8 }, node: <ChartCardSkeleton height={320} subheader /> },
        { size: { xs: 12, md: 6, lg: 4 }, node: <ChartCardSkeleton height={320} /> },
        { size: 12, node: <TableSkeleton columns={9} rows={WEIGHTS_DEFAULT_LIMIT} headerAction toolbar={<FilterCardSkeleton inCard fold fields={[160, 160, 160]} />} /> },
      ]}
    />
  );
}
