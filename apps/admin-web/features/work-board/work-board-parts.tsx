"use client";

import Box from "@mui/material/Box";

import { Label } from "@/components/minimal/label";
import type { WorkBoardRow } from "@/lib/api/work-board-server";
import { barSegments, clockClass } from "./work-board-model";

// Shared Work Board presentation pieces (board card + issue dialog), built on template components
// and theme palette tokens only.

const SEGMENTS = [
  { key: "ok", color: "success.main" },
  { key: "rev", color: "info.main" },
  { key: "run", color: "warning.main" },
  { key: "brk", color: "error.main" },
] as const;

/** Stacked done / in-review / running / broken bar: the template LinearProgress track, split. */
export function WorkProgress({ row, size = "sm" }: { row: WorkBoardRow; size?: "sm" | "lg" }) {
  const seg = barSegments(row);
  return (
    <Box
      aria-hidden="true"
      sx={{
        display: "flex",
        overflow: "hidden",
        height: size === "lg" ? "var(--sp-1)" : "var(--sp-half)",
        borderRadius: "var(--r-pill)",
        bgcolor: "action.hover",
      }}
    >
      {SEGMENTS.map((s) => (
        <Box key={s.key} component="i" sx={{ display: "block", height: 1, width: `${seg[s.key]}%`, bgcolor: s.color, transition: (t) => t.transitions.create("width") }} />
      ))}
    </Box>
  );
}

const CLOCK_COLOR = { brk: "error", run: "warning" } as const;

/** The card clock ("due 14:00", "late 2h"): a template soft Label toned by the row's severity. */
export function ClockLabel({ row }: { row: WorkBoardRow }) {
  if (!row.clock_label) return null;
  const cls = clockClass(row);
  const tone = cls === "brk" || cls === "run" ? cls : "";
  return (
    <Label variant="soft" color={tone ? CLOCK_COLOR[tone] : "default"} sx={{ maxWidth: 1, height: "auto", minHeight: (theme) => theme.spacing(3), whiteSpace: "normal", fontVariantNumeric: "tabular-nums" }}>
      {row.clock_label}
    </Label>
  );
}
