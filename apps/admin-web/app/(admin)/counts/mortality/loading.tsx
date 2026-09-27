import { ChartCardSkeleton, ControlRowSkeleton, FieldSkeleton, GridSkeleton, KpiRowSkeleton, PageHeaderSkeleton, PageSkeleton, TableSkeleton } from "@/components/app/skeletons";

/** /counts/mortality: header, date range, six EcommerceWidgetSummary cards, the stacked monthly chart, then the two-up rate cards. */
export default function Loading() {
  return (
    <PageSkeleton gap={3} root="">
      <PageHeaderSkeleton />
      {/* The small Window select: full width on a phone (44px tap floor), 300 x 56 from md. */}
      <ControlRowSkeleton>
        <FieldSkeleton width={{ xs: "100%", sm: 300 }} height={{ xs: 44, md: 56 }} />
      </ControlRowSkeleton>
      {/* Deaths and the rate cards carry no caption; the second caption wraps to two lines on a phone. */}
      <KpiRowSkeleton count={6} shapes={[{}, { hint: true, hintLines: { xs: 2, sm: 1 } }, { hint: true }, { hint: true }, {}, { hint: true }]} />
      <ChartCardSkeleton height={364} />
      <GridSkeleton
        items={[0, 1, 2, 3].map(() => ({ size: { xs: 12, lg: 6 }, node: <TableSkeleton columns={5} rows={5} pager={false} /> }))}
      />
    </PageSkeleton>
  );
}
