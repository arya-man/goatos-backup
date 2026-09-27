// Layout constants shared by the DLQ page and its loading.tsx, so the skeleton cannot drift from it.
/** The status tabs, in order (labels from the contract's `dlq_status_tabs`). */
export const DLQ_STATUS_KEYS = ["dead_letter", "failed", "discarded"] as const;
/** The event-type and topic fields' width from md (full width on a phone). */
export const DLQ_FIELD_WIDTH = 200;
/** Placeholder width of the filter form's Apply button (its label's width; skeleton only). */
export const DLQ_APPLY_TWIN_WIDTH = 72;
/** The contract's `dlq-events` columns (backend adminui service), in order. */
export const DLQ_EVENT_COLUMNS = ["event", "topic", "attempts", "replays", "last_error", "updated"] as const;
/** Rows the placeholder shows (one read renders every row). */
export const DLQ_SKELETON_ROWS = 10;
/** InvoiceAnalytic cells in the status strip (dead-letter, failed, discarded, replayed). */
export const DLQ_STRIP_CELLS = 4;
