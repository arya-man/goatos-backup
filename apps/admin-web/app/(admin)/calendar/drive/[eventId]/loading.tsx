import { Skeleton, SkeletonCard, SkeletonTable } from "@/components/app/page-skeletons";
import { PageHeaderSkeleton } from "@/components/app/page-header";

/**
 * Route-shaped shimmer for /calendar/drive/[eventId].
 *
 * Mirrors the real detail page: breadcrumb trail + title, the hero block whose right edge is the
 * progress ring, meta cards beside it, then the roster table with its real tracks (checkbox,
 * identity, two data columns, status chip, row menu). The previous version drew a four-tile KPI
 * row this page does not have and a generic four-column table, so the loader and the page shared
 * no structure at all.
 */
export default function Loading() {
  return (
    <div className="screen on" aria-busy="true">
      <PageHeaderSkeleton />
      <div style={{ display: "grid", gridTemplateColumns: "minmax(0,2fr) minmax(0,1fr)", gap: 16, alignItems: "start" }}>
        <div className="card" style={{ display: "flex", alignItems: "center", gap: 20, padding: 20 }}>
          <Skeleton width={104} height={104} radius="50%" />
          <div style={{ flex: 1, minWidth: 0 }}>
            <Skeleton width="60%" height={22} style={{ marginBottom: 12 }} />
            <Skeleton width="85%" height={14} style={{ marginBottom: 8 }} />
            <Skeleton width="45%" height={14} />
          </div>
        </div>
        <SkeletonCard lines={4} />
      </div>
      <div style={{ marginTop: 16 }}>
        <SkeletonTable rows={10} widths={["44px", "2.2fr", "1.2fr", "1fr", "120px", "44px"]} />
      </div>
    </div>
  );
}
