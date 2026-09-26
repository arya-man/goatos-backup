import { Skeleton, SkeletonCard } from "@/components/app/page-skeletons";
import { PageHeaderSkeleton } from "@/components/app/page-header";

/**
 * Route-shaped shimmer for /work-board.
 *
 * The real page is a MULTI-COLUMN BOARD (`.col` > `.ch` header chip + count > `.cards`), not one
 * wide card — the previous loader drew a KPI row and a single `SkeletonCard`, so the whole page
 * re-flowed on hydration. This draws the toolbar and four lane columns with their header chip,
 * count badge and card placeholders.
 */
const LANES = [4, 3, 3, 2];

export default function Loading() {
  return (
    <div className="screen on" aria-busy="true">
      <PageHeaderSkeleton action />
      <div style={{ display: "flex", flexWrap: "wrap", gap: 10, margin: "16px 0" }}>
        <Skeleton width={240} height={36} radius={10} />
        {[140, 120, 160].map((w, wi) => (
          <Skeleton key={`${wi}-${w}`} width={w} height={36} radius={10} />
        ))}
      </div>
      <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit,minmax(min(260px,100%),1fr))", gap: 14, alignItems: "start" }}>
        {LANES.map((cards, lane) => (
          <div key={lane} style={{ display: "flex", flexDirection: "column", gap: 10 }}>
            <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
              <Skeleton width={110} height={16} />
              <Skeleton width={26} height={18} radius={999} />
            </div>
            {Array.from({ length: cards }, (_, i) => (
              <SkeletonCard key={i} lines={2} />
            ))}
          </div>
        ))}
      </div>
    </div>
  );
}
