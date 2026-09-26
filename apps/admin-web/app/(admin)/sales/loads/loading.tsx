import { Skeleton, SkeletonChart, SkeletonTable } from "@/components/app/page-skeletons";
import { PageHeaderSkeleton } from "@/components/app/page-header";
import { skeletonClasses } from "@/components/app/page-skeletons";

/**
 * Shimmer placeholder shaped like the REAL Purchase-and-Born layout: a page header with no filter
 * chips (the view toggle only renders when the contract serves more than one view) and no KPI
 * deck, then the single Load wise card — stock-price note, summary strip, FIVE stacked charts
 * (animals, money, weight in/out, cost vs return per kg, fattening clock) and the load table.
 *
 * It previously rendered a four-tile KPI deck, a chip row and two charts, none of which this route
 * has; the page then jumped as it settled. Presentation only — it renders no copy.
 */
export default function Loading() {
  return (
    <div className="screen on sales-loads-page" aria-busy="true">
      <PageHeaderSkeleton />

      <div className={skeletonClasses.card} style={{ gap: 16 }}>
        {/* Card head: icon + title, then the stock-price note. */}
        <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
          <Skeleton width={20} height={20} radius={6} />
          <Skeleton width={180} height={18} />
        </div>
        <Skeleton width="min(520px,80vw)" height={12} />

        {/* Summary strip — four inline totals above the charts. */}
        <div style={{ display: "flex", flexWrap: "wrap", gap: 28 }}>
          {[132, 110, 148, 124].map((width) => (
            <div key={width} style={{ display: "grid", gap: 7 }}>
              <Skeleton width={width * 0.62} height={10} />
              <Skeleton width={width} height={20} radius={8} />
            </div>
          ))}
        </div>

        {/* The five charts the page actually draws, each under its own caption. */}
        {[0, 1, 2, 3, 4].map((index) => (
          <div key={index} style={{ display: "grid", gap: 10 }}>
            <Skeleton width={`${26 + ((index * 9) % 18)}%`} height={13} />
            <SkeletonChart bars={12} height={186} card={false} />
            {/* Axis band under the columns. */}
            <Skeleton width="100%" height={9} radius={4} />
          </div>
        ))}

        {/* The load table. */}
        <div style={{ display: "grid", gap: 10 }}>
          <Skeleton width="24%" height={14} />
          <SkeletonTable rows={10} />
        </div>
      </div>
    </div>
  );
}
