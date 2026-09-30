import { ChipSkeleton, FilterCardSkeleton, KpiRowSkeleton, PageHeaderSkeleton, PageSkeleton, StackSkeleton, TableSkeleton } from "@/components/app/skeletons";
import { ROUTINES_CARD_HEADER_SX, ROUTINES_TOOLBAR_FIELDS, ROUTINE_TILE_SIZE, TASKS_TOOLBAR_FIELDS } from "@/features/pen-routines/routines-layout";

/**
 * /routines, block for block with RoutinesPage: header + New routine (no crumb row), then the page
 * Stack: the five Today tiles, the routines card (header, park / status / role + search + ⋮, rows)
 * and the Today tasks card (header with the park subheader and count, routine / state / assignee,
 * the day pair, search + ⋮, the always-on day chip strip, rows).
 */
export default function Loading() {
  return (
    <PageSkeleton root="page-root">
      <PageHeaderSkeleton crumbs={false} titleWidth={140} actionWidths={[129]} />
      <StackSkeleton>
        <KpiRowSkeleton count={5} size={ROUTINE_TILE_SIZE} />
        <TableSkeleton columns={7} rows={4} pager={false} headerSx={ROUTINES_CARD_HEADER_SX} toolbar={<FilterCardSkeleton inCard fields={ROUTINES_TOOLBAR_FIELDS} actionWidths={[36]} />} />
        <TableSkeleton
          columns={6}
          rows={4}
          pager={false}
          subheader
          headerSx={ROUTINES_CARD_HEADER_SX}
          headerAction={<ChipSkeleton width={24} height={24} />}
          toolbar={<FilterCardSkeleton inCard summary fields={TASKS_TOOLBAR_FIELDS} actionWidths={[36]} />}
        />
      </StackSkeleton>
    </PageSkeleton>
  );
}
