import { PageHeaderSkeleton, PageSkeleton } from "@/components/app/skeletons";
import { CountsWindowSkeleton, MortalityPanelSkeleton } from "@/features/counts/counts-skeletons";

/** /counts/mortality: header, the window control, then the SAME panel its UrlSuspense shows (KPIs, monthly chart, rate cards). */
export default function Loading() {
  return (
    <PageSkeleton gap={3} root="">
      <PageHeaderSkeleton />
      <CountsWindowSkeleton />
      <MortalityPanelSkeleton />
    </PageSkeleton>
  );
}
