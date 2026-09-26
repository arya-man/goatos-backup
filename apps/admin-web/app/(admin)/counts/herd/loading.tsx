import { Skeleton, SkeletonKpiRow, SkeletonTable } from "@/components/app/page-skeletons";
import { PageHeaderSkeleton } from "@/components/app/page-header";

/** Route-shaped shimmer: mirrors the real layout of this route so the page settles instead of flashing. */
export default function Loading() {
  return (
    <div className="screen on" aria-busy="true">
      <PageHeaderSkeleton action />
      <Skeleton height={56} radius={14} style={{ marginBottom: 16 }} />
      <SkeletonKpiRow count={4} spark />
      {/* Shape-matched to the real herd table: 11 columns, the leading checkbox cell and the
          trailing row-action circle, so the swap does not re-flow the row. */}
      <SkeletonTable
        rows={12}
        cols={11}
        widths={["44px", "2fr", "1.4fr", "1.4fr", "1fr", "1fr", "1fr", "110px", "110px", "110px", "44px"]}
      />
    </div>
  );
}
