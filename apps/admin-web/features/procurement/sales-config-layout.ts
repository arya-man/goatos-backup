// Layout constants shared by the page and its loading.tsx, so the skeleton cannot drift from it.
/** Deals per page when the contract offers no page sizes. */
export const DEFAULT_LIMIT = 25;

/** Sales Config tabs (template account view, `?tab=`), in order; "sales" is the default. */
export const SALES_CONFIG_TABS = ["sales", "loads", "items", "market", "reporters", "valuation"] as const;
export type SalesConfigTab = (typeof SALES_CONFIG_TABS)[number];
