import { ChartCardSkeleton, FilterCardSkeleton, GridSkeleton, KpiRowSkeleton, PageHeaderSkeleton, PageSkeleton, TableSkeleton } from "@/components/app/skeletons";

/**
 * /counts/breakdown: header, the EcommerceWidgetSummary count deck, the pen table card (filters in
 * the card, pager), breed share (md 5) beside stage × sex (md 7), then the shed bars.
 */
export default function Loading() {
  return (
    <PageSkeleton gap={3} root="" className="counts-breakdown-page">
      <PageHeaderSkeleton crumbLink={false} />
      {/* The first card (the live head count) has no caption line; the other five do. */}
      <KpiRowSkeleton count={6} shapes={[{}, ...Array.from({ length: 5 }, () => ({ hint: true }))]} />
      <TableSkeleton columns={8} rows={10} header={false} toolbar={<FilterCardSkeleton inCard fields={[160, 160, 160, 160, 160]} />} />
      <GridSkeleton
        items={[
          { size: { xs: 12, md: 5 }, node: <ChartCardSkeleton height={364} /> },
          { size: { xs: 12, md: 7 }, node: <ChartCardSkeleton height={364} /> },
          { size: 12, node: <ChartCardSkeleton height={360} /> },
        ]}
      />
    </PageSkeleton>
  );
}
