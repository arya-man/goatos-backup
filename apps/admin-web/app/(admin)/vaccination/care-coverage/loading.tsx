import { Skeleton, SkeletonCard, SkeletonTable } from "@/components/app/page-skeletons";
import { PageHeaderSkeleton } from "@/components/app/page-header";

// Care Coverage's shape: the page header, the filter bar (park select + pen picker), then the
// pens x PC Care jobs matrix card.
export default function Loading() {
  return (
    <div className="screen on">
      <PageHeaderSkeleton />
      <div style={{ display: "grid", gap: "var(--sp-2)" }} aria-busy="true">
        <SkeletonCard lines={0}>
          <div style={{ display: "flex", gap: "var(--sp-1h)", flexWrap: "wrap" }}>
            <Skeleton width={180} height={44} radius={10} />
            <Skeleton width={150} height={44} radius={10} />
          </div>
        </SkeletonCard>
        <SkeletonCard lines={0}>
          <SkeletonTable rows={8} />
        </SkeletonCard>
      </div>
    </div>
  );
}
