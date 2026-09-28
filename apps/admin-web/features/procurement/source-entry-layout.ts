// Layout constants shared by the source entry page and its loading twin.
/** The contract's `source_load_status` keys (backend adminui service), in order; the status select lists them all. */
export const SOURCE_LOAD_STATUS_KEYS = ["source_warmup", "health_pending", "pre_dispatch_pending", "dispatch_ready", "in_transit", "arrival_review", "accepted_intake", "rejected", "deferred", "blocked", "canceled"] as const;
/** The contract's `source-loads` table columns (backend adminui service), in order. */
export const SOURCE_LOAD_COLUMNS = ["load", "holding_farm_supplier", "purpose", "animals", "warmup", "tagging", "vaccination_hf", "health_selection", "status"] as const;
/** Rows the placeholder shows (one read renders every row). */
export const SOURCE_LOAD_SKELETON_ROWS = 10;
/** Placeholder width of the toolbar's Filters button (its label's width; skeleton only). */
export const SOURCE_FILTER_BUTTON_TWIN_WIDTH = 96;
/** The work-state TABS: All + these in-flight stages (5 tabs, the template order list's count, so the
 *  strip fits the card at 1440 with no scroll arrows, TR3-P1-2). EVERY stage stays reachable through
 *  the status select in the toolbar; both drive the same `status` param. guard: source-entry-tabs-fit */
export const SOURCE_LOAD_TAB_STATES = ["health_pending", "dispatch_ready", "in_transit", "arrival_review"] as const;
