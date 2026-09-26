import type { SxProps, Theme } from '@mui/material/styles';

/** Webview rule: every control is at least 44px at phone width. */
export const TAP_MIN = 44;

export const phoneTapSx: SxProps<Theme> = (theme) => ({
  [theme.breakpoints.down('sm')]: { minWidth: TAP_MIN, minHeight: TAP_MIN },
});
