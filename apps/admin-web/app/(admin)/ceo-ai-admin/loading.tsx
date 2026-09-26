import { DetailCardSkeleton, PageHeaderSkeleton, PageSkeleton, StackSkeleton } from "@/components/app/skeletons";

/** /ceo-ai-admin: header, then the lookup card (the trace cards appear only after a lookup). */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton />
      <StackSkeleton>
        <DetailCardSkeleton rows={1} />
      </StackSkeleton>
    </PageSkeleton>
  );
}
