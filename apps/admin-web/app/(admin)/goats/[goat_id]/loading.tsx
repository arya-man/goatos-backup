import { BlockSkeleton, GridSkeleton, PageHeaderSkeleton, PageSkeleton, StackSkeleton, TableSkeleton, TabsSkeleton } from "@/components/app/skeletons";

/**
 * /goats/[goat_id] (template user account): header, the four icon tabs, then the summary tab's
 * profile Card (md 4) beside the read-only form Card and the vaccination passport card (md 8).
 */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton />
      <TabsSkeleton count={4} sx={{ mb: { xs: 3, md: 5 } }} />
      <GridSkeleton
        items={[
          { size: { xs: 12, md: 4 }, node: <BlockSkeleton height={420} /> },
          {
            size: { xs: 12, md: 8 },
            node: (
              <StackSkeleton spacing={3}>
                <BlockSkeleton height={400} />
                <TableSkeleton columns={5} rows={4} pager={false} />
              </StackSkeleton>
            ),
          },
        ]}
      />
    </PageSkeleton>
  );
}
