import { ChartCardSkeleton, FilterCardSkeleton, GridSkeleton, KpiCardSkeleton, PageHeaderSkeleton, PageSkeleton } from "@/components/app/skeletons";

/**
 * /weighing/weights: header + download, the filter card, four EcommerceWidgetSummary cards, the
 * sale-ready ring (lg 4) beside breed-wise daily gain (lg 8), then breed (lg 6) / sex / stage (lg 3).
 */
export default function Loading() {
  return (
    <PageSkeleton gap={3} root="">
      <PageHeaderSkeleton actions={1} />
      <FilterCardSkeleton fields={[160, 260, 160, 160, 160]} />
      <GridSkeleton
        items={[
          ...Array.from({ length: 4 }, () => ({ size: { xs: 12, sm: 6, lg: 3 }, node: <KpiCardSkeleton hint /> })),
          { size: { xs: 12, md: 6, lg: 4 }, node: <ChartCardSkeleton height={420} subheader /> },
          { size: { xs: 12, md: 6, lg: 8 }, node: <ChartCardSkeleton height={420} subheader legend /> },
          { size: { xs: 12, lg: 6 }, node: <ChartCardSkeleton height={360} action /> },
          { size: { xs: 12, sm: 6, lg: 3 }, node: <ChartCardSkeleton height={360} action /> },
          { size: { xs: 12, sm: 6, lg: 3 }, node: <ChartCardSkeleton height={360} action /> },
        ]}
      />
    </PageSkeleton>
  );
}
