import { ChipRowSkeleton, FilterCardSkeleton, KanbanSkeleton, PageHeaderSkeleton, PageSkeleton } from "@/components/app/skeletons";

/** /work-board: header, the board toolbar (search, people, park, date, module), the legend, the lanes. */
export default function Loading() {
  return (
    <PageSkeleton root="kit-enter wb">
      <PageHeaderSkeleton crumbLink={false} />
      <FilterCardSkeleton inCard fields={["search", 160, 160, 200, 120]} small />
      <ChipRowSkeleton count={4} />
      <KanbanSkeleton lanes={[4, 3, 3, 2]} />
    </PageSkeleton>
  );
}
