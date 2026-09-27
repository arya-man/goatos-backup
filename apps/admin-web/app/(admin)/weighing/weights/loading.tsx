import { WEIGHTS_HEADER } from "@/features/weighing/weights-analytics-layout";
import { PageHeaderSkeleton, PageSkeleton } from "@/components/app/skeletons";
import { WeightsFilterSkeleton, WeightsKidsPanelSkeleton } from "@/features/weighing/weights-skeletons";

/**
 * /weighing/weights: the page's Stack spacing 3 — header + Download (a short title: on a phone the
 * button sits beside it), the WorklistFilters card, then the SAME panel skeleton the page's
 * UrlSuspense shows on a filter change.
 */
export default function Loading() {
  return (
    <PageSkeleton gap={3} root="">
      <PageHeaderSkeleton crumbLink={false} titleWidth={WEIGHTS_HEADER.kidsTitleWidth} actionWidths={[WEIGHTS_HEADER.downloadWidth]} />
      <WeightsFilterSkeleton />
      <WeightsKidsPanelSkeleton />
    </PageSkeleton>
  );
}
