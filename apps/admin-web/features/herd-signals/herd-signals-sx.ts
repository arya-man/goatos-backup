import type { SxProps, Theme } from "@mui/material/styles";

// Herd Signals table/cell styling on the MUI theme (FIXJ2 J1 P0-1): what the page-scoped
// `.herd-signals-page` rules in mesha-theme.css / frame.css used to paint (`.resp` phone rows,
// `.delta`, `.mono`, `.faint`, `.num`, `.hs-selectable`, `.animcell`), as sx objects read from the
// theme palette and spacing. No class names; nothing here depends on a page wrapper.

/** Monospace figure (tag id, MAC, gateway id). */
export const HS_MONO = { fontFamily: "monospace", fontVariantNumeric: "tabular-nums" } as const;

/** Secondary line / placeholder text inside a cell. */
export const HS_FAINT = { color: "text.secondary" } as const;

/** The small second line under a cell's main value (display id, park, profile). */
export const HS_SUBLINE = {
  display: "block",
  typography: "caption",
  color: "text.secondary",
  whiteSpace: "nowrap",
  overflow: "hidden",
  textOverflow: "ellipsis",
  maxWidth: 320,
} as const;

const DELTA_COLOR: Record<"up" | "zero" | "warn", string> = {
  up: "success.main",
  zero: "text.secondary",
  warn: "warning.main",
};

/** Signed motion delta ("+140" green, "+0" muted, a spike amber). */
export function deltaSx(tone: "up" | "zero" | "warn") {
  return { fontWeight: "fontWeightBold", fontVariantNumeric: "tabular-nums", color: DELTA_COLOR[tone] } as const;
}

/** A clickable / selectable table row (opens the tag drawer, or arms the mapping action bar). */
export function selectableRowSx(selected = false): SxProps<Theme> {
  return (theme) => ({
    cursor: "pointer",
    transition: theme.transitions.create("background-color", { duration: theme.transitions.duration.shortest }),
    "&:hover": { bgcolor: "action.hover" },
    "&:focus-visible": { outline: `2px solid ${theme.vars.palette.primary.main}`, outlineOffset: -2 },
    ...(selected
      ? {
          bgcolor: "action.selected",
          "& > td:first-of-type": { boxShadow: `inset 3px 0 0 ${theme.vars.palette.primary.main}` },
        }
      : {}),
  });
}

/**
 * The responsive Herd Signals table: a wide desktop table (every column keeps its min width and the
 * table scrolls sideways inside the template Scrollbar), and below `md` each row becomes a two-column
 * field grid inside the card, every cell labelled by its `data-l` attribute (the header row is hidden).
 * Cells marked `data-wide` span both columns on a phone.
 */
export function respTableSx({ minWidth, columns }: { minWidth?: number; columns?: number[] } = {}): SxProps<Theme> {
  const widths = Object.fromEntries(
    (columns ?? []).map((width, index) => [`& :is(th, td):nth-of-type(${index + 1})`, { minWidth: width }]),
  );
  return (theme) => ({
    "& th": { whiteSpace: "nowrap" },
    "& td": { whiteSpace: "nowrap", verticalAlign: "middle" },
    "& :is(th, td)[data-num]": { textAlign: "right" },
    [theme.breakpoints.up("md")]: {
      ...(minWidth ? { minWidth } : {}),
      ...widths,
      // Sticky first column: the row's identity stays in view while the wide table scrolls.
      "& :is(th, td):first-of-type": { position: "sticky", left: 0, zIndex: 2 },
      "& tbody td:first-of-type": { bgcolor: "background.paper" },
      "& tbody tr:is(:hover, [aria-selected=true]) > td:first-of-type": {
        backgroundImage: `linear-gradient(${theme.vars.palette.action.hover}, ${theme.vars.palette.action.hover})`,
      },
    },
    [theme.breakpoints.down("md")]: {
      // Doubled class: outranks the shell's phone floor for every table (`.main table{min-width:540px}`),
      // which is right for a table that keeps its columns and wrong for these stacked field rows.
      "&&": { minWidth: 0, width: 1 },
      "& thead": { display: "none" },
      "& tbody": { display: "grid" },
      "& tbody tr": {
        display: "grid",
        gridTemplateColumns: "1fr 1fr",
        gap: theme.spacing(1, 1.5),
        px: 2,
        py: 1.5,
        borderBottom: `1px solid ${theme.vars.palette.divider}`,
      },
      "& tbody td": {
        border: 0,
        p: 0,
        display: "flex",
        flexDirection: "column",
        alignItems: "flex-start",
        gap: 0.25,
        whiteSpace: "normal",
        textAlign: "left",
        minWidth: 0,
      },
      "& tbody td[data-l]::before": {
        content: "attr(data-l)",
        ...theme.typography.overline,
        color: theme.vars.palette.text.secondary,
      },
      "& tbody td[data-wide], & tbody td[colspan]": { gridColumn: "1 / -1" },
    },
  });
}
