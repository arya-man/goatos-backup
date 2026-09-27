"use client";

import { OrderToolbarSkeleton, PageHeaderSkeleton, PageSkeleton, TableSkeleton, TabsSkeleton } from "@/components/app/skeletons";
import { PAGE_SIZE, VENDOR_FILTER_KEYS } from "./vendor-layout";

/** The vendor rows (the register's UrlSuspense fallback): head + one page of rows + the footer. */
export function VendorRowsSkeleton({ columns = 6, rows = PAGE_SIZE }: { columns?: number; rows?: number }) {
  return <TableSkeleton bare header={false} columns={columns} rows={rows} />;
}

/**
 * Loading twin of VendorBoardPage (procurement + sales vendors): header + Add (the parent crumb is
 * plain text), then the register card — status tabs, the order toolbar (the non-status selects,
 * Apply, search, ⋮), the rows and footer.
 */
export function VendorBoardSkeleton() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton crumbLink={false} titleWidth={110} actionWidths={[128]} />
      <TableSkeleton
        columns={6}
        rows={PAGE_SIZE}
        header={false}
        tabs={<TabsSkeleton count={1} counts />}
        toolbar={<OrderToolbarSkeleton filters={VENDOR_FILTER_KEYS.filter((key) => key !== "status").length} trailing={[120]} menu />}
      />
    </PageSkeleton>
  );
}
