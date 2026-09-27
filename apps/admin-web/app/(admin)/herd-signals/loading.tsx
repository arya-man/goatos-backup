import { PageHeaderSkeleton, PageSkeleton, TabsSkeleton } from "@/components/app/skeletons";
import { HERD_SIGNALS_TABS } from "@/features/herd-signals/params";
import { HerdSignalsFilterSkeleton, HerdSignalsLivePanelSkeleton } from "@/features/herd-signals/herd-signals-skeletons";

/**
 * /herd-signals (route loading AND the page's Suspense fallback): header (live stream status) with
 * the six signal tabs, then the Live Monitor's filter card and the SAME panel skeleton its
 * UrlSuspense shows on a filter / KPI change.
 */
export default function Loading() {
  return (
    <PageSkeleton root="herd-signals-page">
      <PageHeaderSkeleton crumbLink={false} actionWidths={[150]} tabs={<TabsSkeleton count={HERD_SIGNALS_TABS.length} counts />} />
      <HerdSignalsFilterSkeleton />
      <HerdSignalsLivePanelSkeleton />
    </PageSkeleton>
  );
}
