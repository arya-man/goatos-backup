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
// `cssVar` is the scheme-following custom property theme/app-baseline.tsx emits for the slot.
//
// No error red: an ordinary category never wears the colour that means "at risk" elsewhere.
// Slot 1 is the brand green, so a one-series chart still reads as the product's own.
export const CHART_CATEGORICAL = [
  { key: "series-green", cssVar: "--chart-series-green", light: "#54A02C", dark: "#5EA834" },
  { key: "series-blue", cssVar: "--chart-series-blue", light: "#2A78D6", dark: "#3987E5" },
  { key: "series-orange", cssVar: "--chart-series-orange", light: "#EB6834", dark: "#D95926" },
  { key: "series-violet", cssVar: "--chart-series-violet", light: "#4A3AA7", dark: "#9085E9" },
  { key: "series-magenta", cssVar: "--chart-series-magenta", light: "#E87BA4", dark: "#D55181" },
  { key: "series-olive", cssVar: "--chart-series-olive", light: "#8A9A00", dark: "#8F9A00" },
  { key: "series-teal", cssVar: "--chart-series-teal", light: "#0E98B5", dark: "#1B93B0" },
  { key: "series-yellow", cssVar: "--chart-series-yellow", light: "#EDA100", dark: "#C98500" },
  { key: "series-indigo", cssVar: "--chart-series-indigo", light: "#5468D4", dark: "#6F80E8" },
  { key: "series-aqua", cssVar: "--chart-series-aqua", light: "#1BAF7A", dark: "#199E70" },
  { key: "series-plum", cssVar: "--chart-series-plum", light: "#A8329A", dark: "#C45AB6" },
  { key: "series-brown", cssVar: "--chart-series-brown", light: "#A35D22", dark: "#B06A2C" },
] as const;

export type ChartCategoricalKey = (typeof CHART_CATEGORICAL)[number]["key"];
