import { Skeleton, SkeletonKpiRow, SkeletonTable } from "@/components/app/page-skeletons";
import { PageHeaderSkeleton } from "@/components/app/page-header";

export default function Loading() {
  return (
    <div className="screen on" aria-busy="true">
      <PageHeaderSkeleton />
      <div style={{ display: "flex", gap: 8, margin: "20px 0 16px" }}>
        {[120, 140, 100].map((w, wi) => (
          <Skeleton key={`${wi}-${w}`} width={w} height={34} radius={10} />
        ))}
      </div>
      <SkeletonKpiRow count={4} />
      <SkeletonTable rows={9} />
    </div>
  );
}
