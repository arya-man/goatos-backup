import { Skeleton, SkeletonList, SkeletonTable } from "@/components/app/page-skeletons";
import { PageHeaderSkeleton } from "@/components/app/page-header";
import { skeletonClasses } from "@/components/app/page-skeletons";

/**
 * Route-shaped shimmer for /goats/[goat_id] (the passport): kit pagehead with status pills on the
 * right, the summary card with its two definition columns, the identifiers table card beside the
 * evidence card, then the timeline list. Same tracks as the real page.
 */
export default function Loading() {
  return (
    <div className="screen on gp-page" aria-busy="true">
      <PageHeaderSkeleton />
      <div className={skeletonClasses.card}>
        <Skeleton width="16%" height={18} />
        <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit,minmax(min(320px,100%),1fr))", gap: "0 32px" }}>
          {[0, 1].map((col) => (
            <div key={col} style={{ display: "grid", gap: 0 }}>
              {Array.from({ length: 5 }, (_, i) => (
                <div key={i} style={{ display: "grid", gridTemplateColumns: "38% 1fr", gap: 12, padding: "12px 0" }}>
                  <Skeleton height={12} width="70%" />
                  <Skeleton height={14} width={`${44 + ((i * 17 + col * 11) % 40)}%`} />
                </div>
              ))}
            </div>
          ))}
        </div>
      </div>
      <div className="gp-grid">
        <div className={skeletonClasses.card}>
          <Skeleton width="24%" height={18} />
          <SkeletonTable rows={2} widths={["60px", "1.6fr", "70px", "80px", "70px", "120px", "90px"]} />
          <Skeleton width="20%" height={14} style={{ marginTop: 8 }} />
          <SkeletonTable rows={1} header={false} widths={["1fr", "1fr", "1fr"]} />
        </div>
        <div className={skeletonClasses.card}>
          <Skeleton width="30%" height={18} />
          <Skeleton width="60%" height={12} />
        </div>
      </div>
      <SkeletonList rows={4} avatar />
    </div>
  );
}
