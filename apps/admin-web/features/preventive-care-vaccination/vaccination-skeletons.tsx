import Box from "@mui/material/Box";
import { FilterCardSkeleton, KpiRowSkeleton, OptionalSkeleton, StackSkeleton, StatStripSkeleton, TableSkeleton, TabsSkeleton, ChipRowSkeleton } from "@/components/app/skeletons";

// Panel fallbacks for /vaccination's streamed sections, composed ONLY from the shared skeleton blocks.
// The route's loading.tsx stacks the same three, so a hard load and a panel stream paint one shape.

/**
 * Command board (TR1-#21 template table cards): the vaccine / operator-day filter card with status
 * chips, the eight-card deck, then one template table card per matrix — Pen × Vaccine (legend, only
 * with data), Pending by pen, Vaccine × Pen status (legend), the farm-tabbed cohort matrix — each with
 * its pager (MATRIX_ROWS_PER_PAGE rows).
 */
export function VaccinationCommandBoardSkeleton() {
  return (
    <StackSkeleton spacing={3}>
      <FilterCardSkeleton fields={[190, 320, "chip", "chip", "chip", "chip", "chip"]} />
      <KpiRowSkeleton count={8} icon />
      <OptionalSkeleton>
        <TableSkeleton columns={7} rows={10} subheader headerAction toolbar={<LegendSkeleton count={5} />} />
      </OptionalSkeleton>
      <TableSkeleton columns={3} rows={10} headerAction />
      <TableSkeleton columns={8} rows={10} headerAction toolbar={<LegendSkeleton count={6} />} />
      <TableSkeleton columns={8} rows={10} tabs={<TabsSkeleton count={2} counts />} />
    </StackSkeleton>
  );
}

/** The matrix legend row (template Labels) under a matrix card header. */
function LegendSkeleton({ count }: { count: number }) {
  return (
    <Box sx={{ px: 3, pb: 2 }}>
      <ChipRowSkeleton count={count} />
    </Box>
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
