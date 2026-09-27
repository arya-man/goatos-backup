import { PageHeaderSkeleton, PageSkeleton, TabsSkeleton } from "@/components/app/skeletons";
import { WEIGHTS_HEADER, WEIGHTS_TABS } from "@/features/weighing/weights-analytics-layout";
import { WeightsFilterSkeleton, WeightsGeneralPanelSkeleton } from "@/features/weighing/weights-skeletons";

/**
 * /weighing/analytics: the page's Stack spacing 3 — header + Download with the analytics tabs, the
 * WorklistFilters card, then the SAME General-tab panel skeleton the page's UrlSuspense shows.
 */
export default function Loading() {
  return (
    <PageSkeleton gap={3} root="">
      <PageHeaderSkeleton crumbLink={false} actionWidths={[WEIGHTS_HEADER.downloadWidth]} tabs={<TabsSkeleton count={WEIGHTS_TABS.length} />} />
      <WeightsFilterSkeleton />
      <WeightsGeneralPanelSkeleton />
    </PageSkeleton>
  );
}
