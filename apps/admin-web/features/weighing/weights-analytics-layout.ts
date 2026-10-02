import { DATE_RANGE_PICKER_MIN, FILTER_SELECT_MIN } from "@/components/app/filter-field-widths";

// Layout constants shared by the page and its loading.tsx, so the skeleton cannot drift from it.
/** The analytics tabs, in order ("general" is the default). */
export const WEIGHTS_TABS = ["general", "breed", "birth", "shed", "weight", "time", "load", "fcr"] as const;
/** Pens-table rows per page. */
export const WEIGHTS_DEFAULT_LIMIT = 25;
/**
 * /weighing/weights panel Grid (weights.tsx): the KpiWidget cards, breed-wise daily gain across the
 * full row (eleven breeds x four bands drew hairline bars and "Ananta… 407 mal…" labels in two
 * thirds of it), the sale-ready ring beside daily gain by breed, then sex and stage side by side.
 */
export const KIDS_GRID = {
  kpi: { xs: 12, sm: 6, lg: 3 },
  breedGain: { xs: 12 },
  ring: { xs: 12, md: 6, lg: 4 },
  breed: { xs: 12, md: 6, lg: 8 },
  sexStage: { xs: 12 },
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
  pens: 12,
} as const;
/** The General tab KPI cards (kids, total, average). */
export const GENERAL_KPI_COUNT = 3;
/** The page WorklistFilters bar from md up: park, the period range, weighing, origin, sex (the controls' own floors). */
export const WEIGHTS_FILTER_FIELDS = [FILTER_SELECT_MIN, DATE_RANGE_PICKER_MIN, FILTER_SELECT_MIN, FILTER_SELECT_MIN, FILTER_SELECT_MIN];
/** The General tab pens-table WorklistFilters (the staged compare operator + value + apply). */
export const PENS_FILTER_FIELDS = [FILTER_SELECT_MIN, FILTER_SELECT_MIN, FILTER_SELECT_MIN];
/** The pens table's columns (contract table "shed-weights"). */
export const PENS_TABLE_COLUMNS = 9;

// Loading-twin estimates of rendered sizes the page does not set itself (template chart cards size
// their own plots; header text widths come from copy). Measured on the served page at 1440 and 390.
/** /weighing/weights KPI captions: one line on a phone, two at lg 3 across. */
export const KIDS_KPI_CAPTION_LINES = { xs: 1, lg: 2 } as const;
/** /weighing/weights chart plot heights (ChartCardSkeleton `height`). */
export const KIDS_CHART_PLOT = { ring: 420, breedGain: 420, breed: 360, sexStage: 120 } as const;
/** General tab chart plot heights (ChartCardSkeleton `height`). */
export const GENERAL_CHART_PLOT = { ring: { xs: 358, lg: 382 }, weekly: 382, rank: 320, parkGain: 320 } as const;
/** The header's Download button width; "Kids — Weights" is short enough that Download sits beside it on a phone. */
export const WEIGHTS_HEADER = { downloadWidth: 112, kidsTitleWidth: 150 } as const;
