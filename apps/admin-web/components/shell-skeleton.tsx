import Avatar from "@mui/material/Avatar";
import Box from "@mui/material/Box";
import MuiSkeleton from "@mui/material/Skeleton";
import { KpiRowSkeleton, PageHeaderSkeleton, PageSkeleton, TableSkeleton } from "@/components/app/skeletons";
import { TAP_MIN } from "@/theme/tap-target";

// MUI Minimal DashboardLayout geometry (layouts/dashboard/css-vars.ts + layouts/core/css-vars.ts):
// nav 300px / mini 88px (html[data-nav-rail="mini"]), hidden below lg (1200px); header 64px /
// 72px from lg; content gutters 16px / 24px from sm / 40px from lg. Object sx only (server module).
const MINI = 'html[data-nav-rail="mini"] &';
const NAV_W = 300;
const MINI_W = 88;

/**
 * The shell, drawn before the bootstrap contract arrives. Same geometry as the MUI Minimal
 * DashboardLayout the shell renders (layouts/dashboard), so the first paint does not jump when the
 * real shell hydrates; the page column is the same `main > [data-page-column]` box. No copy of its own beyond the wordmark tile.
 * FIXJ4: theme sx, no layouts/shell-skeleton.css.
 */
export function ShellSkeleton({ children }: { children?: React.ReactNode }) {
  return (
    <Box aria-busy="true" data-shell-skeleton sx={{
        position: "relative",
        minHeight: "100dvh",
        bgcolor: "background.default",
        // Skeletons paint at once on a hard navigation: no page-enter animation on the first shape.
        "& .wrap > *": { animation: "none", opacity: 1, transform: "none" },
      }}
    >
      <Box
        component="aside"
        aria-hidden="true"
        sx={{
          display: { xs: "none", lg: "block" },
          // Absolute in the full-height skeleton root (no fixed layer outside a portal).
          position: "absolute",
          top: 0,
          bottom: 0,
          left: 0,
          zIndex: 1,
          width: NAV_W,
          overflow: "hidden",
          bgcolor: "background.default",
          borderRight: 1,
          borderColor: "divider",
          [MINI]: { width: MINI_W },
        }}
      >
        <Box sx={{ pt: 2.5, pb: 1, pl: 3.5, [MINI]: { pl: 0, display: "flex", justifyContent: "center" } }}>
          <Avatar sx={{ bgcolor: "primary.main", color: "primary.contrastText", typography: "h6", fontWeight: 800, lineHeight: 1 }}>मे</Avatar>
        </Box>
        {[6, 8].map((rows, group) => (
          <Box key={group} sx={{ px: 2, pb: 1, [MINI]: { px: 0.5 } }}>
            <Box sx={{ pt: 2, px: 1.5, pb: 1, [MINI]: { display: "none" } }}>
              <MuiSkeleton variant="text" width={group ? 56 : 64} />
            </Box>
            {Array.from({ length: rows }, (_, i) => (
              <Box key={i} sx={{ display: "flex", alignItems: "center", gap: 2, minHeight: TAP_MIN, py: 0.5, px: 1.5, [MINI]: { justifyContent: "center", minHeight: 0, py: 2 } }}>
                <MuiSkeleton variant="rounded" width={24} height={24} />
                <Box component="span" sx={{ [MINI]: { display: "none" } }}>
                  <MuiSkeleton variant="text" width={(group ? 80 : 90) + (i % (group ? 4 : 3)) * (group ? 16 : 18)} />
                </Box>
              </Box>
            ))}
          </Box>
        ))}
      </Box>
      <Box sx={{ pl: { lg: `${NAV_W}px` }, [MINI]: { pl: { lg: `${MINI_W}px` } } }}>
        <Box
          aria-hidden="true"
          sx={{ display: "flex", alignItems: "center", justifyContent: "flex-end", gap: 0.75, height: { xs: 64, lg: 72 }, px: { xs: 2, sm: 3, lg: 5 } }}
        >
          <MuiSkeleton variant="circular" width={40} height={40} />
          <MuiSkeleton variant="circular" width={40} height={40} />
          <MuiSkeleton variant="circular" width={40} height={40} />
        </Box>
        <Box component="main" sx={{ flex: 1, minWidth: 0, overflow: "hidden", pt: 1, px: { xs: 2, sm: 3, lg: 5 } }}>
          <Box data-page-column="" sx={{ width: 1, minWidth: 0 }}>{children ?? <GenericPageSkeleton />}</Box>
        </Box>
      </Box>
    </Box>
  );
}

/** The page-column skeleton for a route that has no loading.tsx of its own: header, KPI deck, table card. */
export function GenericPageSkeleton() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton actions={1} />
      <KpiRowSkeleton count={4} />
      <TableSkeleton columns={5} rows={8} />
    </PageSkeleton>
  );
}
