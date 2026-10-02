// Categorical chart palette: twelve hues, each stepped separately for the light and the dark chart
// surface (#FFFFFF / #1C252E). It exists because the theme has only six hues (primary, secondary,
// info, success, warning, error), and a chart of eleven breeds or feeds drawn from their shades put
// three greens, three blues and two purples side by side (PR #294 review D2/E4).
//
// The ORDER is the colour-blind safety mechanism, not decoration: it was chosen by enumerating
// orderings and keeping one where every ADJACENT pair clears OKLab deltaE >= 8 under simulated
// protan/deutan vision and >= 15 under normal vision, in BOTH modes, with every slot inside the
// mode's lightness band and above the chroma floor (dataviz validate_palette.js). Do not reorder or
// re-step one slot without re-running that validation; components/app/chart-palette.test.mjs
// re-checks the normal-vision floor.
//
// PR #294 K7 re-stepped indigo, olive (both schemes) and aqua (light): a legend of seven or more
// series puts NON-adjacent slots side by side, and violet/indigo and green/olive/aqua measured
// deltaE ~5-8. Same-family slots now clear deltaE 8.5 (11 for violet/indigo and green/olive), with
// every slot >= 3:1 against its chart surface; chart-palette.test.mjs pins both.
//
// `cssVar` is the scheme-following custom property theme/app-baseline.tsx emits for the slot.
//
// No error red: an ordinary category never wears the colour that means "at risk" elsewhere.
// Slot 1 is the brand green, so a one-series chart still reads as the product's own.
import { MESHA_TOKENS_DARK, MESHA_TOKENS_LIGHT } from './mesha-tokens';

// The hexes live in theme/mesha-tokens.ts (the one palette file, design:guard brand-lock).
export const CHART_CATEGORICAL = [
  { key: "series-green", cssVar: "--chart-series-green", light: MESHA_TOKENS_LIGHT["--chart-series-green"], dark: MESHA_TOKENS_DARK["--chart-series-green"] },
  { key: "series-blue", cssVar: "--chart-series-blue", light: MESHA_TOKENS_LIGHT["--chart-series-blue"], dark: MESHA_TOKENS_DARK["--chart-series-blue"] },
  { key: "series-orange", cssVar: "--chart-series-orange", light: MESHA_TOKENS_LIGHT["--chart-series-orange"], dark: MESHA_TOKENS_DARK["--chart-series-orange"] },
  { key: "series-violet", cssVar: "--chart-series-violet", light: MESHA_TOKENS_LIGHT["--chart-series-violet"], dark: MESHA_TOKENS_DARK["--chart-series-violet"] },
  { key: "series-magenta", cssVar: "--chart-series-magenta", light: MESHA_TOKENS_LIGHT["--chart-series-magenta"], dark: MESHA_TOKENS_DARK["--chart-series-magenta"] },
  { key: "series-olive", cssVar: "--chart-series-olive", light: MESHA_TOKENS_LIGHT["--chart-series-olive"], dark: MESHA_TOKENS_DARK["--chart-series-olive"] },
  { key: "series-teal", cssVar: "--chart-series-teal", light: MESHA_TOKENS_LIGHT["--chart-series-teal"], dark: MESHA_TOKENS_DARK["--chart-series-teal"] },
  { key: "series-yellow", cssVar: "--chart-series-yellow", light: MESHA_TOKENS_LIGHT["--chart-series-yellow"], dark: MESHA_TOKENS_DARK["--chart-series-yellow"] },
  { key: "series-indigo", cssVar: "--chart-series-indigo", light: MESHA_TOKENS_LIGHT["--chart-series-indigo"], dark: MESHA_TOKENS_DARK["--chart-series-indigo"] },
  { key: "series-aqua", cssVar: "--chart-series-aqua", light: MESHA_TOKENS_LIGHT["--chart-series-aqua"], dark: MESHA_TOKENS_DARK["--chart-series-aqua"] },
  { key: "series-plum", cssVar: "--chart-series-plum", light: MESHA_TOKENS_LIGHT["--chart-series-plum"], dark: MESHA_TOKENS_DARK["--chart-series-plum"] },
  { key: "series-brown", cssVar: "--chart-series-brown", light: MESHA_TOKENS_LIGHT["--chart-series-brown"], dark: MESHA_TOKENS_DARK["--chart-series-brown"] },
] as const;

export type ChartCategoricalKey = (typeof CHART_CATEGORICAL)[number]["key"];
