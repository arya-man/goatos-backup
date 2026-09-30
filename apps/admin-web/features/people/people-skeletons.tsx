import { PanelSkeleton } from "@/components/app/panel-skeleton";
import { FormCardSkeleton, PagerSkeleton, StackedRowsSkeleton, TableSkeleton } from "@/components/app/skeletons";

/**
 * The /people Vaccination desk's loading shape, ONE component for the tab-click fallback
 * (people-page UrlSuspense) and the screen's own client loading state, so the three phases of a
 * click (panel skeleton, screen loading, content) keep one shape (J3 P1-2 class; r2
 * `interact|*|fallback-shape`). A multi-park principal with no park chosen lands on the "Choose a
 * park" card (template Card + CardHeader + one select), everyone else on the roster.
 */
export function VaccinationDeskSkeleton({ chooser }: { chooser: boolean }) {
  return chooser ? <FormCardSkeleton subheader wrap controls={[260]} /> : <PanelSkeleton kpis={4} table={10} />;
}

/** Whether the desk opens on the park chooser: no park in the URL and more than one park in scope. */
export function vaccinationDeskOpensOnChooser(initialParkId: string | undefined, parkCount: number): boolean {
  return !initialParkId && parkCount > 1;
}

/**
 * The /people directory rows' twin (status / filter / search / pager clicks; guard url-keyed-panel):
 * the table from sm, and below sm the SAME stacked phone rows the page renders (name + status Label,
 * park · department · designation, clock Label, 44px Access button), plus the pager when the page
 * has one, so a phone pager tap keeps the reader where they were (FIXJ11, J3B N-P1-2: the 6-column
 * table skeleton on a phone dropped the page 1150px; r2 `interact|Pager|pager-jump` / `pager-shape`).
 */
export function PeopleDirectorySkeleton({ rows, pager }: { rows: number; pager: boolean }) {
  return (
    <>
      <TableSkeleton bare header={false} pager={false} columns={6} rows={rows} hideBelow="sm" />
      <StackedRowsSkeleton rows={rows} headLabel lines={1} tailLabel trailing />
      {pager ? <PagerSkeleton /> : null}
    </>
  );
}
