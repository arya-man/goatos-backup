"use client";

// Composed from the licensed MUI Minimal template: sections/invoice/invoice-analytic.tsx in the
// invoice list view's dashed-divider Stack.

import type { ReactNode } from "react";
import { varAlpha } from "minimal-shared/utils";
import Box from "@mui/material/Box";
import CircularProgress from "@mui/material/CircularProgress";
import Divider from "@mui/material/Divider";
import Stack from "@mui/material/Stack";
import Typography from "@mui/material/Typography";

import { Scrollbar } from "@/components/minimal/scrollbar";
import { CountUp } from "@/components/app/count-up";
import { toneVars, type KitTone } from "@/lib/tone";

export type StatStripCell = {
  key: string;
  label: ReactNode;
  /** A number counts up; anything else renders as-is (pass a formatted string from a Server Component). */
  value: number | ReactNode;
  /** Client callers only: a Server Component cannot hand a function to the client CountUp. */
  format?: (n: number) => string;
  /** One-line meta under the label, e.g. "animals sold · 12%". */
  meta?: ReactNode;
  icon?: ReactNode;
  tone?: KitTone;
  /** 0–100; fills the ring around the icon. */
  share?: number;
};

/**
 * A row of stats split by dashed dividers, inside the caller's card: the template invoice list's `InvoiceAnalytic` row
 * (ring + icon, subtitle1 label, body2 meta, subtitle2 figure), scrolling sideways inside its own
 * card on a phone. Numbers and labels are the caller's.
 */
/** Columns per row: every cell on one row up to four, then rows of three (6, 9) or four. */
export function stripColumns(count: number): number {
  if (count <= 4) return Math.max(count, 1);
  return count % 3 === 0 && count % 4 !== 0 ? 3 : 4;
}

function Cell({ cell }: { cell: StatStripCell }) {
  const color = toneVars(cell.tone ?? "primary").solid;
  const pct = cell.share == null ? 100 : Math.max(0, Math.min(100, cell.share));
  return (
    <Box sx={{ width: 1, gap: 2.5, minWidth: 200, px: 2, display: "flex", alignItems: "center", justifyContent: "center" }}>
      <Box
        sx={{ display: "flex", position: "relative", alignItems: "center", justifyContent: "center", flexShrink: 0 }}
        {...(cell.share == null ? {} : { role: "progressbar", "aria-valuenow": Math.round(pct), "aria-valuemin": 0, "aria-valuemax": 100 })}
      >
        {cell.icon ? (
          <Box component="span" sx={{ color, position: "absolute", display: "flex", "& svg": { width: 28, height: 28 } }}>
            {cell.icon}
          </Box>
        ) : null}
        <CircularProgress size={56} thickness={2} value={pct} variant="determinate" sx={{ color, opacity: 0.48 }} />
        <CircularProgress
          size={56}
          value={100}
          thickness={3}
          variant="determinate"
          sx={(theme) => ({ top: 0, left: 0, opacity: 0.48, position: "absolute", color: varAlpha(theme.vars.palette.grey["500Channel"], 0.16) })}
        />
      </Box>
      <Box sx={{ minWidth: 0 }}>
        <Typography variant="subtitle1" component="div">
          {cell.label}
        </Typography>
        {cell.meta ? (
          <Box component="span" sx={{ my: 0.5, display: "block", typography: "body2", color: "text.disabled" }}>
            {cell.meta}
          </Box>
        ) : null}
        <Box component="span" className="kit-kpi-value" sx={{ typography: "subtitle2" }}>
          {typeof cell.value === "number" ? <CountUp value={cell.value} format={cell.format} /> : cell.value}
        </Box>
      </Box>
    </Box>
  );
}

/**
 * A row of stats split by dashed dividers, inside the caller's card: the template invoice list's
 * `InvoiceAnalytic` row (ring + icon, subtitle1 label, body2 meta, subtitle2 figure), scrolling
 * sideways inside the card on a phone. More than four cells wrap into rows of four (or three when
 * that divides evenly) with the same dashed rules between rows, so no stat hides off-screen on a
 * laptop. Numbers and labels are the caller's.
 */
export function StatStrip({ cells, className, ariaLabel }: { cells: StatStripCell[]; className?: string; ariaLabel?: string }) {
  const cols = stripColumns(cells.length);
  if (cells.length > cols) {
    return (
      <Box
        className={className}
        role="group"
        aria-label={ariaLabel}
        data-cols={cols}
        sx={{
          display: "grid",
          gridTemplateColumns: { xs: "minmax(0,1fr)", md: `repeat(${cols}, minmax(0,1fr))` },
          "& > *": { py: 2, borderStyle: "dashed", borderColor: "divider", borderWidth: 0 },
          "& > *:not(:first-of-type)": { borderTopWidth: { xs: 1, md: 0 } },
          [`& > *:not(:nth-of-type(${cols}n + 1))`]: { borderLeftWidth: { md: 1 } },
          [`& > *:nth-of-type(n + ${cols + 1})`]: { borderTopWidth: { md: 1 } },
        }}
      >
        {cells.map((cell) => (
          <Box key={cell.key} sx={{ display: "flex", minWidth: 0, "& > *": { minWidth: 0 } }}>
            <Cell cell={cell} />
          </Box>
        ))}
      </Box>
    );
  }
  return (
    <Box className={className} role="group" aria-label={ariaLabel} data-cols={cols} sx={{ minWidth: 0 }}>
      <Scrollbar sx={{ minHeight: 108 }}>
        <Stack divider={<Divider orientation="vertical" flexItem sx={{ borderStyle: "dashed" }} />} sx={{ py: 2, flexDirection: "row" }}>
          {cells.map((cell) => (
            <Cell key={cell.key} cell={cell} />
          ))}
        </Stack>
      </Scrollbar>
    </Box>
  );
}
