"use client";

/**
 * The eager Suspense fallback for the bell's lazy panel: header title + five rows, the shape the
 * panel resolves into, from the shared skeleton blocks. Kept tiny and separate from
 * `./notification-panel` on purpose -- it ships in the shell chunk of every route, so it imports only
 * the blocks and the panel's stylesheet (which the panel would load anyway) for the panel shell.
 */

import Box from "@mui/material/Box";
import { ListRowsSkeleton, SkeletonLine } from "@/components/app/skeletons";
import "./notification-panel.css";

export function NotificationSkeleton({ rows = 5 }: { rows?: number }) {
  return (
    <Box className="nc" data-notification-panel-fallback aria-busy="true">
      <Box className="nc-head">
        <SkeletonLine variant="subtitle1" width={120} />
      </Box>
      <Box className="nc-scroll">
        <NotificationRowsSkeleton rows={rows} />
      </Box>
    </Box>
  );
}

/** The list rows: 72px notification rows (avatar + title + meta). */
export function NotificationRowsSkeleton({ rows = 5 }: { rows?: number }) {
  return (
    <Box sx={{ px: 2, py: 1.75 }}>
      <ListRowsSkeleton rows={rows} trailing={false} spacing={3.5} />
    </Box>
  );
}
