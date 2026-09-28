import { OrderToolbarSkeleton, PageHeaderSkeleton, PageSkeleton, StackSkeleton, ToolbarCardSkeleton } from "@/components/app/skeletons";

/**
 * /ceo-ai-admin, block for block with CeoAiAdminTraceViewer (TR3-P0-1): header (three crumbs, no
 * actions), then the lookup card: CardHeader (h6 title, the long body2 subheader that runs 5 lines on
 * a phone and 2 from md), then the OrderTableToolbar form (the large "Look up history" trailing
 * button, then the Reference search; a right-aligned column below md). The trace cards appear only
 * after a lookup. (Not in the shell registry: the ceo-ai boundary keeps click-nav on the generic skeleton.)
 */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton crumbLink={false} titleWidth={180} crumbWidths={[44, 150]} />
      <StackSkeleton>
        <ToolbarCardSkeleton subheaderLines={{ xs: 5, md: 2 }} toolbar={<OrderToolbarSkeleton trailing={[172]} trailingTall />} />
      </StackSkeleton>
    </PageSkeleton>
  );
}
