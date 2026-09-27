import { FilterCardSkeleton, KpiRowSkeleton, PageHeaderSkeleton, PageSkeleton, TableSkeleton } from "@/components/app/skeletons";
import { FEED_DEFAULT_PAGE_SIZE, FEED_KPI_SIZE, FEED_TABLE_COLUMNS, FEED_DIRECTION_FILTER_FIELDS } from "@/features/feed/feed-layout";

/**
 * /feed/direction, block for block with the page: header (plain crumbs), the day's two KpiWidget tiles
 * (shown for every served day), then ONE card: CardHeader with caption, the WorklistFilters card
 * (day + selects; one Filters button on a phone), the sheet and its pager.
 */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton crumbLink={false} titleWidth={180} />
      <KpiRowSkeleton count={2} hint size={FEED_KPI_SIZE} />
      <TableSkeleton columns={FEED_TABLE_COLUMNS} rows={FEED_DEFAULT_PAGE_SIZE} subheader toolbar={<FilterCardSkeleton fold fields={FEED_DIRECTION_FILTER_FIELDS} />} />
    </PageSkeleton>
  );
}
