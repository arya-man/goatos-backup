import { Skeleton, SkeletonKpiRow } from "@/components/app/page-skeletons";
import { PageHeaderSkeleton } from "@/components/app/page-header";
import { skeletonClasses } from "@/components/app/page-skeletons";

// Shimmer shaped like the real Routines page: page head with the park pills, the five-tile Today
// summary, the routines table, then the Today tasks table with its day stepper and routine filter.
// Presentation only -- it renders no copy.
function TableCard({ columns, rows, tools }: { columns: string[]; rows: number; tools?: boolean }) {
  return (
    <div className={skeletonClasses.card} style={{ padding: 0, gap: 0, overflow: "hidden", marginTop: 16 }}>
      <div style={{ display: "flex", alignItems: "center", gap: 12, padding: "20px 24px 12px" }}>
        <Skeleton width={200} height={20} />
        <div style={{ flex: 1 }} />
        {tools ? (
          <div style={{ display: "flex", gap: 10, alignItems: "center" }}>
            <Skeleton width={150} height={30} radius={8} />
            <Skeleton width={180} height={40} radius={10} />
          </div>
        ) : (
          <Skeleton width={128} height={32} radius={10} />
        )}
      </div>
      <div className={skeletonClasses.thead}>
        {columns.map((w, wi) => (
          <Skeleton key={`${wi}-${w}`} width={w} height={12} />
        ))}
      </div>
      {Array.from({ length: rows }, (_, row) => (
        <div key={row} style={{ display: "flex", gap: 16, alignItems: "center", padding: "16px 24px" }}>
          {columns.map((w, wi) => (
            <Skeleton key={`${wi}-${w}`} width={w} height={14} />
          ))}
        </div>
      ))}
    </div>
  );
}

export default function Loading() {
  return (
    <div className="screen on proc-mx" aria-busy="true">
      <PageHeaderSkeleton />

      <SkeletonKpiRow count={5} />

      <TableCard columns={["18%", "12%", "18%", "18%", "16%", "10%", "8%"]} rows={6} />
      <TableCard columns={["24%", "20%", "22%", "18%", "16%"]} rows={8} tools />
    </div>
  );
}
