import type { SxProps, Theme } from "@mui/material/styles";
import { TAP_MIN } from "@/theme/tap-target";

/**
 * Shared sx for the Sales / Procurement boards (replaces the retired procurement-minimal.css).
 * Everything here is template layout on theme spacing/breakpoints — no colours, no raw sizes.
 */

/**
 * Phone media query (below the theme's `sm` breakpoint, 600px). A plain key rather than a
 * `(theme) => …` sx function: these sx objects are passed from SERVER components into MUI client
 * components, and a function cannot cross that boundary.
 */
export const PHONE = "@media (max-width:599.95px)";


/**
 * Hook class for a whole-cell link (a row cell that opens the row's drawer / detail). No stylesheet
 * defines it (guard `legacy-free-zone`): the look is `cellLinksSx` on the table, the template
 * table cell with the link filling it as a >= 44px tap box.
 */
export const CELL_LINK = "cell-link";

/** Table-level sx for `CELL_LINK` cells: link fills the cell padding, inherits the cell colour, 44px tap floor. */
export const cellLinksSx = {
  [`& td .${CELL_LINK}`]: {
    display: "block",
    boxSizing: "border-box",
    m: -1.5,
    p: 1.5,
    minHeight: TAP_MIN,
    minWidth: 0,
    maxWidth: "100%",
    color: "inherit",
    textDecoration: "none",
    overflowWrap: "anywhere",
    wordBreak: "break-word",
  },
} as const;

/** Wide table inside a card: scrolls sideways inside the card, never past its edge (template Scrollbar box). */
export const cardTableScrollSx: SxProps<Theme> = {
  overflowX: "auto",
  maxWidth: "100%",
  minWidth: 0,
  scrollbarGutter: "stable",
};

/**
 * Phone: a loads register reads as compact tap rows (template list rhythm, dashed dividers, ~90px each)
 * showing only the key fields; the rest of a load is one tap away in its drawer / detail. The
 * first cell's link covers the whole card, so the card is the tap target. Laptop keeps the table.
 *
 * `slots` places the visible cells: nth-child index -> grid position (+ optional secondary text).
 */
export function phoneLoadCardsSx(
  tableClass: string,
  slots: { nth: number; column: string; row: number; secondary?: boolean; alignEnd?: boolean }[],
): SxProps<Theme> {
  const t = `& table.${tableClass}`;
  const placed: Record<string, object> = {};
  for (const slot of slots) {
    placed[`${t} td:nth-of-type(${slot.nth})`] = {
      display: "block !important",
      gridColumn: slot.column,
      gridRow: slot.row,
      ...(slot.row === 1 && slot.nth === 1 ? { typography: "subtitle1" } : {}),
      ...(slot.secondary ? { color: "text.secondary", typography: "body2" } : {}),
      ...(slot.alignEnd ? { justifySelf: "end", textAlign: "right" } : {}),
    };
    if (slot.alignEnd) {
      placed[`${t} td:nth-of-type(${slot.nth}) .${CELL_LINK}`] = { display: "flex !important", alignItems: "center", justifyContent: "flex-end", gap: 1 };
    }
  }
  return {
    [PHONE]: {
      [t]: { minWidth: "0 !important", borderCollapse: "separate", borderSpacing: 0 },
      [`${t} thead`]: { display: "none !important" },
      [`${t}, ${t} tbody`]: { display: "block", width: "100%" },
      [`${t} tr`]: {
        position: "relative",
        display: "grid !important",
        gridTemplateColumns: "minmax(0,1fr) auto",
        alignItems: "center",
        columnGap: 1.5,
        rowGap: 0.5,
        px: 2.5,
        py: 2,
        // Divider rows inside the table card (template list rhythm), never a bordered card inside
        // the card. guard: no-card-in-card (template-fidelity-guards.test.mjs, sx half).
        borderBottom: "1px dashed",
        borderColor: "divider",
      },
      [`${t} td`]: {
        display: "none !important",
        position: "static !important",
        width: "auto",
        height: "auto !important",
        minWidth: "0 !important",
        p: "0 !important",
        border: 0,
        whiteSpace: "normal !important",
      },
      [`${t} td .${CELL_LINK}`]: {
        display: "block",
        textAlign: "inherit",
        minWidth: "0 !important",
        maxWidth: "100%",
        p: "0 !important",
        m: "0 !important",
        height: "auto !important",
        position: "static !important",
        whiteSpace: "normal !important",
      },
      [`${t} td:first-of-type .${CELL_LINK}::after`]: { content: '""', position: "absolute", inset: 0 },
      ...placed,
    },
  };
}
