import { GridSkeleton, NavRailSkeleton, PageHeaderSkeleton, PageSkeleton } from "@/components/app/skeletons";
import { ITEMS_RAIL_GROUPS, ITEMS_RAIL_SIZE, ITEMS_REGISTER_SIZE } from "@/features/configuration/items-layout";
import { ItemsRegisterSkeleton } from "@/features/configuration/items-skeleton";

const DEFAULT_ROWS = 25;

/**
 * /configuration/items (template mail layout + user list): header with the "Add …" action (its crumb
 * only repeats the title), then the Grid: the register rail (overline groups of nav items; rows of
 * items on a phone) and the register card (header, status tabs, toolbar, table, pager).
 */
export default function Loading() {
  return (
    <PageSkeleton root="page-root">
      <PageHeaderSkeleton crumbs={false} titleWidth={200} actionWidths={[140]} />
      <GridSkeleton
        items={[
          { size: ITEMS_RAIL_SIZE, node: <NavRailSkeleton groups={ITEMS_RAIL_GROUPS} /> },
          { size: ITEMS_REGISTER_SIZE, node: <ItemsRegisterSkeleton rows={DEFAULT_ROWS} /> },
        ]}
      />
    </PageSkeleton>
  );
}
