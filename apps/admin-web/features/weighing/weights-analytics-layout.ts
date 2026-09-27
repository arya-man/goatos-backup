// Layout constants shared by the page and its loading.tsx, so the skeleton cannot drift from it.
/** The analytics tabs, in order ("general" is the default). */
export const WEIGHTS_TABS = ["general", "breed", "birth", "shed", "weight", "time", "load", "fcr"] as const;
/** Pens-table rows per page. */
export const WEIGHTS_DEFAULT_LIMIT = 25;
/**
 * /weighing/weights panel Grid (weights.tsx): the KpiWidget cards, the sale-ready ring beside
 * breed-wise daily gain, then breed beside the stacked sex + stage cards.
 */
export const KIDS_GRID = {
  kpi: { xs: 12, sm: 6, lg: 3 },
  ring: { xs: 12, md: 6, lg: 4 },
  breedGain: { xs: 12, md: 6, lg: 8 },
  breed: { xs: 12, lg: 6 },
  sexStage: { xs: 12, lg: 6 },
} as const;
/** The /weighing/weights KPI cards (kids, total, average, park gain). */
export const KIDS_KPI_COUNT = 4;
/**
 * /weighing/analytics General tab Grid (GeneralTab): the KpiWidget cards, the sale-ready ring beside
 * weekly growth, the pen gain ranking beside the park gain balance card.
 */
export const GENERAL_GRID = {
  kpi: { xs: 12, md: 4 },
  ring: { xs: 12, md: 6, lg: 4 },
  weekly: { xs: 12, md: 6, lg: 8 },
  rank: { xs: 12, md: 6, lg: 8 },
  parkGain: { xs: 12, md: 6, lg: 4 },
} as const;
/** The General tab KPI cards (kids, total, average). */
export const GENERAL_KPI_COUNT = 3;
/**
 * The page WorklistFilters bar from md up: park, the period range, weighing, origin, sex
 * (160 = WorklistFilters' select floor; the range field is the 300 DateRangePicker).
 */
export const WEIGHTS_FILTER_FIELDS = [160, 300, 160, 160, 160];
/** The General tab pens-table WorklistFilters (the staged compare operator + value + apply). */
export const PENS_FILTER_FIELDS = [160, 160, 160];
