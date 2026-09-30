/**
 * Table `sx` that keeps the first column (the row's name) on screen while a wide table scrolls
 * sideways inside its card: template TableCell parts, sticky at left 0 on the row surface (paper)
 * and the head surface (neutral), above the scrolling cells. The legacy `.health-scroll` rule used
 * to do this and went with the legacy CSS (FIXJ2); guard: sticky-first-column
 * (components/app/table/sticky-first-column.test.mjs).
 */
export const STICKY_FIRST_COLUMN_SX = {
  "& tbody tr > :first-of-type": { position: "sticky", left: 0, zIndex: 2, bgcolor: "background.paper" },
  "& thead tr > :first-of-type": { position: "sticky", left: 0, zIndex: 3, bgcolor: "background.neutral" },
} as const;
