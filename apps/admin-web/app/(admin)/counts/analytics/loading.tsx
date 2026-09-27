import { PageHeaderSkeleton, PageSkeleton } from "@/components/app/skeletons";
import { COUNTS_HEADER } from "@/features/counts/counts-layout";
import { CountsWindowSkeleton, HerdAnalyticsPanelSkeleton } from "@/features/counts/counts-skeletons";

/**
 * /counts/analytics: the page's Stack spacing 3 — header + Export ("Herd Analytics" is short: Export
 * sits beside it on a phone), the window control, then the SAME panel its UrlSuspense shows.
 */
export default function Loading() {
  return (
    <PageSkeleton gap={3} root="">
      <PageHeaderSkeleton crumbLink={false} titleWidth={COUNTS_HEADER.analyticsTitleWidth} actionWidths={[COUNTS_HEADER.exportWidth]} />
      <CountsWindowSkeleton />
      <HerdAnalyticsPanelSkeleton />
    </PageSkeleton>
  );
}
