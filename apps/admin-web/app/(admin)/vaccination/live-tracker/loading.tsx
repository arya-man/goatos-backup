import { BlockSkeleton, FilterCardSkeleton, PageHeaderSkeleton, PageSkeleton } from "@/components/app/skeletons";
import { LiveTrackerBodySkeleton } from "@/features/vaccination-live-tracker/live-tracker-skeleton";

/**
 * /vaccination/live-tracker (route loading AND the page's Suspense fallback): header (Full Schedule +
 * Command Board), the AppWelcome drive-day row (TR1-#31), the filter card, then the board body.
 */
export default function Loading() {
  return (
    <PageSkeleton root="lt-page">
      <PageHeaderSkeleton actionWidths={[150, 160]} />
      <BlockSkeleton height={{ xs: 260, md: 200 }} />
      <FilterCardSkeleton fields={[200, 200, 200, 200, 200]} />
      <LiveTrackerBodySkeleton />
    </PageSkeleton>
  );
}
