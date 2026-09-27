import { FilterCardSkeleton, KpiRowSkeleton, StackSkeleton, StatStripSkeleton, TableSkeleton } from "@/components/app/skeletons";
import { MILK_DEFAULT_PAGE_SIZE, MILK_FARM_STATE_KEYS, MILK_FILTER_FIELDS, MILK_KPIS, MILK_KPI_SIZE, MILK_TABLE_COLUMNS } from "./milk-preparation-layout";

// Loading twins for /counts/milk-preparation, composed ONLY from the shared skeleton blocks and the
// page's milk-preparation-layout.ts. The route loading.tsx and the page's UrlSuspense fallback render
// them, so a hard load, a sidebar click and a park change paint one shape.

/** The KPI + farm-state panel: the KpiWidget cards (a unit caption where the card has one) over the InvoiceAnalytic strip. */
export function MilkPreparationPanelSkeleton() {
  return (
    <StackSkeleton spacing={3}>
      <KpiRowSkeleton count={MILK_KPIS.length} size={MILK_KPI_SIZE} shapes={MILK_KPIS.map((kpi) => ({ hint: kpi.unit }))} />
      <StatStripSkeleton count={MILK_FARM_STATE_KEYS.length} meta />
    </StackSkeleton>
  );
}

/** The worklist card: title + prepared-for subheader, the park WorklistFilters (folded below md), the table (contract "milk-preparation": 10 columns), pager. */
export function MilkPreparationTableSkeleton() {
  return <TableSkeleton columns={MILK_TABLE_COLUMNS} rows={MILK_DEFAULT_PAGE_SIZE} subheader toolbar={<FilterCardSkeleton inCard fold fields={MILK_FILTER_FIELDS} />} />;
}
