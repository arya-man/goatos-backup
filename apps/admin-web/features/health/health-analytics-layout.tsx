import Grid from "@mui/material/Grid";
import { KpiCardSkeleton } from "@/components/app/skeletons";

// Layout shared by /health/analytics, its loading.tsx and its UrlSuspense fallback.
/** The top KPI grid: ONE Grid container, three md-4 tiles then the two death tiles at md 6 each, so
 *  five tiles leave no orphan on the second row (TR2-P2-15; guard: health-kpi-no-orphan). The page
 *  and the skeleton both read this list, tile by tile (TR3-P0-2: the skeleton drew two separate
 *  row blocks against the page's one grid). */
export const HEALTH_KPI_MD = [4, 4, 4, 6, 6] as const;
export const healthKpiSize = (i: number) => ({ xs: 12, sm: 6, md: HEALTH_KPI_MD[i] });

/** The top KPI grid's skeleton: the same single Grid container and per-tile sizes as the page. */
export function HealthKpiSkeleton() {
  return (
    <div data-skel="kpis" aria-hidden="true">
      <Grid container spacing={3}>
        {HEALTH_KPI_MD.map((_, i) => (
          <Grid key={i} size={healthKpiSize(i)} sx={{ minWidth: 0 }}>
            <KpiCardSkeleton hint />
          </Grid>
        ))}
      </Grid>
    </div>
  );
}
