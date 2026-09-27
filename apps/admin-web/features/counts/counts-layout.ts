// Layout constants shared by the Counts pages (herd-analytics, counts-breakdown, herd-register,
// mortality) and their loading twins (counts-skeletons.tsx), so a skeleton cannot drift from its page.
import { DATE_RANGE_PICKER_MIN, FILTER_SELECT_MIN } from "@/components/app/filter-field-widths";

/** /counts/analytics: the flow chart beside the sex ring, then the two mix columns. */
export const HA_GRID = { flow: { xs: 12, lg: 8 }, sex: { xs: 12, lg: 4 }, mix: { xs: 12, md: 6 } } as const;
/** /counts/breakdown: breed share beside stage x sex, then the full-width pen bars. */
export const BD_GRID = { breed: { xs: 12, md: 5 }, stageSex: { xs: 12, md: 7 }, pens: 12 } as const;
/** /counts/herd: a make-up KPI card's Grid size. */
export const HERD_KPI_SIZE = { xs: 12, sm: 4 } as const;
/** /counts/mortality: a rate card's Grid size (two to a row from lg). */
export const MORTALITY_RATE_SIZE = { xs: 12, lg: 6 } as const;
/** The window control on /counts/analytics and /counts/mortality (the DateRangePicker floor). */
export const COUNTS_WINDOW_WIDTH = { xs: "100%", sm: DATE_RANGE_PICKER_MIN } as const;
/** /counts/breakdown pen-table filters (five WorklistFilters selects). */
export const BD_FILTER_FIELDS = [FILTER_SELECT_MIN, FILTER_SELECT_MIN, FILTER_SELECT_MIN, FILTER_SELECT_MIN, FILTER_SELECT_MIN];

// Loading-twin estimates of what the pages do not set themselves (captions follow the served data,
// chart plots are the template cards' own, header widths follow copy), measured at 1440 and 390.
/** KPI caption lines per card, in page order (0 = no caption line). */
export const HA_KPI_CAPTIONS = [0, 1, 0, 0, 0, 0];
export const BD_KPI_CAPTIONS = [0, 1, 1, 1, 1, 1];
export const MORTALITY_KPI_CAPTIONS: (number | { xs: number; sm: number })[] = [0, { xs: 2, sm: 1 }, 1, 1, 0, 1];
export const HERD_KPI_COUNT = 3;
/** The window field's height: the medium outlined field, 44px below md (tap floor). */
export const COUNTS_WINDOW_HEIGHT = { xs: 44, md: 56 } as const;
/** Chart plot heights (ChartCardSkeleton `height`). */
export const HA_PLOT = { flow: 364, sex: 364, breed: 520, stage: 160, age: 100 } as const;
export const BD_PLOT = { breed: 364, stageSex: 364, pens: 360 } as const;
export const MORTALITY_PLOT = { monthly: 364 } as const;
/** Tables: breakdown pens, herd register, mortality rate cards (columns, rows). */
export const BD_TABLE = { columns: 8, rows: 10 } as const;
export const HERD_TABLE = { columns: 9, rows: 10, statusTabs: 5, searchFields: ["search", 120] as ("search" | number)[] } as const;
export const MORTALITY_RATE_TABLE = { cards: 4, columns: 5, rows: 5 } as const;
/** Header widths: analytics title (Export beside it on a phone), Export, the herd register's two actions. */
export const COUNTS_HEADER = { analyticsTitleWidth: 150, exportWidth: 89, herdActionWidths: [154, 154] } as const;
