// Layout constants shared by the source entry page and its loading twin.
/** The contract's `source_load_status` keys (backend adminui service), in order; the tabs are "all" plus these. */
export const SOURCE_LOAD_STATUS_KEYS = ["source_warmup", "health_pending", "pre_dispatch_pending", "dispatch_ready", "in_transit", "arrival_review", "accepted_intake", "rejected", "deferred", "blocked", "canceled"] as const;
/** The contract's `source-loads` table columns (backend adminui service), in order. */
export const SOURCE_LOAD_COLUMNS = ["load", "holding_farm_supplier", "purpose", "animals", "warmup", "tagging", "vaccination_hf", "health_selection", "status"] as const;
/** Rows the placeholder shows (one read renders every row). */
export const SOURCE_LOAD_SKELETON_ROWS = 10;
/** Placeholder width of the toolbar's Filters button (its label's width; skeleton only). */
export const SOURCE_FILTER_BUTTON_TWIN_WIDTH = 96;
