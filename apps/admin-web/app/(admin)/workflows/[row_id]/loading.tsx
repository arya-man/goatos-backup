import { Skeleton, SkeletonCard } from "@/components/app/page-skeletons";
import { PageHeaderSkeleton } from "@/components/app/page-header";

export default function Loading() {
  return (
    <div className="screen on" aria-busy="true">
      <PageHeaderSkeleton />
      <div style={{ display: "flex", gap: 8, flexWrap: "wrap", marginBottom: 14 }}>
        {Array.from({ length: 5 }, (_, i) => (
          <Skeleton key={i} width={110} height={26} radius={999} />
        ))}
      </div>
      <SkeletonCard lines={6} />
    </div>
  );
}
