import { LiveTrackerPageSkeleton } from "@/features/vaccination-live-tracker/live-tracker-skeleton";

/**
 * /vaccination/live-tracker (route loading AND the page's Suspense fallback): header, the AppWelcome
 * drive-day row (TR1-#31), the filter card, then the SAME board body the board's UrlSuspense shows.
 */
export default function Loading() {
  return <LiveTrackerPageSkeleton />;
}
