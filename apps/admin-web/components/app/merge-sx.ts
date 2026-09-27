import type { SxProps, Theme } from '@mui/material/styles';

/** A template sx value followed by a caller's declared override (template-derived sections). */
export function mergeSx(base: SxProps<Theme>, override?: SxProps<Theme>): SxProps<Theme> {
  const list = (v?: SxProps<Theme>) => (v == null ? [] : Array.isArray(v) ? v : [v]);
  return [...list(base), ...list(override)] as SxProps<Theme>;
}
