import Box, { type BoxProps } from "@mui/material/Box";
import type { SxProps, Theme } from "@mui/material/styles";

/**
 * The page root of a route on the template (guard `page-root-sx`): the page column's 24px block
 * rhythm as theme sx, carried from the legacy frame.css `.wrap>.screen` grid (display grid, gap 3,
 * min-width 0, children with no outer margins) without the legacy `screen on` classes.
 * `data-page-root` is the hook the r2 audit (page-rhythm, skeleton block walk) and the skeleton
 * twin (`PageSkeleton root="page-root"`) key off. Server-safe: object sx only.
 */
export const PAGE_ROOT_SX = {
  display: "grid",
  gridTemplateColumns: "minmax(0, 1fr)",
  gap: 3,
  minWidth: 0,
  alignContent: "start",
  "& > *": { mt: 0, mb: 0, minWidth: 0 },
} as const;

export function PageRoot({ children, sx, ...other }: Omit<BoxProps, "className">) {
  return (
    <Box data-page-root="" sx={[PAGE_ROOT_SX, ...(Array.isArray(sx) ? sx : sx ? [sx as SxProps<Theme>] : [])]} {...other}>
      {children}
    </Box>
  );
}
