import { BlockSkeleton, PageHeaderSkeleton, PageSkeleton } from "@/components/app/skeletons";
import { CALENDAR_CARD_HEIGHT } from "@/features/calendar/calendar-layout";

/**
 * /calendar (template calendar view): the heading (its only crumb is the title, so none shows; no
 * header action), then ONE card: the calendar toolbar and the month grid. The filter result chips
 * render only when a filter is applied (none on entry).
 */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton crumbs={false} titleWidth={120} />
      <BlockSkeleton height={CALENDAR_CARD_HEIGHT} />
    </PageSkeleton>
  );
}
