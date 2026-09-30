import type { CSSObject, Theme } from "@mui/material/styles";

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

/**
 * Phones (the deleted `.tablewrap` rule's 640px breakpoint): the identity column (first) and the
 * action column (last, when it holds the row's ⋮ / icon action) of EVERY page-content table stay
 * pinned while the row scrolls sideways, so a
 * reader who swiped to "Sheep Pox" still sees which pen the row is and can still reach its menu
 * (J3B N-P1-1; the rule was app/minimal-theme.css:226-229 and went with c4bd347ff).
 *
 * Template pattern (theme/core/components/table.tsx `stickyHeader`): an opaque paper cell body and
 * the neutral head surface, above the scrolling cells. Every selector sits inside `:where()`, so it
 * has zero specificity: the theme's head-cell fill and any cell sx a page sets itself still win.
 * A full-width `colSpan` cell (empty state, group caption) is never pinned. Opt a table out with
 * `data-sticky-edges="off"` (it pins its own columns).
 *
 * ONE global rule, in AppBaseline, for every table in the page content column: the shared
 * adapters (PagedRows, DataTable, DenseTable) and every raw MUI Table alike. It cannot live in a
 * component's `sx`: emotion prefixes the component class onto a `:where(&…)` selector
 * (`.css-x:where(.css-x > tbody …)`), which never matches a cell, and a plain `& > tbody > tr > td`
 * would outrank the cell sx a page sets (a `position: relative` overlay cell).
 * guard: sticky-edges-phone (sticky-first-column.test.mjs) + r2 `sticky-identity` (390 scan).
 */
export const PHONE_STICKY_EDGES_QUERY = "@media (max-width: 640px)";
/** The row-action hook (components/app/row-menu.tsx trigger). */
export const ROW_ACTION = "[data-row-menu]";

export function phoneStickyEdgeCells(table: string, theme: Theme): CSSObject {
  const paper = theme.vars?.palette.background.paper ?? theme.palette.background.paper;
  const neutral = theme.vars?.palette.background.neutral ?? theme.palette.background.neutral;
  // The last column pins only when it is the row's ACTION column (the ⋮ RowMenu or a bare
  // IconButton): a pinned data column beside a wide identity column covered the whole 358px
  // scroller at 390 (/counts/herd 252 + 124px), so nothing between them could be read.
  const actions = `:has(> tbody > tr > :last-child ${ROW_ACTION}, > tbody > tr > :last-child > .MuiIconButton-root)`;
  const cells = (edge: "first" | "last", part: "thead" | "tbody") =>
    `:where(${table}:not([data-sticky-edges="off"])${edge === "last" ? actions : ""} > ${part} > tr > :${edge}-child:not([colspan]))`;
  return {
    [`${cells("first", "tbody")}, ${cells("first", "thead")}`]: { position: "sticky", left: 0, zIndex: 2, backgroundColor: paper },
    [`${cells("last", "tbody")}, ${cells("last", "thead")}`]: { position: "sticky", right: 0, zIndex: 2, backgroundColor: paper },
    [`${cells("first", "thead")}, ${cells("last", "thead")}`]: { zIndex: 3, backgroundColor: neutral },
  };
}
