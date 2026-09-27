import { PageHeaderSkeleton, PageSkeleton } from "@/components/app/skeletons";
import { MilkPreparationPanelSkeleton, MilkPreparationTableSkeleton } from "@/features/counts/milk-preparation-skeletons";

/**
 * /counts/milk-preparation: the page's Stack spacing 3 — header + Export (a short title: on a phone
 * Export sits beside it), the SAME KPI + farm-state panel its UrlSuspense shows, the worklist card.
 */
export default function Loading() {
  return (
    <PageSkeleton gap={3} root="">
      <PageHeaderSkeleton titleWidth={150} actionWidths={[89]} />
      <MilkPreparationPanelSkeleton />
      <MilkPreparationTableSkeleton />
    </PageSkeleton>
  );
}
