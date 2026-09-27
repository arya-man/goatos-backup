// Layout constants shared by the page and its loading.tsx, so the skeleton cannot drift from it
// (guard: verify-loading-mirror).
/** Queue rows per cursor page. */
export const QUEUE_LIMIT = 20;
/** The queue table's columns when the contract's verification-actions table has none yet. */
export const QUEUE_COLUMNS = 9;
/** Status strip cells: the backend "All" option plus To verify / Accepted / Rejected. */
export const STATUS_STRIP_CELLS = 4;
/** Status tabs on the board card (the contract's statuses). */
export const STATUS_TABS = STATUS_STRIP_CELLS - 1;
/** Header panel buttons (Analytics, Video Log, Randomization) at their rendered widths; they wrap on a phone. */
export const HEADER_ACTION_WIDTHS = [111, 114, 148];
/** The module select's width from md up. */
export const MODULE_FILTER_WIDTH = 180;
/** The capture-date field's rendered width from md up. */
export const CAPTURE_DATE_WIDTH = 300;
/** Board toolbar fields: the module select, then the capture-date field. */
export const BOARD_TOOLBAR_FIELDS = [MODULE_FILTER_WIDTH, CAPTURE_DATE_WIDTH];
