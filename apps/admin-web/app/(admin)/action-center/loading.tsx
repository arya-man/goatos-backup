import { PageHeaderSkeleton, PageSkeleton, StackSkeleton, TabsSkeleton } from "@/components/app/skeletons";
import { VIEW_SKELETON } from "@/features/process-integrity/action-center-skeletons";

/** /action-center: header, board / verify tabs, then the SAME board panel shape the page streams
 *  behind (quick tiles, toolbar, kanban lanes, pager). guard: action-center-loading-mirrors-page */
export default function Loading() {
  return (
    <PageSkeleton root="page-root">
      <StackSkeleton spacing={3}>
        <PageHeaderSkeleton />
        <TabsSkeleton count={2} />
        {VIEW_SKELETON[""]}
      </StackSkeleton>
    </PageSkeleton>
  );
}
