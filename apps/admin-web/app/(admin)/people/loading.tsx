import { FilterCardSkeleton, PageHeaderSkeleton, PageSkeleton, TableSkeleton, TabsSkeleton } from "@/components/app/skeletons";
import { PAGE_SIZE, PEOPLE_COLUMNS } from "@/features/people/people-layout";

/** /people: header, the people tabs, the people list card (search + three selects, pager). */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton crumbLink={false} actionWidths={[132]} />
      <TabsSkeleton count={4} />
      <TableSkeleton columns={PEOPLE_COLUMNS} rows={PAGE_SIZE} headerAction toolbar={<FilterCardSkeleton inCard fields={["search", 160, 160, 160]} actions={1} />} />
    </PageSkeleton>
  );
}
