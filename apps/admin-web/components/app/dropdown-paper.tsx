"use client";

import type { Ref } from "react";
import Paper, { type PaperProps } from "@mui/material/Paper";
import { listClasses } from "@mui/material/List";

/**
 * The template's dropdown paper (theme/core/components/popover.tsx: `paperStyles(theme, { dropdown })`)
 * for a panel that must stay IN PLACE instead of portalling into a MUI Popover:
 *   - lists anchored to a text field that keeps focus while the reader types (a Popover's focus
 *     trap would take the caret away), and
 *   - disclosure panels that open in flow inside a phone filter sheet (the sheet's own scroll must
 *     reach them; a portalled layer over a bottom sheet is a scroll trap).
 * Opaque (Mesha: nothing behind a menu reads through it), so no backdrop blur. Rows are MUI
 * MenuItems, which the theme styles with `menuItemStyles`. Position stays with the host.
 */
export function DropdownPaper({ sx, ref, ...other }: PaperProps & { ref?: Ref<HTMLDivElement> }) {
  return (
    <Paper
      ref={ref}
      elevation={0}
      sx={[
        (theme) => ({
          ...theme.mixins.paperStyles(theme, { dropdown: true, blur: 0, color: theme.vars.palette.background.paper }),
          [`& .${listClasses.root}`]: { paddingTop: 0, paddingBottom: 0 },
        }),
        ...(Array.isArray(sx) ? sx : [sx]),
      ]}
      {...other}
    />
  );
}
