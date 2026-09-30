import { DetailCardSkeleton, GridSkeleton, OrderDetailsToolbarSkeleton, PageSkeleton, StackSkeleton, TableSkeleton } from "@/components/app/skeletons";

/** /procurement/source-entry/loads/[load_id]: the OrderDetailsToolbar (back arrow, title + status Label, date line), then the 8/4 grid: goats table + action cards beside the load facts card. */
export default function Loading() {
  return (
    <PageSkeleton root="">
      <OrderDetailsToolbarSkeleton />
      <GridSkeleton
        items={[
          {
            size: { xs: 12, md: 8 },
            node: (
              <StackSkeleton>
                {/* Contract table "load-goats": 8 columns, PagedRows 10 per page. */}
                <TableSkeleton columns={8} rows={10} />
                <DetailCardSkeleton rows={3} />
              </StackSkeleton>
            ),
          },
          { size: { xs: 12, md: 4 }, node: <DetailCardSkeleton rows={8} header={false} /> },
        ]}
      />
    </PageSkeleton>
  );
}
