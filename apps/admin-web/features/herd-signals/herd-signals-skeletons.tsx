import { FilterCardSkeleton, KpiRowSkeleton, StackSkeleton, TableSkeleton } from "@/components/app/skeletons";
import { LIMIT_DEFAULT } from "./params";

// Loading twins for /herd-signals' Live Monitor, composed ONLY from the shared skeleton blocks. The
// route loading.tsx, the tab-strip fallback and the Live panel's UrlSuspense fallback render these,
// so a hard load, a tab click and a filter / KPI change paint one shape.

/** HerdSignalsFilters card: search + shed + movement + Columns (all in its md+ controls box); one "Filters" button below md. */
export function HerdSignalsFilterSkeleton() {
  return <FilterCardSkeleton fold fields={[496, 160, 188, 128]} />;
}

/**
 * The Live panel under the filters: the eight KPI filter cards (KpiWidget with a detail caption, two
 * lines at four across) and the "Live tag signals" table card (title + count Label, the aggregate
 * note subheader, 21 columns). The card carries `mt: 3` on top of the page gap, hence spacing 6.
 */
export function HerdSignalsLivePanelSkeleton() {
  return (
    <StackSkeleton spacing={6}>
      {/* KPI_DEFS: the second card's detail ("… in the last minute") wraps to two lines even full width. */}
      <KpiRowSkeleton count={8} shapes={Array.from({ length: 8 }, (_, i) => ({ hint: true, hintLines: i === 1 ? 2 : { xs: 1, md: 2 } }))} />
      <TableSkeleton columns={21} rows={LIMIT_DEFAULT} subheader />
    </StackSkeleton>
  );
}

/** The whole Live tab body (filters + panel) as the tab strip's fallback. */
export function HerdSignalsLiveTabSkeleton() {
  return (
    <StackSkeleton spacing={3}>
      <HerdSignalsFilterSkeleton />
      <HerdSignalsLivePanelSkeleton />
    </StackSkeleton>
  );
}
