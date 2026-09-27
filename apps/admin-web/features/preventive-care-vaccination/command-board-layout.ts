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
