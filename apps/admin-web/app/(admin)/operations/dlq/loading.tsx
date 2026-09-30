import { OrderToolbarSkeleton, PageHeaderSkeleton, PageSkeleton, StatStripSkeleton, TableSkeleton, TabsSkeleton } from "@/components/app/skeletons";
import { DLQ_APPLY_TWIN_WIDTH, DLQ_EVENT_COLUMNS, DLQ_FIELD_WIDTH, DLQ_SKELETON_ROWS, DLQ_STATUS_KEYS, DLQ_STRIP_CELLS } from "@/features/operations-dlq/dlq-layout";

/** /operations/dlq: header + Audit Log (plain crumb), the InvoiceAnalytic status strip, the list card (status tabs, event-type + topic + Apply form and search, events table, readout footer). */
export default function Loading() {
  return (
    <PageSkeleton root="page-root">
      <PageHeaderSkeleton crumbLink={false} titleWidth={120} actions={1} />
      <StatStripSkeleton count={DLQ_STRIP_CELLS} minHeight={false} />
      <TableSkeleton
        header={false}
        columns={DLQ_EVENT_COLUMNS.length}
        rows={DLQ_SKELETON_ROWS}
        tabs={<TabsSkeleton count={DLQ_STATUS_KEYS.length} counts />}
        toolbar={<OrderToolbarSkeleton fields={[DLQ_FIELD_WIDTH, DLQ_FIELD_WIDTH]} buttons={[DLQ_APPLY_TWIN_WIDTH]} />}
      />
    </PageSkeleton>
  );
}
