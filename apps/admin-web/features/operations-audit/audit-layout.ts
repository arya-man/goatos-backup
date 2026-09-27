// Layout constants shared by the page and its loading.tsx, so the skeleton cannot drift from it.
/** Activity trail rows per page. */
export const PAGE_SIZE = 25;

/** The status tabs over the trail (labels from the contract's `audit_status_tabs`). */
export const AUDIT_STATUS_TABS: Array<{ key: string; status?: string; result?: string; proofGaps?: boolean }> = [
  { key: "all_results" },
  { key: "awaiting", status: "verification_pending" },
  { key: "rejected", result: "rejected" },
  { key: "proof_gaps", proofGaps: true },
];

/** The operation-family LinkSelect's minimum width in the toolbar. */
export const AUDIT_FAMILY_SELECT_WIDTH = 200;
/** Placeholder width of the "Anomalies only" toggle button (its label's width; skeleton only). */
export const AUDIT_ANOMALIES_BUTTON_TWIN_WIDTH = 148;

/** The contract's `activity-trail` columns (backend adminui service), in order. */
export const AUDIT_TRAIL_COLUMNS = ["when", "operation", "operator", "action", "target", "result", "proof"] as const;

/** The operators card beside the advanced filters. */
export const AUDIT_SIDE_GRID = { operators: { xs: 12, md: 5 }, advanced: { xs: 12, md: 7 } } as const;
/** Operator rows the placeholder shows (the operators card and its UrlSuspense fallback). */
export const AUDIT_OPERATOR_SKELETON_ROWS = 4;
/** The collapsed advanced-filters accordion card (h6 + note summary). */
export const AUDIT_ADVANCED_COLLAPSED_HEIGHT = 78;
/** InvoiceAnalytic cells in the summary strip (actions in view, awaiting, proof coverage, flagged). */
export const AUDIT_STRIP_CELLS = 4;
