import Box from "@mui/material/Box";
import { CB_DRIVE_FIELD_MIN, CB_KPI_KEYS, CB_KPI_SIZE, CB_VACCINE_FIELD_MIN, MATRIX_ROWS_PER_PAGE, STATUS_KEYS } from "./command-board-layout";
import { SHED_BOARD_PAGE_SIZE } from "@/features/vaccination-sheds/shed-board-layout";
import { ChipRowSkeleton, ChipSkeleton, ControlsCardSkeleton, FilterCardSkeleton, KpiRowSkeleton, OptionalSkeleton, StackSkeleton, StatStripSkeleton, TableSkeleton, TabsSkeleton } from "@/components/app/skeletons";

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
      {/* "Command Board" CardHeader (mb 2.5) over the filter Stack (px 3, pb 3, spacing 2): the vaccine
          + operator-day selects (column below sm), then the soft status chips (44px tap floor below sm). */}
      <ControlsCardSkeleton
        header
        toolbar={
          <>
            <Box sx={{ px: 3, pb: 2 }}>
              <FilterCardSkeleton bare fields={[CB_VACCINE_FIELD_MIN, CB_DRIVE_FIELD_MIN]} />
            </Box>
            <Box sx={{ px: 3, pb: 3 }}>
              <ChipRowSkeleton count={STATUS_KEYS.length} widths={[92, 150, 124, 120, 100]} height={{ xs: 44, sm: 32 }} />
            </Box>
          </>
        }
      />
      {/* Captions are the backend's fixed explanations: two lines on the first row at md:3, three on the second. */}
      <KpiRowSkeleton count={CB_KPI_KEYS.length} size={CB_KPI_SIZE} shapes={CB_KPI_KEYS.map((_, i) => ({ hint: true, hintLines: { xs: 1, md: i < 4 ? 2 : 3 } }))} />
      <OptionalSkeleton>
        <TableSkeleton columns={7} rows={MATRIX_ROWS_PER_PAGE} subheader headerAction={<ChipSkeleton width={44} height={44} />} toolbar={<LegendSkeleton count={5} />} />
      </OptionalSkeleton>
      <TableSkeleton columns={3} rows={MATRIX_ROWS_PER_PAGE} headerAction={<ChipSkeleton width={44} height={44} />} />
      <TableSkeleton columns={8} rows={MATRIX_ROWS_PER_PAGE} headerAction={<ChipSkeleton width={44} height={44} />} toolbar={<LegendSkeleton count={6} />} />
      <TableSkeleton columns={8} rows={MATRIX_ROWS_PER_PAGE} tabs={<TabsSkeleton count={2} counts />} />
    </StackSkeleton>
  );
}

/** The matrix legend row (template Labels) under a matrix card header. */
function LegendSkeleton({ count }: { count: number }) {
  return (
    <Box sx={{ px: 3, pb: 2 }}>
      <ChipRowSkeleton count={count} height={24} />
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
      rows={SHED_BOARD_PAGE_SIZE}
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
