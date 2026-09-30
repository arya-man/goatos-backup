import { FilterCardSkeleton, PageHeaderSkeleton, PageSkeleton, StackSkeleton, TableSkeleton, TabsSkeleton } from "@/components/app/skeletons";

/**
 * /alerts, block for block with AlertsPage: header + Configure (the crumb repeats the title, so
 * PageHeader shows none), then one Stack spacing 3 holding the alerts card (severity tabs with counts, the park select + day stepper toolbar,
 * the table rows; no pager: every row renders).
 */
export default function Loading() {
  return (
    <PageSkeleton root="" gap={3}>
      <PageHeaderSkeleton crumbs={false} titleWidth={80} actionWidths={[156]} />
      <StackSkeleton>
        <TableSkeleton
          columns={6}
          rows={5}
          pager={false}
          header={false}
          tabs={<TabsSkeleton count={3} counts />}
          toolbar={<FilterCardSkeleton inCard fields={[200]} actionWidths={[36, 120, 36, 120]} />}
        />
      </StackSkeleton>
    </PageSkeleton>
  );
}
