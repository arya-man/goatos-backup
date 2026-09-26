import { Skeleton, SkeletonChart, SkeletonKpiRow } from "@/components/app/page-skeletons";
import { PageHeaderSkeleton } from "@/components/app/page-header";

/** Route-shaped shimmer: mirrors the real layout of this route so the page settles instead of flashing. */
export default function Loading() {
  return (
    <div className="screen on" aria-busy="true">
      <PageHeaderSkeleton action />
      <Skeleton height={56} radius={14} style={{ marginBottom: 16 }} />
      <SkeletonKpiRow count={6} spark />
      <SkeletonChart bars={14} height={300} withLegend />
      <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit,minmax(min(360px,100%),1fr))", gap: 16, marginTop: 16 }}>
        <SkeletonChart bars={8} height={200} />
        <SkeletonChart bars={8} height={200} />
        <SkeletonChart bars={8} height={200} />
      </div>
    </div>
  );
}
