import { FilterCardSkeleton, KpiRowSkeleton, PageHeaderSkeleton, PageSkeleton, StackSkeleton, TableSkeleton, TabsSkeleton } from "@/components/app/skeletons";
import {
  ADHERENCE_FILTERS_BUTTON_WIDTH,
  ADHERENCE_LEDGER_HEADER_SX,
  ADHERENCE_TAB_STATES,
  ADHERENCE_TILE_SIZE,
  ADHERENCE_TOOLBAR_FIELDS,
} from "@/features/process-integrity/protocol-adherence-layout";

/**
 * /protocol-adherence (template order list), block for block: header (crumb links home, no
 * actions), then the page Stack: four KpiWidget course tiles with captions, and ONE ledger card:
 * CardHeader (title + adherence Label + formula "i", note subheader), All + 4 work-state tabs, the
 * toolbar (severity select, work-state select, Filters), the 7-column table and its pager.
 */
export default function Loading() {
  return (
    <PageSkeleton root="page-root">
      <PageHeaderSkeleton titleWidth={345} />
      <StackSkeleton>
        <KpiRowSkeleton count={4} hint size={ADHERENCE_TILE_SIZE} />
        <TableSkeleton
          columns={7}
          rows={10}
          headerSx={ADHERENCE_LEDGER_HEADER_SX}
          subheader
          tabs={<TabsSkeleton count={ADHERENCE_TAB_STATES.length + 1} counts />}
          toolbar={<FilterCardSkeleton inCard fields={ADHERENCE_TOOLBAR_FIELDS} actionWidths={[ADHERENCE_FILTERS_BUTTON_WIDTH]} />}
        />
      </StackSkeleton>
    </PageSkeleton>
  );
}
