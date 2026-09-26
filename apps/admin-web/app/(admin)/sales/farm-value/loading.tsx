import { Skeleton } from "@/components/app/page-skeletons";
import { PageHeaderSkeleton } from "@/components/app/page-header";
import { skeletonClasses } from "@/components/app/page-skeletons";

/**
 * Shimmer placeholder shaped like the REAL Farm value layout: page header, the farm-scope chip
 * row, the headed valuation block with its THREE tiles (farm value, total meat, Over 35 kg with
 * its tolerance control), then the by-category breakdown card — a tag in the head and the
 * seven-tile grid that divides the same total.
 *
 * It previously rendered a chart block and a six-row table, neither of which this route has.
 * Presentation only — it renders no copy.
 */
export default function Loading() {
  return (
    <div className="screen on" aria-busy="true">
      <PageHeaderSkeleton />

      {/* Farm scope chips: a label followed by the served farm options. */}
      <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 14, flexWrap: "wrap" }} aria-hidden="true">
        <Skeleton width={44} height={12} />
        {[92, 76, 76].map((width, index) => (
          <Skeleton key={index} width={width} height={30} radius={999} />
        ))}
      </div>

      {/* Valuation block: its own heading, then three tiles. */}
      <div style={{ display: "grid", gap: 12, marginBottom: 16 }} aria-hidden="true">
        <div style={{ display: "flex", alignItems: "baseline", gap: 12 }}>
          <Skeleton width={150} height={17} />
          <Skeleton width={230} height={11} />
        </div>
        <div className={skeletonClasses.kpis}>
          {[0, 1, 2].map((index) => (
            <div key={index} className={skeletonClasses.card} style={{ gap: 10, padding: 20 }}>
              <Skeleton width="50%" height={12} />
              <Skeleton width="68%" height={30} radius={10} />
              <Skeleton width="42%" height={10} />
              {/* The Over 35 kg tile carries the sale-ready tolerance control. */}
              {index === 2 ? (
                <div style={{ display: "flex", gap: 8, marginTop: 4 }}>
                  <Skeleton width="58%" height={30} radius={8} />
                  <Skeleton width={66} height={30} radius={8} />
                </div>
              ) : null}
            </div>
          ))}
        </div>
      </div>

      {/* Breakdown card: head with a tag, then the seven category tiles. */}
      <div className={skeletonClasses.card} style={{ gap: 16 }} aria-hidden="true">
        <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 12, flexWrap: "wrap" }}>
          <Skeleton width={210} height={17} />
          <Skeleton width={250} height={24} radius={999} />
        </div>
        <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit,minmax(180px,1fr))", gap: 14 }}>
          {Array.from({ length: 7 }, (_, index) => (
            <div key={index} style={{ display: "grid", gap: 7, padding: 14, borderRadius: 12, border: "1px dashed var(--line)" }}>
              <Skeleton width={`${46 + ((index * 13) % 26)}%`} height={11} />
              <Skeleton width="62%" height={10} />
              <Skeleton width="76%" height={22} radius={8} />
              <Skeleton width="88%" height={10} />
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
