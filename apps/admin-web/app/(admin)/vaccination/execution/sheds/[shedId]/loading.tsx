import { SkeletonCard, SkeletonKpiRow, SkeletonTable } from "@/components/app/page-skeletons";
import { PageHeaderSkeleton } from "@/components/app/page-header";

export default function Loading() {
  return (
    <div className="screen on">
      <PageHeaderSkeleton />
      <div style={{ display: "grid", gap: 16 }} aria-busy="true">
        <SkeletonKpiRow count={4} />
        <SkeletonCard lines={0}>
          <SkeletonTable rows={10} />
        </SkeletonCard>
      </div>
    </div>
  );
}
