// Layout constants shared by the page and its loading.tsx, so the skeleton cannot drift from it.
/** Purchases per page when the contract offers no page sizes. */
export const DEFAULT_LIMIT = 25;
/** The "every delivery state" tab value (the other tabs are the contract's delivery statuses). */
export const DEFAULT_DELIVERY = "all";
/** The contract's `feed_purchase_delivery_statuses` keys (backend adminui service), in order. */
export const FEED_DELIVERY_STATUS_KEYS = ["purchased", "reached"] as const;
/** The delivery tabs: every state, then one per status. */
export const FEED_DELIVERY_TAB_COUNT = 1 + FEED_DELIVERY_STATUS_KEYS.length;
/** The contract's `feed-purchases` table columns (backend adminui service), in order. */
export const FEED_PURCHASE_COLUMNS = ["purchase_date", "farm", "feed_item", "batch_no", "quantity_kg", "delivery_status", "total_cost", "per_kg_cost", "vendor", "payment_status", "payment_balance"] as const;
/** The farm LinkSelect is the toolbar's one template select. */
export const FEED_TOOLBAR_SELECTS = 1;
/** InvoiceAnalytic cells in the aggregate strip (spend, quantity). */
export const FEED_STRIP_CELLS = 2;
