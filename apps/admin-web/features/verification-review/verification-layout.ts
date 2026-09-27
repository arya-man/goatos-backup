// Layout constants shared by the page and its loading.tsx, so the skeleton cannot drift from it.
/** Queue rows per cursor page. */
export const QUEUE_LIMIT = 20;
/** Status strip cells: the backend "All" option plus To verify / Accepted / Rejected. */
export const STATUS_STRIP_CELLS = 4;
/** Status tabs on the board card. */
export const STATUS_TABS = 3;
/** Header panel buttons (Analytics, Video Log, Randomization) as rendered; they wrap on a phone. */
export const HEADER_ACTION_WIDTHS = [111, 114, 148];
/** Board toolbar fields: the module select (180) and the capture-date field (300). */
export const BOARD_TOOLBAR_FIELDS = [180, 300];
