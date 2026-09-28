import Box from "@mui/material/Box";
import {
  CB_DRIVE_FIELD_MIN,
  CB_KPI_CAPTION_LINES,
  CB_KPI_KEYS,
  CB_KPI_SIZE,
  CB_MATRIX,
  CB_STATUS_FIELD_MIN,
  CB_VACCINE_FIELD_MIN,
  FULL_SCHEDULE,
  INVENTORY,
  MATRIX_ROWS_PER_PAGE,
  SHED_BOARD_TOOLBAR,
} from "./command-board-layout";
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
          + operator-day + status selects in one row (a column below sm). */}
      <ControlsCardSkeleton
        header
        toolbar={
          <>
            <Box sx={{ px: 3, pb: 3 }}>
              <FilterCardSkeleton bare fields={[CB_VACCINE_FIELD_MIN, CB_DRIVE_FIELD_MIN, CB_STATUS_FIELD_MIN]} />
            </Box>
          </>
        }
      />
      {/* Captions are the backend's fixed explanations: two lines on the first row at md:3, three on the second. */}
      <KpiRowSkeleton count={CB_KPI_KEYS.length} size={CB_KPI_SIZE} shapes={CB_KPI_KEYS.map((_, i) => ({ hint: true, hintLines: CB_KPI_CAPTION_LINES[i] }))} />
      <OptionalSkeleton>
        <TableSkeleton columns={CB_MATRIX.penVaccineColumns} rows={MATRIX_ROWS_PER_PAGE} subheader headerAction={<ChipSkeleton width={CB_MATRIX.infoTipSize} height={CB_MATRIX.infoTipSize} />} toolbar={<LegendSkeleton count={CB_MATRIX.penVaccineLegend} />} />
      </OptionalSkeleton>
      <TableSkeleton columns={CB_MATRIX.pendingColumns} rows={MATRIX_ROWS_PER_PAGE} headerAction={<ChipSkeleton width={CB_MATRIX.infoTipSize} height={CB_MATRIX.infoTipSize} />} />
      <TableSkeleton columns={CB_MATRIX.shedDoseColumns} rows={MATRIX_ROWS_PER_PAGE} headerAction={<ChipSkeleton width={CB_MATRIX.infoTipSize} height={CB_MATRIX.infoTipSize} />} toolbar={<LegendSkeleton count={CB_MATRIX.shedDoseLegend} />} />
      <TableSkeleton columns={CB_MATRIX.cohortColumns} rows={MATRIX_ROWS_PER_PAGE} tabs={<TabsSkeleton count={CB_MATRIX.cohortFarmTabs} counts />} />
    </StackSkeleton>
  );
}

/** The matrix legend row (template Labels) under a matrix card header. */
function LegendSkeleton({ count }: { count: number }) {
  return (
    <Box sx={{ px: 3, pb: 2 }}>
      <ChipRowSkeleton count={count} height={CB_MATRIX.legendChipHeight} />
    </Box>
  );
}

/** Inventory progress: four metric tiles over the per-vaccine table. */
export function VaccinationInventorySkeleton({ id }: { id?: string }) {
  return <TableSkeleton id={id} columns={INVENTORY.columns} rows={INVENTORY.rows} pager={false} toolbar={<StatStripSkeleton count={INVENTORY.tiles} card={false} />} />;
}

/**
 * Pen board: search, the status and capacity pill strips, the pen table (leading checkbox + the
 * contract's visible columns + the row-action column), pager. `columns` counts all of them.
 */
export function VaccinationShedBoardSkeleton({ columns = SHED_BOARD_TOOLBAR.columns }: { columns?: number }) {
  return (
    <TableSkeleton
      columns={columns}
      rows={SHED_BOARD_PAGE_SIZE}
      toolbar={
        <StackSkeleton spacing={1.5}>
          <FilterCardSkeleton inCard fields={[...SHED_BOARD_TOOLBAR.fields]} small />
          <ChipRowSkeleton count={SHED_BOARD_TOOLBAR.statusPills} />
          <ChipRowSkeleton count={SHED_BOARD_TOOLBAR.capacityPills} />
        </StackSkeleton>
      }
    />
  );
}

/** Full schedule: the month chips over the operator-day table (8 schedule columns). */
export function VaccinationFullScheduleSkeleton() {
  return <TableSkeleton columns={FULL_SCHEDULE.columns} rows={FULL_SCHEDULE.rows} pager={false} headerAction toolbar={<ChipRowSkeleton count={FULL_SCHEDULE.monthChips} />} />;
}
