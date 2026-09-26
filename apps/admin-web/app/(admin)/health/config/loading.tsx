import { Skeleton, SkeletonChart, SkeletonKpiRow, SkeletonTable } from "@/components/app/page-skeletons";
import { PageHeaderSkeleton } from "@/components/app/page-header";
import { skeletonClasses } from "@/components/app/page-skeletons";

/** Route-shaped shimmer: matches the real page's header, deck, and blocks, so the swap is a fade, not a jump. */
export default function Loading() {
  return (
    <div className="screen on" aria-busy="true">
      <PageHeaderSkeleton />
      <div style={{ marginBottom: 20 }}>
        <SkeletonKpiRow count={5} />
      </div>
      <div className={skeletonClasses.card} style={{ gap: 16 }}>
        <Skeleton width="30%" height={18} />
        <div style={{ display: "flex", gap: 10, flexWrap: "wrap" }}>
          {[240, 160, 160, 120].map((w, wi) => (
            <Skeleton key={`${wi}-${w}`} width={w} height={40} radius={10} />
          ))}
        </div>
        <SkeletonTable rows={10} />
      </div>
    </div>
  );
}
