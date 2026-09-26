import { PageHeaderSkeleton, PageSkeleton, StackSkeleton, StatStripSkeleton, TableSkeleton } from "@/components/app/skeletons";

/** /vaccination/plan: header + the version action, then the live plan card (key tiles, vaccine table). */
export default function Loading() {
  return (
    <PageSkeleton root="kit-enter vplan" gap={2}>
      <PageHeaderSkeleton actions={1} />
      <StackSkeleton spacing={0}>
        <TableSkeleton columns={4} rows={6} toolbar={<StatStripSkeleton count={4} card={false} />} />
      </StackSkeleton>
    </PageSkeleton>
  );
}
