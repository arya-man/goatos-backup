import { Skeleton, SkeletonCard } from "@/components/app/page-skeletons";
import { PageHeaderSkeleton } from "@/components/app/page-header";

/**
 * Route-shaped shimmer for /calendar.
 *
 * Mirrors the real page: header (breadcrumb + title + view switcher on the right), the month
 * picker strip, the owner tab row, then the drive-card grid. It deliberately does NOT draw a KPI
 * row or the two charts the previous version drew — the calendar renders neither, so those bars
 * re-flowed the whole page on hydration.
 */
export default function Loading() {
  return (
    <div className="screen on" aria-busy="true">
      <PageHeaderSkeleton />
      <div style={{ display: "flex", alignItems: "center", gap: 10, margin: "16px 0" }}>
        <Skeleton width={32} height={32} radius={10} />
        <Skeleton width={180} height={20} />
        <Skeleton width={32} height={32} radius={10} />
      </div>
      <div style={{ display: "flex", gap: 10, marginBottom: 16 }}>
        {[120, 140, 110, 96].map((w, wi) => (
          <Skeleton key={`${wi}-${w}`} width={w} height={34} radius={999} />
        ))}
      </div>
      <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fill,minmax(min(320px,100%),1fr))", gap: 16 }}>
        {Array.from({ length: 6 }, (_, i) => (
          <SkeletonCard key={i} lines={3} />
        ))}
      </div>
    </div>
  );
}
