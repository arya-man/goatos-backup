import { PageHeaderSkeleton, PageSkeleton } from "@/components/app/skeletons";
import { FeedAnalyticsOverviewSkeleton, FeedAnalyticsStripSkeleton } from "@/features/feed/feed-analytics-skeletons";

/**
 * /feed/analytics, block for block with the page (Overview, the entry tab): header + Export (plain
 * crumbs), the tab strip over the window chips + Consumption view toggle, then the Overview panel
 * (the page's own fallback): KPI deck 3 + 2, the consumption and spend chart cards.
 */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton crumbLink={false} titleWidth={180} actionWidths={[96]} />
      <FeedAnalyticsStripSkeleton />
      <FeedAnalyticsOverviewSkeleton />
    </PageSkeleton>
  );
}
