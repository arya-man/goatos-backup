import type { SxProps, Theme } from "@mui/material/styles";

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


/** Wide table inside a card: scrolls sideways inside the card, never past its edge (template Scrollbar box). */
export const cardTableScrollSx: SxProps<Theme> = {
  overflowX: "auto",
  maxWidth: "100%",
  minWidth: 0,
  scrollbarGutter: "stable",
};

/**
 * Phone: a loads register reads as compact tap cards (template list-card rhythm, ~100px each)
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
      placed[`${t} td:nth-of-type(${slot.nth}) .celllink`] = { display: "flex !important", alignItems: "center", justifyContent: "flex-end", gap: 1 };
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
        px: 2,
        py: 1.75,
        border: "1px solid",
        borderColor: "divider",
        borderRadius: "var(--r-xl)",
        bgcolor: "background.paper",
        boxShadow: "var(--shadow-card)",
      },
      [`${t} tr + tr`]: { mt: 1.5 },
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
      [`${t} td .celllink`]: {
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
      [`${t} td:first-of-type .celllink::after`]: { content: '""', position: "absolute", inset: 0, borderRadius: "var(--r-xl)" },
      ...placed,
    },
  };
}
