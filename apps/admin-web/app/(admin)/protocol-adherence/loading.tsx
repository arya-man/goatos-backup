import { Skeleton, SkeletonKpiRow, SkeletonTable } from "@/components/app/page-skeletons";
import { PageHeaderSkeleton } from "@/components/app/page-header";

export default function Loading() {
  return (
    <div className="screen on" aria-busy="true">
      <PageHeaderSkeleton />
      <SkeletonKpiRow count={4} className="kit-kpi-wrap" />
      <div style={{ display: "flex", gap: 10, margin: "14px 0" }}>
        <Skeleton width={230} height={36} radius={999} />
        <Skeleton width={180} height={36} radius={999} />
      </div>
      <SkeletonTable rows={10} />
    </div>
  );
}
