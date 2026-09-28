import Box from "@mui/material/Box";
import { BlockSkeleton, FilterCardSkeleton, GridSkeleton, KpiRowSkeleton, ListCardSkeleton, OptionalSkeleton, PageHeaderSkeleton, PageSkeleton, StackSkeleton, TableSkeleton } from "@/components/app/skeletons";
import {
  LT_BLOCK_MB,
  LT_FILTER_IDS,
  LT_FILTER_MIN,
  LT_LIVE_INTERVAL_FIELD,
  LT_HEADER,
  LT_HEADER_MB,
  LT_KPI_KEYS,
  LT_MAIN_SIZE,
  LT_RAIL_ROWS,
  LT_RAIL_SIZE,
  LT_TABLES,
  LT_WELCOME_HEIGHT,
} from "./live-tracker-layout";

// Loading twins for /vaccination/live-tracker, composed ONLY from the shared skeleton blocks and the
// board's live-tracker-layout.ts (margins, filter list, Grid sizes, KPI keys), which the board reads too.

/**
 * The drive-day board body (below header + filter card): the KpiWidget row (only once the day has
 * counts), then operators / pens / combo table Cards beside the activity / attention / verification
 * rail. The route loading.tsx and the board's URL-keyed Suspense fallback both render it.
 */
export function LiveTrackerBodySkeleton() {
  return (
    <StackSkeleton spacing={3}>
      <OptionalSkeleton>
        <KpiRowSkeleton count={LT_KPI_KEYS.length} hint />
      </OptionalSkeleton>
      <GridSkeleton
        items={[
          {
            size: LT_MAIN_SIZE,
            node: (
              <StackSkeleton spacing={3}>
                <TableSkeleton columns={LT_TABLES.operators.columns} rows={LT_TABLES.operators.rows} subheader />
                <TableSkeleton columns={LT_TABLES.pens.columns} rows={LT_TABLES.pens.rows} headerAction />
                <TableSkeleton columns={LT_TABLES.combo.columns} rows={LT_TABLES.combo.rows} subheader pager={false} />
              </StackSkeleton>
            ),
          },
          {
            size: LT_RAIL_SIZE,
            node: (
              <StackSkeleton spacing={3}>
                <ListCardSkeleton rows={LT_RAIL_ROWS.activity} avatar={false} />
                <ListCardSkeleton rows={LT_RAIL_ROWS.attention} avatar={false} />
                <ListCardSkeleton rows={LT_RAIL_ROWS.verification} avatar={false} />
              </StackSkeleton>
            ),
          },
        ]}
      />
    </StackSkeleton>
  );
}

/**
 * The whole board as the route loading.tsx paints it: the board's block Box with margins (no grid
 * gap) — header (Full Schedule + Command Board; the parent crumb is text), the AppWelcome drive-day
 * row, the filter card (a select per LT_FILTER_IDS on the auto-fill grid), then the body.
 */
export function LiveTrackerPageSkeleton() {
  return (
    <PageSkeleton root="">
      <Box sx={{ mb: LT_HEADER_MB }}>
        <PageHeaderSkeleton crumbLink={false} crumbWidths={LT_HEADER.crumbWidths} actionWidths={[...LT_HEADER.actionWidths]} />
      </Box>
      <Box sx={{ mb: LT_BLOCK_MB }}>
        <BlockSkeleton height={LT_WELCOME_HEIGHT} />
      </Box>
      <Box sx={{ mb: LT_BLOCK_MB }}>
        <FilterCardSkeleton fields={[...LT_FILTER_IDS, ...Array.from({ length: LT_LIVE_INTERVAL_FIELD }, () => "interval")].map(() => LT_FILTER_MIN)} />
      </Box>
      <LiveTrackerBodySkeleton />
    </PageSkeleton>
  );
}
