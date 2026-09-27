// Layout constants shared by the /vaccination Command Board (command-board-view.tsx, command-board-cards.tsx)
// and its loading twin (vaccination-skeletons.tsx), so the skeleton cannot drift from the page.
/** Status filter chips on the Command Board card, in order (soft Chips; 44px tap floor below sm). */
export const STATUS_KEYS = ["verified", "awaiting", "rework", "overdue", "scheduled"] as const;
/** The KPI deck, in order (KpiWidget course cards with the backend explanation caption). */
export const CB_KPI_KEYS = [
  "targets",
  "missed",
  "verified",
  "awaiting_verification",
  "rework_needed",
  "overdue",
  "scheduled_ahead",
  "closed_without_dose",
] as const;
/** A KPI card's Grid item size (spacing-3 Grid). */
export const CB_KPI_SIZE = { xs: 12, sm: 6, md: 3 } as const;
/** The vaccine select's width floor from sm. */
export const CB_VACCINE_FIELD_MIN = 190;
/** The operator-day select's width floor from sm. */
export const CB_DRIVE_FIELD_MIN = 320;
/** Rows per page in every matrix card's pager. */
export const MATRIX_ROWS_PER_PAGE = 10;
/** The Command Board header action: Full Schedule. */
export const VACCINATION_HEADER_ACTION_WIDTHS = [140];

// Loading-twin estimates of rendered sizes the page does not set itself (copy- and data-driven),
// measured on the served page at 1440 and 390. Matrix column counts follow the day's vaccines.
/** The status chips' widths (copy) and height (44px tap floor below sm). */
export const CB_STATUS_CHIP_WIDTHS = [92, 150, 124, 120, 100];
export const CB_STATUS_CHIP_HEIGHT = { xs: 44, sm: 32 } as const;
/** KPI caption lines (one on a phone; at md 3 across the backend explanations take two lines on the first row, three on the second). */
export const CB_KPI_CAPTION_LINES = [2, 2, 2, 2, 3, 3, 3, 3].map((md) => ({ xs: 1, md }));
/** The matrix cards: columns (vaccines / pens on the day), legend Labels, the header InfoTip size, legend Label height. */
export const CB_MATRIX = {
  penVaccineColumns: 7,
  pendingColumns: 3,
  shedDoseColumns: 8,
  cohortColumns: 8,
  cohortFarmTabs: 2,
  penVaccineLegend: 5,
  shedDoseLegend: 6,
  infoTipSize: 44,
  legendChipHeight: 24,
} as const;
/** Inventory progress: metric tiles and per-vaccine rows / columns. */
export const INVENTORY = { tiles: 4, columns: 5, rows: 4 } as const;
/** Pen board toolbar: search + page-size fields, status and capacity pill counts; default columns. */
export const SHED_BOARD_TOOLBAR = { fields: [280, 72], statusPills: 7, capacityPills: 4, columns: 10 } as const;
/** Full schedule: operator-day columns, rows shown, month chips. */
export const FULL_SCHEDULE = { columns: 8, rows: 5, monthChips: 12 } as const;
