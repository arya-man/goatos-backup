import { ChipSkeleton, FilterCardSkeleton, KpiRowSkeleton, PageHeaderSkeleton, PageSkeleton, StackSkeleton, TableSkeleton, TabsSkeleton } from "@/components/app/skeletons";
import { WORK_STATE_ORDER } from "@/features/process-integrity/process-integrity";
import {
  ADHERENCE_FILTERS_BUTTON_WIDTH,
  ADHERENCE_LEDGER_HEADER_SX,
  ADHERENCE_TILE_SIZE,
  ADHERENCE_TOOLBAR_FIELDS,
} from "@/features/process-integrity/protocol-adherence-layout";

/**
 * /protocol-adherence (template order list), block for block: header (crumb links home; the info
 * "i" is the only action), then the page Stack: four KpiWidget course tiles with captions, and ONE
 * ledger card: CardHeader (title + adherence Label, info "i"), work-state tabs with counts, the
 * toolbar (severity select, Filters), the 7-column table and its pager.
 */
export default function Loading() {
  return (
    <PageSkeleton>
      {/* titleWidth = the crumb row's width (the widest line), so the "i" wraps under it on a phone as it does on the page. */}
      <PageHeaderSkeleton titleWidth={345} actionWidths={[24]} actionHeights={[24]} />
      <StackSkeleton>
        <KpiRowSkeleton count={4} hint size={ADHERENCE_TILE_SIZE} />
        <TableSkeleton
          columns={7}
          rows={10}
          headerSx={ADHERENCE_LEDGER_HEADER_SX}
          headerAction={<ChipSkeleton width={24} height={24} />}
          tabs={<TabsSkeleton count={WORK_STATE_ORDER.length + 1} counts />}
          toolbar={<FilterCardSkeleton inCard fields={ADHERENCE_TOOLBAR_FIELDS} actionWidths={[ADHERENCE_FILTERS_BUTTON_WIDTH]} />}
        />
      </StackSkeleton>
    </PageSkeleton>
  );
}
