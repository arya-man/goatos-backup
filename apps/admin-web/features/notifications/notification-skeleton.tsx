"use client";

/**
 * The eager Suspense fallback for the bell's lazy panel: header title + five rows, the shape the
 * panel resolves into, from the shared skeleton blocks. Kept tiny and separate from
 * `./notification-panel` on purpose -- it ships in the shell chunk of every route, so it imports only
 * the blocks; the shell's layout is sx (the panel's actions row, then the rows).
 */

import Box from "@mui/material/Box";
import { ListRowsSkeleton, SkeletonLine } from "@/components/app/skeletons";

export function NotificationSkeleton({ rows = 5 }: { rows?: number }) {
  return (
    <Box data-notification-panel-fallback aria-busy="true" sx={{ display: "flex", flexDirection: "column", minHeight: 0 }}>
      <Box sx={{ display: "flex", alignItems: "center", justifyContent: "flex-end", px: 2.5, py: 1.5 }}>
        <SkeletonLine variant="subtitle1" width={120} />
      </Box>
      <NotificationRowsSkeleton rows={rows} />
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
