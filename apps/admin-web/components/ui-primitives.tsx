import type { CSSProperties, ReactNode } from "react";
import Box from "@mui/material/Box";
import type { Theme } from "@mui/material/styles";
import type { SystemStyleObject } from "@mui/system";

import { Label, type LabelColor } from "@/components/minimal/label";

// Mock palette tones, rendered as template Label colours (TONE_COLOR). Same literal set as the
// per-domain `Tone` aliases in features/*/process-integrity.ts and features/*/work-state.ts; those
// identical unions remain assignable to this one, so screens can keep their domain-typed tone
// values and still pass them here.
export type Tone = "ok" | "warn" | "dng" | "info" | "mut" | "pur" | "teal";

// Shared status pill: the template `Label` (soft), tone mapped onto the template palette.
export const TONE_COLOR: Record<Tone, LabelColor> = {
  ok: "success",
  warn: "warning",
  dng: "error",
  info: "info",
  mut: "default",
  pur: "secondary",
  teal: "info",
};

export function Tag({ tone, children, title }: { tone: Tone; children: ReactNode; title?: string }) {
  return (
    <Label variant="soft" color={TONE_COLOR[tone] ?? "default"} title={title ?? (typeof children === "string" ? children : undefined)}>
      {children}
    </Label>
  );
}

/**
 * One-line (or `two`-line) truncating text with the full value as its title. `className` keeps its
 * old modifier words (`inline`, `two`, `strong`) as props-by-name; no stylesheet class is rendered.
 */
export function ClipText({
  children,
  title,
  className = "",
  style,
}: {
  children: ReactNode;
  title?: string;
  className?: string;
  style?: CSSProperties;
}) {
  const inferredTitle = typeof children === "string" || typeof children === "number" ? String(children) : undefined;
  const words = className.split(/\s+/);
  const two = words.includes("two");
  return (
    <Box
      component="span"
      title={title ?? inferredTitle}
      data-truncate
      sx={{
          display: two ? "-webkit-box" : words.includes("inline") ? "inline-block" : "block",
          minWidth: 0,
          maxWidth: "100%",
          overflow: "hidden",
          textOverflow: "ellipsis",
          whiteSpace: two ? "normal" : "nowrap",
          verticalAlign: words.includes("inline") ? "bottom" : undefined,
          fontWeight: words.includes("strong") ? 700 : undefined,
          ...(two ? { WebkitLineClamp: 2, WebkitBoxOrient: "vertical" } : null),
          ...(style as SystemStyleObject<Theme> | undefined),
      }}
    >
      {children}
    </Box>
  );
}
