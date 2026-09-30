"use client";

import type { CSSProperties } from "react";
import LinearProgress from "@mui/material/LinearProgress";
import { varAlpha } from "minimal-shared/utils";

/**
 * Bare bar for table cells on the template's MUI `LinearProgress variant="determinate"`
 * (EcommerceSalesOverview item: 8px, radius 16, neutral grey-500 16% track, so 0% reads as an
 * empty track). `value` is 0–100; `color` (any Mesha token) paints the bar and defaults to primary.
 * Decorative by default (the cell shows the number). `className` / `fillClassName` are hook
 * classes for the root and the bar.
 */
export function ProgressBar({ value, color, className, fillClassName, style }: { value: number; color?: string; className?: string; fillClassName?: string; style?: CSSProperties }) {
  const pct = Math.max(0, Math.min(100, Number.isFinite(value) ? value : 0));
  return (
    <LinearProgress
      variant="determinate"
      value={pct}
      aria-hidden="true"
      className={className}
      classes={fillClassName ? { bar: fillClassName } : undefined}
      style={style}
      sx={[
        (theme) => ({
          height: "calc(1 * var(--spacing))",
          borderRadius: 2,
          bgcolor: varAlpha(theme.vars.palette.grey["500Channel"], 0.16),
          "& .MuiLinearProgress-bar": { borderRadius: "inherit", bgcolor: color ?? "var(--palette-primary-main)" },
        }),
      ]}
    />
  );
}
