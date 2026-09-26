import { Skeleton, SkeletonCard, SkeletonKpiRow, SkeletonTable } from "@/components/app/page-skeletons";
import { PageHeaderSkeleton } from "@/components/app/page-header";

export default function Loading() {
  return (
    <div className="screen on">
      <PageHeaderSkeleton action />
      <div style={{ display: "grid", gap: 16 }} aria-busy="true">
        <SkeletonCard lines={0}>
          <div style={{ display: "flex", gap: 12, flexWrap: "wrap" }}>
            <Skeleton width={190} height={44} radius={10} />
            <Skeleton width={320} height={44} radius={10} />
          </div>
          <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
            {[96, 86, 116, 72, 110, 132].map((w, i) => (
              <Skeleton key={i} width={w} height={30} radius={999} />
            ))}
          </div>
          <SkeletonKpiRow count={8} />
        </SkeletonCard>
        <SkeletonCard lines={2} />
        <SkeletonCard lines={0}>
          <SkeletonTable rows={8} />
        </SkeletonCard>
      </div>
    </div>
  );
}
