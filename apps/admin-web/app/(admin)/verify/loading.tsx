import { FilterCardSkeleton, PageHeaderSkeleton, PageSkeleton, StatStripSkeleton, TableSkeleton, TabsSkeleton } from "@/components/app/skeletons";
import { BOARD_TOOLBAR_FIELDS, HEADER_ACTION_WIDTHS, QUEUE_COLUMNS, QUEUE_LIMIT, STATUS_STRIP_CELLS, STATUS_TABS } from "@/features/verification-review/verification-layout";

/**
 * /verify, block for block with the page (template InvoiceListView): header + the panel buttons
 * (plain crumbs), the status summary strip card, then the board card: status tabs with counts, the
 * toolbar (module select, capture date), the queue table and its pager.
 */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton crumbLink={false} titleWidth={80} actionWidths={HEADER_ACTION_WIDTHS} />
      <StatStripSkeleton count={STATUS_STRIP_CELLS} />
      <TableSkeleton
        columns={QUEUE_COLUMNS}
        rows={QUEUE_LIMIT}
        header={false}
        tabs={<TabsSkeleton count={STATUS_TABS} counts />}
        toolbar={<FilterCardSkeleton inCard fields={BOARD_TOOLBAR_FIELDS} />}
      />
    </PageSkeleton>
  );
}
