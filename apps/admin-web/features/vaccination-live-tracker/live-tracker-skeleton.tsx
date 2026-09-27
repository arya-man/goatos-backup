import { GridSkeleton, KpiRowSkeleton, ListCardSkeleton, OptionalSkeleton, StackSkeleton, TableSkeleton } from "@/components/app/skeletons";

/**
 * The drive-day board body (below header + filter card): the KpiWidget row, then operators / pens /
 * combo table Cards (lg 8) beside the activity / attention / verification rail (lg 4). Used by the
 * route loading.tsx and the board's URL-keyed Suspense fallback so both match the loaded layout.
 */
export function LiveTrackerBodySkeleton() {
  return (
    <StackSkeleton spacing={3}>
      <OptionalSkeleton>
        <KpiRowSkeleton count={6} icon hint />
      </OptionalSkeleton>
      <GridSkeleton
        items={[
          {
            size: { xs: 12, lg: 8 },
            node: (
              <StackSkeleton spacing={3}>
                <TableSkeleton columns={10} rows={5} subheader />
                <TableSkeleton columns={10} rows={10} headerAction />
                <TableSkeleton columns={4} rows={3} subheader pager={false} />
              </StackSkeleton>
            ),
          },
          {
            size: { xs: 12, lg: 4 },
            node: (
              <StackSkeleton spacing={3}>
                <ListCardSkeleton rows={6} avatar={false} />
                <ListCardSkeleton rows={2} avatar={false} />
                <ListCardSkeleton rows={3} avatar={false} />
              </StackSkeleton>
            ),
          },
        ]}
      />
    </StackSkeleton>
  );
}
