import { SkeletonCard } from "@/components/app/page-skeletons";
import { PageHeaderSkeleton } from "@/components/app/page-header";

export default function Loading() {
  return (
    <div className="screen on">
      <PageHeaderSkeleton />
      <div style={{ display: "grid", gap: 16 }} aria-busy="true">
        <SkeletonCard lines={2} />
        {Array.from({ length: 3 }, (_, i) => (
          <SkeletonCard key={i} lines={4} />
        ))}
      </div>
    </div>
  );
}
