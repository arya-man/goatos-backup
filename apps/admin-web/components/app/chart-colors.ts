import type { Theme } from "@mui/material/styles";

/**
 * Categorical series colours from the locked Mesha palette (scheme-aware CSS variables, so a chart
 * follows light/dark with no re-render). Seven distinct hues: no second green and no error red, so
 * an ordinary category is never painted in the colours that mean "at risk" elsewhere (invariant
 * 1d06f72d0). Slots 5-7 are the Mesha teal, a neutral grey and the violet ink -- the same tail the
 * old kit ramp used, minus its pink `gain-under`, which reads as error.light in dark.
 */
export function chartColors(theme: Theme): string[] {
  const p = theme.vars.palette;
  return [p.primary.main, p.info.main, p.secondary.main, p.warning.main, "var(--teal)", p.grey[500], "var(--violet-ink)"];
}
