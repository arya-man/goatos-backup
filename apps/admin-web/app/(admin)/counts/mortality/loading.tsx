import { Skeleton, SkeletonChart, SkeletonKpiRow, SkeletonTable } from "@/components/app/page-skeletons";
import { PageHeaderSkeleton } from "@/components/app/page-header";
import { skeletonClasses } from "@/components/app/page-skeletons";

/** Route-shaped shimmer for Counts -> Mortality: window filter, six KPI tiles, the monthly
 *  stacked chart, then the two-up rate tables. Same column rhythm as the real cards. */
export default function Loading() {
  return (
    <div className="pagegrid mortality-page" aria-busy="true">
      <PageHeaderSkeleton />
      <div style={{ display: "flex", gap: 12, alignItems: "center" }}>
        <Skeleton width={280} height={40} radius={10} />
      </div>
      <SkeletonKpiRow count={6} spark />
      <SkeletonChart bars={12} height={240} withLegend />
      <div className="mortality-grid">
        {Array.from({ length: 4 }, (_, i) => (
          <div key={i} className={skeletonClasses.card}>
            <Skeleton width="34%" height={18} />
            <SkeletonTable rows={5} widths={["2fr", "70px", "70px", "60px", "1.4fr"]} />
          </div>
        ))}
      </div>
    </div>
  );
}
