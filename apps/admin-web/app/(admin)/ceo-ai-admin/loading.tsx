import { Skeleton } from "@/components/app/page-skeletons";
import { PageHeaderSkeleton } from "@/components/app/page-header";
import { skeletonClasses } from "@/components/app/page-skeletons";

// Shape-matched placeholder for the step-trace debug surface: page header,
// lookup card (label + input + button), the 8-tile request-summary grid, the
// redacted-question card, and the steps table with the real column widths.
export default function Loading() {
  return (
    <div className="screen on" aria-busy="true">
      <PageHeaderSkeleton />

      <div style={{ display: "flex", flexDirection: "column", gap: 24 }}>
        {/* Lookup card */}
        <div className={skeletonClasses.card}>
          <Skeleton width={320} height={20} />
          <Skeleton width="72%" height={14} style={{ marginTop: 4 }} />
          <div style={{ display: "flex", alignItems: "flex-end", gap: 16, marginTop: 16, flexWrap: "wrap" }}>
            <div style={{ display: "flex", flexDirection: "column", gap: 6, flex: "1 1 380px" }}>
              <Skeleton width={78} height={11} />
              <Skeleton width="100%" height={40} radius={8} />
            </div>
            <Skeleton width={148} height={36} radius={8} />
          </div>
        </div>

        {/* Request summary: 8 meta tiles */}
        <div className={skeletonClasses.card}>
          <Skeleton width={190} height={20} />
          <div
            style={{ display: "grid", gap: 16, gridTemplateColumns: "repeat(auto-fit,minmax(180px,1fr))", marginTop: 16 }}
          >
            {Array.from({ length: 8 }, (_, i) => (
              <div key={i} style={{ display: "flex", flexDirection: "column", gap: 8, padding: 16, borderRadius: 12, background: "var(--paper-2)" }}>
                <Skeleton width="56%" height={11} />
                <Skeleton width="42%" height={16} />
              </div>
            ))}
          </div>
        </div>

        {/* Redacted question */}
        <div className={skeletonClasses.card}>
          <Skeleton width={210} height={20} />
          <Skeleton width="94%" height={13} style={{ marginTop: 14 }} />
          <Skeleton width="66%" height={13} style={{ marginTop: 8 }} />
        </div>

        {/* Steps table */}
        <div className={skeletonClasses.card}>
          <Skeleton width={190} height={32} radius={8} />
          <div style={{ marginTop: 16 }}>
            <div style={{ display: "flex", gap: 14, padding: "12px 14px", borderRadius: 8, background: "var(--paper-2)" }}>
              <Skeleton width={28} height={12} />
              <Skeleton width={110} height={12} />
              <Skeleton width={170} height={12} />
              <Skeleton width="34%" height={12} />
              <Skeleton width={68} height={12} style={{ marginLeft: "auto" }} />
              <Skeleton width={52} height={12} />
            </div>
            {Array.from({ length: 6 }, (_, i) => (
              <div
                key={i}
                style={{ display: "flex", alignItems: "center", gap: 14, padding: "14px", borderBottom: "1px solid var(--line)" }}
              >
                <Skeleton width={24} height={24} radius={6} />
                <Skeleton width={110} height={26} radius={6} />
                <Skeleton width={170} height={13} />
                <Skeleton width="34%" height={13} />
                <Skeleton width={68} height={13} style={{ marginLeft: "auto" }} />
                <Skeleton width={52} height={13} />
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
