"use client";

import Box from "@mui/material/Box";
import { OrderToolbarSkeleton, PageHeaderSkeleton, PageSkeleton, StatStripSkeleton, TableSkeleton, TabsSkeleton } from "@/components/app/skeletons";
import { DEFAULT_LIMIT, FEED_RECORD_BUTTON_TWIN_WIDTH, FEED_DELIVERY_TAB_COUNT, FEED_PURCHASE_COLUMNS, FEED_STRIP_CELLS, FEED_TOOLBAR_FILTERS } from "./feed-purchases-layout";

/**
 * The aggregate strip (two InvoiceAnalytic cells in a Card with `mb: { xs: 3, md: 5 }`), layout-
 * transparent like the page's UrlPanel so the card's own margin is not reset as a direct
 * `.wrap > .screen > *` child. Also the strip's UrlSuspense fallback.
 */
export function FeedPurchasesStripSkeleton() {
  return (
    <Box sx={{ display: "contents" }}>
      <Box sx={{ mb: { xs: 3, md: 5 }, minWidth: 0 }}>
        <StatStripSkeleton count={FEED_STRIP_CELLS.length} />
      </Box>
    </Box>
  );
}

/** The ledger rows (their UrlSuspense fallback): head, one page of rows, the footer. */
export function FeedPurchasesRowsSkeleton({ columns = FEED_PURCHASE_COLUMNS.length, rows = DEFAULT_LIMIT }: { columns?: number; rows?: number }) {
  return <TableSkeleton bare header={false} columns={columns} rows={rows} />;
}

/** /procurement/feed-purchases: header + Record, the strip, the ledger card (delivery tabs, farm select + ⋮ toolbar, rows). */
export function FeedPurchasesSkeleton() {
  return (
    <PageSkeleton root="page-root">
      <PageHeaderSkeleton crumbLink={false} actionWidths={[FEED_RECORD_BUTTON_TWIN_WIDTH]} />
      <FeedPurchasesStripSkeleton />
      <TableSkeleton columns={FEED_PURCHASE_COLUMNS.length} rows={DEFAULT_LIMIT} header={false} tabs={<TabsSkeleton count={FEED_DELIVERY_TAB_COUNT} />} toolbar={<OrderToolbarSkeleton filters={FEED_TOOLBAR_FILTERS.length} search={false} menu />} />
    </PageSkeleton>
  );
}
