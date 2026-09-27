// Layout constants shared by /counts/milk-preparation (milk-preparation.tsx) and its loading twin
// (milk-preparation-skeletons.tsx), so the skeleton cannot drift from the page.
/** The KPI cards, in order; `unit` = the card prints its unit as the KpiWidget caption line. */
export const MILK_KPIS = [
  { key: "sheds", unit: false },
  { key: "kids", unit: false },
  { key: "milk", unit: true },
  { key: "citric", unit: true },
] as const;
/** A KPI card's Grid item size (spacing-3 Grid). */
export const MILK_KPI_SIZE = { xs: 12, sm: 6, md: 3 } as const;
/** The farm verification states in the InvoiceAnalytic strip, in order. */
export const MILK_FARM_STATE_KEYS = ["not_submitted", "pending_verification", "verified", "rework"] as const;
/** Worklist rows per page when the URL names no page size. */
export const MILK_DEFAULT_PAGE_SIZE = 10;
/** The worklist's WorklistFilters from md up: the park select (WorklistFilters' 160 floor). */
export const MILK_FILTER_FIELDS = [160];
