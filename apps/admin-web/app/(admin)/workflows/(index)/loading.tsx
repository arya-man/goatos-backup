import {
  DetailCardSkeleton,
  FilterCardSkeleton,
  GridSkeleton,
  KpiRowSkeleton,
  ListCardSkeleton,
  PageHeaderSkeleton,
  PageSkeleton,
  StackSkeleton,
  TableSkeleton,
  TabsSkeleton,
} from "@/components/app/skeletons";

/** /workflows: header, four KPI cards, the catalog table card (tabs, toolbar, table, pager) beside the chain rail. */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton />
      <KpiRowSkeleton count={4} hint />
      <GridSkeleton
        items={[
          {
            size: { xs: 12, lg: 8 },
            node: (
              <TableSkeleton
                header={false}
                columns={5}
                rows={10}
                tabs={<TabsSkeleton count={1} counts />}
                toolbar={<FilterCardSkeleton inCard fields={["search", 120]} />}
              />
            ),
          },
          {
            size: { xs: 12, lg: 4 },
            node: (
              <StackSkeleton>
                <ListCardSkeleton rows={6} />
                <DetailCardSkeleton rows={4} />
              </StackSkeleton>
            ),
          },
        ]}
      />
    </PageSkeleton>
  );
}
