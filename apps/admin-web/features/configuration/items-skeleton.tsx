import { FilterCardSkeleton, TableSkeleton, TabsSkeleton } from "@/components/app/skeletons";
import { ITEMS_COLUMNS, ITEMS_STATUS_TABS, ITEMS_TOOLBAR_FIELDS } from "./items-layout";

/** The register card twin (template user list): CardHeader, status tabs, toolbar + ⋮, table, pager. */
export function ItemsRegisterSkeleton({ rows }: { rows: number }) {
  return (
    <TableSkeleton
      columns={ITEMS_COLUMNS}
      rows={rows}
      tabs={<TabsSkeleton count={ITEMS_STATUS_TABS} counts />}
      toolbar={<FilterCardSkeleton inCard fields={ITEMS_TOOLBAR_FIELDS} actionWidths={[36]} />}
    />
  );
}
