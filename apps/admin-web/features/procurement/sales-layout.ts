// Layout constants shared by the Sales pages and their loading twins (sales-skeletons.tsx), so a
// skeleton cannot drift from the page it stands in for (as vendor-layout.ts does for vendors).

/** Page size when the contract offers none: the Sold ledger, Buyer analytics and Farm born tables. */
export const SALES_DEFAULT_LIMIT = 25;
/** The Sold buyers board page (RankedTableCard) when the contract offers none. */
export const SOLD_BUYERS_PAGE_SIZE = 10;
/** Farm born by-pen breakdown page. */
export const FARM_BORN_PEN_PAGE_SIZE = 10;

/** The panels' MUI Grid item sizes. */
export const SALES_GRID = {
  /** Sold / Buyer analytics / Load wise: the wide column (KPIs 2x2, the monthly or per-load chart). */
  main: { xs: 12, lg: 8 },
  /** ... and the rail beside it (sold by weight, price by breed, the repeat ring, the load summary). */
  side: { xs: 12, lg: 4 },
  /** Sold: the headline KPI row (four tiles, or five with feed sold), hugging their content. */
  kpi: { xs: 12, sm: 6, lg: 3 },
  kpi5: { xs: 12, sm: 6, md: 4, xl: 12 / 5 },
  /** One card of a 2x2 KPI block. */
  half: { xs: 12, sm: 6 },
  /** Farm value: the three valuation cards. */
  valueKpi: { xs: 12, md: 4 },
  /** Farm value: the categories chart and the breakdown rows beside it. */
  valueChart: { xs: 12, md: 6, lg: 5 },
  valueRows: { xs: 12, md: 6, lg: 7 },
  /** Farm born: the four headline cards and the two breakdown columns. */
  bornKpi: { xs: 12, sm: 6, md: 3 },
  // Full width until xl: at 1440 the six-column breakdown tables wrapped every header onto 2-3 lines in
  // half a column (J2 P2-5; guard: farm-born-headers-one-line).
  bornHalf: { xs: 12, xl: 6 },
} as const;

/**
 * Plot heights of the TEMPLATE chart cards the panels render (verbatim components set these
 * themselves; measured on the loaded page at 390 / 1440). The Load wise charts use
 * GROUPED_COLUMNS_HEIGHT from components/grouped-columns instead, plus their legend rows.
 */
export const SALES_CHART_TWIN = {
  /** Load wise GroupedColumns legend rows per chart, in page order (series wrap to more rows on a phone). */
  loadLegends: [
    { xs: 127, lg: 27 },
    { xs: 127, lg: 27 },
    { xs: 76, lg: 27 },
    { xs: 25, lg: 27 },
    { xs: 76, lg: 27 },
  ],
  /** Sold "Month by month" (template Yearly sales, legend + chart). */
  soldMonthly: { xs: 329, lg: 380 },
  /** Farm value categories (template Banking expenses-categories). */
  valueCategories: { xs: 410, md: 460 },
  /** Farm value: the Over 35 kg error-margin toolbar card. */
  marginForm: { xs: 164, sm: 82 },
  /** Buyer analytics repeat ring (template Sale-by-gender). */
  buyerRing: 403,
} as const;
