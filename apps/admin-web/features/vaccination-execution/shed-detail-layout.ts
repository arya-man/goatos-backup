// Shared shape of /vaccination/execution/sheds/[shedId]: the page and its loading.tsx both read these,
// so the skeleton draws the same work-state cells, grid columns and drive-row columns the page renders.

/** Work-state cells, in page order (work_state_filter_chips keys). */
export const SHED_WORK_STATE_KEYS = ["due", "overdue", "proof_pending", "verification_pending", "rejected", "deferred", "missed", "blocked", "completed"] as const;
/** The work-state grid's columns per breakpoint. */
export const SHED_WORK_STATE_COLUMNS = { xs: 2, sm: 3 } as const;
/** Contract table "shed-drive-rows": 6 columns. */
export const SHED_DRIVE_ROW_COLUMNS = 6;
