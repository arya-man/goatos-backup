import { BlockSkeleton, GridSkeleton, ListCardSkeleton, OrderToolbarSkeleton, PageHeaderSkeleton, PageSkeleton, StatStripSkeleton, TableSkeleton, TabsSkeleton } from "@/components/app/skeletons";
import { AUDIT_ADVANCED_COLLAPSED_HEIGHT, AUDIT_ANOMALIES_BUTTON_TWIN_WIDTH, AUDIT_FAMILY_SELECT_WIDTH, AUDIT_OPERATOR_SKELETON_ROWS, AUDIT_SIDE_GRID, AUDIT_STATUS_TABS, AUDIT_STRIP_CELLS, AUDIT_TRAIL_COLUMNS, PAGE_SIZE } from "@/features/operations-audit/audit-layout";

/** /operations/audit: header + Export (plain crumb), the InvoiceAnalytic strip, the list card (status tabs, family select + Anomalies + search, table, pager), operators beside the advanced filters. */
export default function Loading() {
  return (
    <PageSkeleton root="page-root">
      <PageHeaderSkeleton crumbLink={false} titleWidth={120} actions={1} />
      <StatStripSkeleton count={AUDIT_STRIP_CELLS} meta wrapBelowMd />
      <TableSkeleton
        header={false}
        columns={AUDIT_TRAIL_COLUMNS.length}
        rows={PAGE_SIZE}
        tabs={<TabsSkeleton count={AUDIT_STATUS_TABS.length} counts />}
        toolbar={<OrderToolbarSkeleton fields={[AUDIT_FAMILY_SELECT_WIDTH]} trailing={[AUDIT_ANOMALIES_BUTTON_TWIN_WIDTH]} />}
      />
      <GridSkeleton
        items={[
          { size: AUDIT_SIDE_GRID.operators, node: <ListCardSkeleton rows={AUDIT_OPERATOR_SKELETON_ROWS} /> },
          { size: AUDIT_SIDE_GRID.advanced, node: <BlockSkeleton height={AUDIT_ADVANCED_COLLAPSED_HEIGHT} /> },
        ]}
      />
    </PageSkeleton>
  );
}
