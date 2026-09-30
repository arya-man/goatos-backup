import { PageHeaderSkeleton, PageSkeleton, TabsSkeleton } from "@/components/app/skeletons";
import { HERD_SIGNALS_TABS } from "@/features/herd-signals/params";
import { HERD_SIGNALS_HEADER_ACTION_WIDTHS, HERD_SIGNALS_PAGE_GAP } from "@/features/herd-signals/herd-signals-layout";
import { HerdSignalsFilterSkeleton, HerdSignalsLivePanelSkeleton } from "@/features/herd-signals/herd-signals-skeletons";

/**
 * /herd-signals (route loading AND the page's Suspense fallback): header (live stream status) with
 * the six signal tabs, then the Live Monitor's filter card and the SAME panel skeleton its
 * UrlSuspense shows on a filter / KPI change.
 */
export default function Loading() {
  return (
    <PageSkeleton root="" gap={HERD_SIGNALS_PAGE_GAP}>
      <PageHeaderSkeleton crumbLink={false} actionWidths={HERD_SIGNALS_HEADER_ACTION_WIDTHS} tabs={<TabsSkeleton count={HERD_SIGNALS_TABS.length} counts />} />
      <HerdSignalsFilterSkeleton />
      <HerdSignalsLivePanelSkeleton />
    </PageSkeleton>
  );
}
