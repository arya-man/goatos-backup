import { SkeletonCard, SkeletonTable } from "@/components/app/page-skeletons";
import { PageHeaderSkeleton } from "@/components/app/page-header";

export default function Loading() {
  return (
    <div className="screen on">
      <PageHeaderSkeleton />
      <div style={{ display: "grid", gap: 16 }} aria-busy="true">
        <SkeletonCard lines={0}>
          <SkeletonTable rows={6} />
        </SkeletonCard>
        <SkeletonCard lines={3} />
      </div>
    </div>
  );
}
