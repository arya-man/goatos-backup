import { KpiRowSkeleton, PageHeaderSkeleton, PageSkeleton, TableSkeleton, TabsSkeleton } from "@/components/app/skeletons";

/** /alerts: header + Configure, three KPI cards, the alerts table card (park / severity / day pills in its header). */
export default function Loading() {
  return (
    <PageSkeleton className="alerts-page">
      <PageHeaderSkeleton actions={1} />
      <KpiRowSkeleton count={3} icon />
      {/* Contract table "alerts": severity, alert, park, pen, detail, rule. No pager: every row renders. */}
      <TableSkeleton columns={6} rows={8} pager={false} headerAction={<TabsSkeleton count={3} variant="pill" />} />
    </PageSkeleton>
  );
}
