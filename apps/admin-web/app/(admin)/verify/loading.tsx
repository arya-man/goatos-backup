import { ChipRowSkeleton, FilterCardSkeleton, PageHeaderSkeleton, PageSkeleton, StackSkeleton, TableSkeleton } from "@/components/app/skeletons";
import { QUEUE_LIMIT } from "@/features/verification-review/verification-layout";

/** /verify: header + panel buttons, then the one verification board card (module chips, date / pen filters, status chips, table, pager). */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton actions={3} />
      <TableSkeleton
        columns={9}
        rows={QUEUE_LIMIT}
        toolbar={
          <StackSkeleton spacing={2}>
            <ChipRowSkeleton count={5} />
            <FilterCardSkeleton inCard fields={[240, 180]} actions={2} small />
            <ChipRowSkeleton count={4} />
          </StackSkeleton>
        }
      />
    </PageSkeleton>
  );
}
