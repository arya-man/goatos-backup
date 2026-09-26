import { DetailCardSkeleton, FilterCardSkeleton, GridSkeleton, PageHeaderSkeleton, PageSkeleton, TableSkeleton } from "@/components/app/skeletons";
import { DRIVE_ROSTER_HEADER_KEYS, DRIVE_ROSTER_PAGE_SIZE } from "@/features/calendar/calendar-drive-layout";

/** /calendar/drive/[eventId]: back header, then the 8/4 hero + meta cards over the full-width roster card. */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton />
      <GridSkeleton
        items={[
          { size: { xs: 12, md: 8 }, node: <DetailCardSkeleton rows={3} height={236} /> },
          { size: { xs: 12, md: 4 }, node: <DetailCardSkeleton rows={3} height={236} /> },
          { size: 12, node: <TableSkeleton columns={DRIVE_ROSTER_HEADER_KEYS.length} rows={DRIVE_ROSTER_PAGE_SIZE} headerAction toolbar={<FilterCardSkeleton inCard fields={["search", 96]} />} /> },
        ]}
      />
    </PageSkeleton>
  );
}
