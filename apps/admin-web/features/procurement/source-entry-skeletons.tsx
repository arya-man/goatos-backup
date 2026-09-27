"use client";

import { OrderToolbarSkeleton, PageHeaderSkeleton, PageSkeleton, TableSkeleton, TabsSkeleton } from "@/components/app/skeletons";
import { SOURCE_FILTER_BUTTON_TWIN_WIDTH, SOURCE_LOAD_COLUMNS, SOURCE_LOAD_SKELETON_ROWS, SOURCE_LOAD_STATUS_KEYS } from "./source-entry-layout";


/** The loads table rows (their UrlSuspense fallback). */
export function SourceLoadRowsSkeleton({ columns = SOURCE_LOAD_COLUMNS.length }: { columns?: number }) {
  return <TableSkeleton bare header={false} columns={columns} rows={SOURCE_LOAD_SKELETON_ROWS} />;
}

/**
 * /procurement/source-entry: header + New load (plain crumb), then the loads card — status tabs,
 * the order toolbar (search + the Filters button), the rows.
 */
export function SourceEntrySkeleton() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton crumbLink={false} actions={1} />
      <TableSkeleton
        columns={SOURCE_LOAD_COLUMNS.length}
        rows={SOURCE_LOAD_SKELETON_ROWS}
        header={false}
       
        tabs={<TabsSkeleton count={1 + SOURCE_LOAD_STATUS_KEYS.length} />}
        toolbar={<OrderToolbarSkeleton trailing={[SOURCE_FILTER_BUTTON_TWIN_WIDTH]} />}
      />
    </PageSkeleton>
  );
}
