// Layout constants shared by /protocol-adherence, its loading.tsx and its UrlSuspense fallbacks.
/** The four KpiWidget course tiles' Grid item size (each with its caption sub-line). */
export const ADHERENCE_TILE_SIZE = { xs: 12, sm: 6, md: 3 } as const;
/** The ledger toolbar: the severity select (200), then the Filters button. */
export const ADHERENCE_SEVERITY_WIDTH = 200;
export const ADHERENCE_TOOLBAR_FIELDS = [ADHERENCE_SEVERITY_WIDTH];
export const ADHERENCE_FILTERS_BUTTON_WIDTH = 90;
/** The ledger CardHeader's own margin. */
export const ADHERENCE_LEDGER_HEADER_SX = { mb: 1 } as const;
