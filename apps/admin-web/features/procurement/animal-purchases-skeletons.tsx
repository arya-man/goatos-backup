"use client";

import { CardGridSkeleton, ControlsCardSkeleton, KpiRowSkeleton, OrderToolbarSkeleton, PageHeaderSkeleton, PageSkeleton, TableSkeleton, TabsSkeleton } from "@/components/app/skeletons";
import {
  ANIMAL_APPLY_TWIN_WIDTH,
  ANIMAL_CARD_COLUMNS,
  ANIMAL_DECISION_KEYS,
  ANIMAL_KPI_COUNT,
  ANIMAL_KPI_SIZE,
  ANIMAL_LOAD_COLUMNS,
  ANIMAL_LOADS_DEFAULT_LIMIT,
  ANIMAL_LOADS_SKELETON_ROWS,
  ANIMAL_LOAD_STRIP_TABS,
  ANIMALS_PAGE_SIZE,
} from "./animal-purchases-layout";

/** The loads table body (its UrlSuspense fallback): head, one page of rows, the pager. */
export function AnimalLoadRowsSkeleton({ rows = ANIMAL_LOADS_DEFAULT_LIMIT }: { rows?: number }) {
  return <TableSkeleton bare header={false} columns={ANIMAL_LOAD_COLUMNS.length} rows={rows} />;
}

/** The animal cards (their UrlSuspense fallback): the JobList grid, one page. */
export function AnimalCardsSkeleton() {
  return <CardGridSkeleton count={ANIMALS_PAGE_SIZE} columns={ANIMAL_CARD_COLUMNS} />;
}

/**
 * /procurement/animal-purchases: header (linked parent crumb), the four KPI cards, the loads card
 * (title + the All loads pill strip, rows, pager), the animals controls card (title, decision tabs,
 * load select + recorded range + Apply), the animal cards.
 */
export function AnimalPurchasesSkeleton() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton />
      <KpiRowSkeleton count={ANIMAL_KPI_COUNT} size={ANIMAL_KPI_SIZE} />
      <TableSkeleton columns={ANIMAL_LOAD_COLUMNS.length} rows={ANIMAL_LOADS_SKELETON_ROWS} headerAction={<TabsSkeleton count={ANIMAL_LOAD_STRIP_TABS} variant="pill" links />} />
      <ControlsCardSkeleton header tabs={<TabsSkeleton count={ANIMAL_DECISION_KEYS.length} counts />} toolbar={<OrderToolbarSkeleton filters={1} trailing={[ANIMAL_APPLY_TWIN_WIDTH]} />} />
      <AnimalCardsSkeleton />
    </PageSkeleton>
  );
}
