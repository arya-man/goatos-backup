import Stack from "@mui/material/Stack";
import { KpiRowSkeleton } from "@/components/app/skeletons";

// Layout shared by /health/analytics, its loading.tsx and its UrlSuspense fallback.
/** The top KPI row: three md-4 tiles, then the two death tiles at md 6 each, so five tiles leave no
 *  orphan on the second row (TR2-P2-15; guard: health-kpi-no-orphan). */
export const HEALTH_KPI_ROW1 = { count: 3, size: { xs: 12, sm: 6, md: 4 } } as const;
export const HEALTH_KPI_ROW2_MD = 6;
export const HEALTH_KPI_ROW2 = { count: 2, size: { xs: 12, sm: 6, md: HEALTH_KPI_ROW2_MD } } as const;

/** The top KPI row's skeleton: the same two Grid rows (Stack spacing 3 = the Grid spacing). */
export function HealthKpiSkeleton() {
  return (
    <Stack spacing={3}>
      <KpiRowSkeleton count={HEALTH_KPI_ROW1.count} hint size={HEALTH_KPI_ROW1.size} />
      <KpiRowSkeleton count={HEALTH_KPI_ROW2.count} hint size={HEALTH_KPI_ROW2.size} />
    </Stack>
  );
}
