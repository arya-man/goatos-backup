import { FilterCardSkeleton, GridSkeleton, KpiRowSkeleton, ListCardSkeleton, OptionalSkeleton, PageHeaderSkeleton, PageSkeleton, StackSkeleton, TableSkeleton } from "@/components/app/skeletons";

/** /vaccination/live-tracker (route loading AND the page's Suspense fallback): header + poller / schedule buttons, the filter card, the six-tile deck, operators / pens beside the activity rail. */
export default function Loading() {
  return (
    <PageSkeleton root="lt-page lt-live-page">
      <PageHeaderSkeleton actionWidths={[96, 120, 140, 150]} />
      <FilterCardSkeleton fields={[180, 180, 180, 180, 180]} />
      <OptionalSkeleton>
        <KpiRowSkeleton count={6} hint />
      </OptionalSkeleton>
      <GridSkeleton
        items={[
          {
            size: { xs: 12, lg: 8 },
            node: (
              <StackSkeleton>
                <TableSkeleton columns={5} rows={5} pager={false} />
                <TableSkeleton columns={5} rows={5} pager={false} />
              </StackSkeleton>
            ),
          },
          { size: { xs: 12, lg: 4 }, node: <ListCardSkeleton rows={6} /> },
        ]}
      />
    </PageSkeleton>
  );
}
