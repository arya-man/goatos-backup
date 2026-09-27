import { OrderToolbarSkeleton, PageHeaderSkeleton, PageSkeleton, StatStripSkeleton, TableSkeleton, TabsSkeleton } from "@/components/app/skeletons";

/** /operations/dlq: header + Open audit, the InvoiceAnalytic status strip, the list card (tabs, toolbar, events table, readout footer). */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton crumbLink={false} titleWidth={120} actions={1} />
      <StatStripSkeleton count={4} />
      {/* Contract table "dlq-events": 6 columns, one read of 100 rows. */}
      <TableSkeleton
        header={false}
        columns={6}
        rows={10}
        tabs={<TabsSkeleton count={3} counts />}
        toolbar={<OrderToolbarSkeleton fields={[200, 200]} buttons={[72]} />}
      />
    </PageSkeleton>
  );
}
