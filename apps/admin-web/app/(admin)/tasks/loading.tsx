import { Skeleton } from "@/components/app/page-skeletons";
import { PageHeaderSkeleton } from "@/components/app/page-header";
import { skeletonClasses } from "@/components/app/page-skeletons";

/**
 * Shimmer shaped like the real Tasks desk as it opens (the BOARD view): kit pagehead with the
 * New task button, the scope strip beside the Board/List toggle, the toolbar row, then the four
 * status columns — each with the header block (name + count pill) and two card blocks — so the
 * page settles in place instead of flashing. Presentation only.
 */
export default function Loading() {
  return (
    <div className="screen on lt-page" aria-busy="true">
      <PageHeaderSkeleton action />

      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", gap: 16, margin: "8px 0 16px" }}>
        <Skeleton width={220} height={40} radius={10} />
        <Skeleton width={116} height={40} radius={10} />
      </div>
      <div style={{ display: "flex", gap: 8, flexWrap: "wrap", marginBottom: 20 }}>
        <Skeleton width={300} height={40} radius={10} />
        <Skeleton width={330} height={40} radius={10} />
        <Skeleton width={120} height={40} radius={10} />
        <Skeleton width={180} height={40} radius={10} />
        <Skeleton width={128} height={40} radius={10} />
      </div>

      <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit,minmax(min(230px,100%),1fr))", gap: 14 }}>
        {Array.from({ length: 4 }, (_, i) => (
          <div key={i} className={skeletonClasses.card} style={{ minHeight: 170, gap: 12 }}>
            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
              <Skeleton width="42%" height={12} />
              <Skeleton width={24} height={20} radius={999} />
            </div>
            <Skeleton height={64} radius={12} />
            {i === 0 ? <Skeleton height={64} radius={12} /> : null}
          </div>
        ))}
      </div>
    </div>
  );
}
