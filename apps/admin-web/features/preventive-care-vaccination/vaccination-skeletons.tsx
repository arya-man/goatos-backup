import { FilterCardSkeleton, KpiRowSkeleton, StackSkeleton, StatStripSkeleton, TableSkeleton, TabsSkeleton, ChipRowSkeleton } from "@/components/app/skeletons";

// Panel fallbacks for /vaccination's streamed sections, composed ONLY from the shared skeleton blocks.
// The route's loading.tsx stacks the same three, so a hard load and a panel stream paint one shape.

/** Command board: the vaccine / operator-day filter card with status chips, the eight-card deck, the matrix card. */
export function VaccinationCommandBoardSkeleton() {
  return (
    <StackSkeleton spacing={2}>
      <FilterCardSkeleton fields={[200, 200, "chip", "chip", "chip", "chip", "chip"]} />
      <KpiRowSkeleton count={8} />
      <TableSkeleton columns={8} rows={8} pager={false} headerAction={<TabsSkeleton count={3} variant="pill" />} />
    </StackSkeleton>
  );
}

/** Inventory progress: four metric tiles over the per-vaccine table. */
export function VaccinationInventorySkeleton({ id }: { id?: string }) {
  return <TableSkeleton id={id} columns={5} rows={4} pager={false} toolbar={<StatStripSkeleton count={4} card={false} />} />;
}

/**
 * Pen board: search, the status and capacity pill strips, the pen table (leading checkbox + the
 * contract's visible columns + the row-action column), pager. `columns` counts all of them.
 */
export function VaccinationShedBoardSkeleton({ columns = 10 }: { columns?: number }) {
  return (
    <TableSkeleton
      columns={columns}
      rows={10}
      toolbar={
        <StackSkeleton spacing={1.5}>
          <FilterCardSkeleton inCard fields={[280, 72]} small />
          <ChipRowSkeleton count={7} />
          <ChipRowSkeleton count={4} />
        </StackSkeleton>
      }
    />
  );
}

/** Full schedule: the month chips over the operator-day table (8 schedule columns). */
export function VaccinationFullScheduleSkeleton() {
  return <TableSkeleton columns={8} rows={5} pager={false} headerAction toolbar={<ChipRowSkeleton count={12} />} />;
}
