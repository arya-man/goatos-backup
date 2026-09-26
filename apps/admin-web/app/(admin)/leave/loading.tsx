import { Skeleton } from "@/components/app/page-skeletons";
import { PageHeaderSkeleton } from "@/components/app/page-header";
import { skeletonClasses } from "@/components/app/page-skeletons";

// Shimmer shaped like the real Leave desk: page head, the "who approves" config card (help line +
// two ticks + save), the approval queue table, then the all-requests table behind its status tab
// strip. No KPI deck, because the page has none. Presentation only -- it renders no copy.
function TableCard({ columns, rows, tabs }: { columns: string[]; rows: number; tabs?: boolean }) {
  return (
    <div className={skeletonClasses.card} style={{ padding: 0, gap: 0, overflow: "hidden", marginBottom: 16 }}>
      <div style={{ display: "flex", alignItems: "center", gap: 12, padding: "20px 24px 12px" }}>
        <Skeleton width={190} height={20} />
        <div style={{ flex: 1 }} />
        {tabs ? (
          <div style={{ display: "flex", gap: 8 }}>
            {[52, 72, 78, 70, 82].map((w, wi) => (
              <Skeleton key={`${wi}-${w}`} width={w} height={30} radius={999} />
            ))}
          </div>
        ) : (
          <Skeleton width={34} height={22} radius={8} />
        )}
      </div>
      <div className={skeletonClasses.thead}>
        {columns.map((w, wi) => (
          <Skeleton key={`${wi}-${w}`} width={w} height={12} />
        ))}
      </div>
      {Array.from({ length: rows }, (_, row) => (
        <div key={row} style={{ display: "flex", gap: 16, alignItems: "center", padding: "16px 24px" }}>
          {columns.map((w, i) => (
            <Skeleton key={`${i}-${w}`} width={w} height={i === 0 ? 32 : 14} radius={i === 0 ? 10 : 8} />
          ))}
        </div>
      ))}
    </div>
  );
}

export default function Loading() {
  return (
    <div className="screen on" aria-busy="true">
      <PageHeaderSkeleton />

      <div className={skeletonClasses.card} style={{ gap: 14, marginBottom: 16 }}>
        <Skeleton width={190} height={20} />
        <Skeleton width={420} height={14} style={{ maxWidth: "100%" }} />
        <div style={{ display: "flex", gap: 20, alignItems: "center" }}>
          <Skeleton width={230} height={20} />
          <Skeleton width={70} height={20} />
          <Skeleton width={78} height={34} radius={10} />
        </div>
      </div>

      <TableCard columns={["22%", "18%", "26%", "18%", "16%"]} rows={4} />
      <TableCard columns={["20%", "14%", "18%", "24%", "14%", "10%"]} rows={6} tabs />
    </div>
  );
}
