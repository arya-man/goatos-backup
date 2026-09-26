import { BlockSkeleton, DetailCardSkeleton, GridSkeleton, PageHeaderSkeleton, PageSkeleton, TableSkeleton } from "@/components/app/skeletons";

/**
 * /goats/[goat_id] (template user profile): header + status Labels, the 290px profile cover with its
 * four tabs, then the summary column (order-details rows) beside the vaccination passport card.
 */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton actionWidths={[72, 72]} />
      <BlockSkeleton height={290} />
      <GridSkeleton
        items={[
          { size: { xs: 12, md: 5, lg: 4 }, node: <DetailCardSkeleton rows={10} /> },
          { size: { xs: 12, md: 7, lg: 8 }, node: <TableSkeleton columns={5} rows={4} pager={false} /> },
        ]}
      />
    </PageSkeleton>
  );
}
