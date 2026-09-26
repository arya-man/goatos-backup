import type { Theme } from "@mui/material/styles";

/**
 * Categorical series colours from the theme palette (scheme-aware CSS variables, so a chart
 * follows light/dark with no re-render). Neutral hues first; warning and error stay at the tail
 * so an ordinary category is not painted in the colours that mean "at risk" elsewhere.
 */
export function chartColors(theme: Theme): string[] {
  const p = theme.vars.palette;
  return [p.primary.main, p.info.main, p.secondary.main, p.warning.main, p.primary.darker, p.error.light, p.error.main];
}
