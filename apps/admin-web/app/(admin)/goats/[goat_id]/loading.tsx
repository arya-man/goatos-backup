import { BlockSkeleton, DetailCardSkeleton, PageHeaderSkeleton, PageSkeleton, TableSkeleton } from "@/components/app/skeletons";

/**
 * /goats/[goat_id]: the passport root (header + status tags, the cover card with its four tabs, the
 * summary card with two definition columns) and, under it, the vaccination passport root.
 */
export default function Loading() {
  return (
    <>
      <PageSkeleton className="gp-page">
        <PageHeaderSkeleton actionWidths={[72, 72]} />
        <BlockSkeleton height={290} />
        <DetailCardSkeleton rows={5} columns={2} />
      </PageSkeleton>
      <PageSkeleton>
        <TableSkeleton columns={5} rows={4} pager={false} />
      </PageSkeleton>
    </>
  );
}
