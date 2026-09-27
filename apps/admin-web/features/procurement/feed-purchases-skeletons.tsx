"use client";

import Box from "@mui/material/Box";
import { OrderToolbarSkeleton, PageHeaderSkeleton, PageSkeleton, StatStripSkeleton, TableSkeleton, TabsSkeleton } from "@/components/app/skeletons";
import { DEFAULT_LIMIT } from "./feed-purchases-layout";

/**
 * The aggregate strip (two InvoiceAnalytic cells in a Card with `mb: { xs: 3, md: 5 }`), layout-
 * transparent like the page's UrlPanel so the card's own margin is not reset as a direct
 * `.wrap > .screen > *` child. Also the strip's UrlSuspense fallback.
 */
export function FeedPurchasesStripSkeleton() {
  return (
    <Box sx={{ display: "contents" }}>
      <Box sx={{ mb: { xs: 3, md: 5 } }}>
        <StatStripSkeleton count={2} />
      </Box>
    </Box>
  );
}

/** The ledger rows (their UrlSuspense fallback): head, one page of rows, the footer. */
export function FeedPurchasesRowsSkeleton({ columns = 11, rows = DEFAULT_LIMIT }: { columns?: number; rows?: number }) {
  return <TableSkeleton bare header={false} columns={columns} rows={rows} />;
}

/** /procurement/feed-purchases: header + Record, the strip, the ledger card (delivery tabs, farm select + ⋮ toolbar, rows). */
export function FeedPurchasesSkeleton() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton crumbLink={false} actions={1} />
      <FeedPurchasesStripSkeleton />
      <TableSkeleton columns={11} rows={DEFAULT_LIMIT} header={false} tabs={<TabsSkeleton count={3} />} toolbar={<OrderToolbarSkeleton filters={1} search={false} menu />} />
    </PageSkeleton>
  );
}
