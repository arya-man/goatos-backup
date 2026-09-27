import { FilterCardSkeleton, PageHeaderSkeleton, PageSkeleton } from "@/components/app/skeletons";
import { LiveTrackerBodySkeleton } from "@/features/vaccination-live-tracker/live-tracker-skeleton";

/** /vaccination/live-tracker (route loading AND the page's Suspense fallback): header + live controls, the filter card, then the board body. */
export default function Loading() {
  return (
    <PageSkeleton root="lt-page">
      <PageHeaderSkeleton actionWidths={[96, 88, 140, 150, 160]} />
      <FilterCardSkeleton fields={[200, 200, 200, 200, 200]} />
      <LiveTrackerBodySkeleton />
    </PageSkeleton>
  );
}
