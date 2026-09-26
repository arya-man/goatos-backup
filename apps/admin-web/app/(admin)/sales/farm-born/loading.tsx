import Box from "@mui/material/Box";
import { Skeleton, SkeletonKpiRow, SkeletonTable } from "@/components/app/page-skeletons";
import { PageHeaderSkeleton } from "@/components/app/page-header";
import { skeletonClasses } from "@/components/app/page-skeletons";

/** Route-shaped shimmer for Sales -> Farm born: header, filter bar, four KPI tiles, the breed /
 *  sex / stage breakdown pair, then the pen table. Same column rhythm as the real cards. */
export default function Loading() {
  return (
    <div className="screen on sales-farm-born-page" aria-busy="true">
      <PageHeaderSkeleton />
      <Skeleton height={64} radius={14} style={{ marginBottom: 16 }} />
      <SkeletonKpiRow count={4} />
      <div style={{ margin: "22px 0 10px" }}>
        <Skeleton width={220} height={18} />
      </div>
      <Box
        sx={{
          display: "grid",
          gridTemplateColumns: { xs: "minmax(0,1fr)", md: "repeat(2,minmax(0,1fr))" },
          gap: 2,
          mt: 1.75,
          alignItems: "start",
          "& > *, & .kit-sk-card": { minWidth: 0, maxWidth: "100%", overflow: "hidden" },
        }}
      >
        <div className={skeletonClasses.card}>
          <Skeleton width="30%" height={18} />
          <SkeletonTable rows={10} widths={["2fr", "1fr", "1fr", "1.6fr", "1.2fr"]} />
        </div>
        <div style={{ display: "grid", gap: 16, alignContent: "start" }}>
          <div className={skeletonClasses.card}>
            <Skeleton width="30%" height={18} />
            <SkeletonTable rows={2} widths={["2fr", "1fr", "1fr", "1.6fr", "1.2fr"]} />
          </div>
          <div className={skeletonClasses.card}>
            <Skeleton width="30%" height={18} />
            <SkeletonTable rows={6} widths={["2fr", "1fr", "1fr", "1.6fr", "1.2fr"]} />
          </div>
        </div>
      </Box>
      <Box sx={{ mt: 2, "& .kit-sk-card": { minWidth: 0, overflow: "hidden" } }}>
        <div className={skeletonClasses.card}>
          <Skeleton width="20%" height={18} />
          <SkeletonTable rows={10} widths={["2fr", "1fr", "1fr", "1.6fr", "1.2fr"]} />
        </div>
      </Box>
    </div>
  );
}
