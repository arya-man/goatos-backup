import { Skeleton, SkeletonCard, SkeletonKpiRow, SkeletonTable } from "@/components/app/page-skeletons";
import { PageHeaderSkeleton } from "@/components/app/page-header";

export default function Loading() {
  return (
    <div className="screen on">
      <PageHeaderSkeleton />
      <div style={{ display: "grid", gap: 16 }} aria-busy="true">
        <SkeletonKpiRow count={6} />
        <SkeletonCard lines={0}>
          <div style={{ display: "flex", gap: 12, flexWrap: "wrap" }}>
            {[200, 200, 160, 160].map((w, i) => (
              <Skeleton key={i} width={w} height={44} radius={10} />
            ))}
          </div>
          <SkeletonTable rows={10} />
        </SkeletonCard>
      </div>
    </div>
  );
}
