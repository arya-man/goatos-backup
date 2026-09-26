import { Skeleton, SkeletonChart, SkeletonKpiRow, SkeletonTable } from "@/components/app/page-skeletons";
import { PageHeaderSkeleton } from "@/components/app/page-header";

/** Route-shaped shimmer: mirrors the real layout of this route so the page settles instead of flashing. */
export default function Loading() {
  return (
    <div className="screen on" aria-busy="true">
      <PageHeaderSkeleton action />
      <Skeleton height={56} radius={14} style={{ marginBottom: 16 }} />
      <SkeletonKpiRow count={6} spark />
      <SkeletonChart bars={12} height={260} withLegend />
      <div style={{ marginTop: 16 }}>
        <SkeletonTable rows={10} widths={["1.8fr", "1fr", "1fr", "1fr", "1fr", "44px"]} />
      </div>
    </div>
  );
}
