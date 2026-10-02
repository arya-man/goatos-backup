import { FILTER_SELECT_MIN } from "@/components/app/filter-field-widths";
// Layout constants shared by /counts/milk-preparation (milk-preparation.tsx) and its loading twin
// (milk-preparation-skeletons.tsx), so the skeleton cannot drift from the page.
/** The KPI cards, in order; `unit` = the card prints its unit as the KpiWidget caption line. */
// Every tile carries its sub-line (the contract's kpi.<key>.sub); units ride in the title
// ("Milk required (L)"), so no tile prints a lone "L" or "g" as its whole sub-line (D8).
export const MILK_KPIS = [
  { key: "sheds", unit: true },
  { key: "kids", unit: true },
  { key: "milk", unit: true },
  { key: "citric", unit: true },
] as const;
/** A KPI card's Grid item size (spacing-3 Grid). */
export const MILK_KPI_SIZE = { xs: 12, sm: 6, md: 3 } as const;
/** The farm verification states in the InvoiceAnalytic strip, in order. */
export const MILK_FARM_STATE_KEYS = ["not_submitted", "pending_verification", "verified", "rework"] as const;
/** Worklist rows per page when the URL names no page size. */
export const MILK_DEFAULT_PAGE_SIZE = 10;
/** The worklist's WorklistFilters from md up: the park select (the control's own floor). */
export const MILK_FILTER_FIELDS = [FILTER_SELECT_MIN];
/** The worklist table's columns (contract table "milk-preparation"). */
export const MILK_TABLE_COLUMNS = 10;
/** Loading-twin estimates (copy-driven): Export's width; the short title lets it sit beside the title on a phone. */
export const MILK_HEADER = { titleWidth: 150, exportWidth: 89 } as const;
