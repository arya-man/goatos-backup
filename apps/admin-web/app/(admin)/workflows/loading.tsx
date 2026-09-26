import { Skeleton, SkeletonKpiRow } from "@/components/app/page-skeletons";
import { PageHeaderSkeleton } from "@/components/app/page-header";
import { skeletonClasses } from "@/components/app/page-skeletons";

/**
 * Shimmer placeholder shaped like the real Workflows screen: page header, the four-tile KPI deck,
 * the single module pill, and the `.wfwrap` two-column body — a 300px catalog rail (search +
 * filter + rows) beside the chain-reaction map. It used to end in a full-width table, which is a
 * layout this route never renders.
 *
 * Presentation only — it renders no copy, and `.kit-skeleton` pauses its sweep under
 * `prefers-reduced-motion`.
 */
export default function Loading() {
  return (
    <div className="screen on" aria-busy="true">
      <PageHeaderSkeleton />

      <SkeletonKpiRow count={4} className="kit-kpi-wrap" />

      {/* The module pill toolbar — one pill, matching the live page. */}
      <div className="wftoolbar" aria-hidden="true">
        <Skeleton width={160} height={34} radius={999} />
      </div>

      <div className="wfwrap" aria-hidden="true">
        {/* Catalog rail: search + filter button, then grouped rows. */}
        <div className={skeletonClasses.card} style={{ padding: 14, gap: 10 }}>
          <Skeleton width="55%" height={13} />
          <div style={{ display: "flex", gap: 8 }}>
            <Skeleton width="100%" height={34} radius={10} />
            <Skeleton width={38} height={34} radius={10} />
          </div>
          <div className={skeletonClasses.group} style={{ gap: 8, marginTop: 4 }}>
            {Array.from({ length: 8 }, (_, index) => (
              <div key={index} style={{ display: "flex", alignItems: "center", gap: 10 }}>
                <Skeleton width={10} height={10} radius={999} />
                <div style={{ display: "grid", gap: 5, flex: 1, minWidth: 0 }}>
                  <Skeleton width={`${62 + ((index * 11) % 26)}%`} height={12} />
                  <Skeleton width={`${44 + ((index * 17) % 32)}%`} height={10} />
                </div>
              </div>
            ))}
          </div>
        </div>

        {/* Chain-reaction map: state chips over a run of stage cards. */}
        <div className={skeletonClasses.card} style={{ gap: 14 }}>
          <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
            {[104, 88, 122, 96, 132].map((width) => (
              <Skeleton key={width} width={width} height={24} radius={999} />
            ))}
          </div>
          <Skeleton width="46%" height={18} />
          <div className={skeletonClasses.group} style={{ gap: 12 }}>
            {Array.from({ length: 5 }, (_, index) => (
              <div
                key={index}
                style={{ display: "grid", gap: 8, padding: 14, borderRadius: 12, border: "1px dashed var(--line)" }}
              >
                <Skeleton width={`${34 + ((index * 13) % 22)}%`} height={13} />
                <Skeleton width="100%" height={8} radius={999} />
                <Skeleton width={`${52 + ((index * 19) % 30)}%`} height={10} />
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
