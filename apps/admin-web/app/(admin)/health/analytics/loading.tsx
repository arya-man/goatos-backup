import { Skeleton, SkeletonChart, SkeletonKpiRow, SkeletonTable } from "@/components/app/page-skeletons";
import { PageHeaderSkeleton } from "@/components/app/page-header";

/** Route-shaped shimmer: matches the real page's header, deck, and blocks, so the swap is a fade, not a jump. */
export default function Loading() {
  return (
    <div className="screen on" aria-busy="true">
      <PageHeaderSkeleton />
      <div style={{ display: "flex", gap: 10, marginBottom: 20 }}>
        {[96, 88, 104, 92, 84].map((w, wi) => (
          <Skeleton key={`${wi}-${w}`} width={w} height={36} radius={999} />
        ))}
      </div>
      <div style={{ marginBottom: 20 }}>
        <SkeletonKpiRow count={5} />
      </div>
      <div style={{ display: "grid", gap: 20 }}>
        <SkeletonChart bars={14} height={260} />
        <SkeletonChart bars={10} height={220} />
      </div>
    </div>
  );
}
