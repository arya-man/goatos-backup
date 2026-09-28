import { FilterCardSkeleton, KpiRowSkeleton, StackSkeleton, TableSkeleton } from "@/components/app/skeletons";
import { LIMIT_DEFAULT } from "./params";
import { HERD_SIGNALS_BAR_FIELDS, HERD_SIGNALS_KPI_CAPTION_LINES, HERD_SIGNALS_KPI_WRAPPED_KEYS, HERD_SIGNALS_KPI_WRAPPED_LINES, HERD_SIGNALS_LIVE_COLUMNS } from "./herd-signals-layout";
import { KPI_DEFS } from "./herd-signals-kpi-defs";

// Loading twins for /herd-signals' Live Monitor, composed ONLY from the shared skeleton blocks. The
// route loading.tsx, the tab-strip fallback and the Live panel's UrlSuspense fallback render these,
// so a hard load, a tab click and a filter / KPI change paint one shape.

/** HerdSignalsFilters card: the bar fields (all in its md+ controls box, search included); one "Filters" button below md. */
export function HerdSignalsFilterSkeleton() {
  return <FilterCardSkeleton fold foldSearch fields={HERD_SIGNALS_BAR_FIELDS} />;
}

/**
 * The Live panel under the filters: the KPI filter cards (KPI_DEFS) (KpiWidget with a detail caption, two
 * lines at four across) and the "Live tag signals" table card (title + count Label,
 * no subheader, 21 columns). The card carries `mt: 3` on top of the page gap, hence spacing 6.
 */
export function HerdSignalsLivePanelSkeleton() {
  return (
    <StackSkeleton spacing={6}>
      <KpiRowSkeleton
        count={KPI_DEFS.length}
        shapes={KPI_DEFS.map((def) => ({ hint: true, hintLines: HERD_SIGNALS_KPI_WRAPPED_KEYS.includes(def.key) ? HERD_SIGNALS_KPI_WRAPPED_LINES : HERD_SIGNALS_KPI_CAPTION_LINES }))}
      />
      <TableSkeleton columns={HERD_SIGNALS_LIVE_COLUMNS} rows={LIMIT_DEFAULT} />
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
