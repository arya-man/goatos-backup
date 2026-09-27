import { PageHeaderSkeleton, PageSkeleton, StackSkeleton } from "@/components/app/skeletons";
import {
  VaccinationCommandBoardSkeleton,
  VaccinationInventorySkeleton,
  VaccinationShedBoardSkeleton,
} from "@/features/preventive-care-vaccination/vaccination-skeletons";

/**
 * /vaccination: header + Full schedule, then the operations column with the SAME three panel
 * skeletons the page streams its sections behind (command board, inventory progress, pen board).
 */
export default function Loading() {
  return (
    <PageSkeleton gap={3}>
      <PageHeaderSkeleton crumbLink={false} actionWidths={[140]} />
      <StackSkeleton spacing={2}>
        <VaccinationCommandBoardSkeleton />
        <VaccinationInventorySkeleton />
        <VaccinationShedBoardSkeleton />
      </StackSkeleton>
    </PageSkeleton>
  );
}
