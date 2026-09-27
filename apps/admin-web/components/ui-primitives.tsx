import type { CSSProperties, ReactNode } from "react";
import { Label, type LabelColor } from "@/components/minimal/label";

// Mock palette tones — all defined in app/mesha-theme.css as `.t-<tone>`. Same literal set as the
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
  const truncateStyle: CSSProperties = {
    display: className.split(/\s+/).includes("inline") ? "inline-block" : "block",
    minWidth: 0,
    maxWidth: "100%",
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: className.split(/\s+/).includes("two") ? undefined : "nowrap",
    ...style,
  };
  return (
    <span className={`cliptext${className ? ` ${className}` : ""}`} title={title ?? inferredTitle} style={truncateStyle} data-truncate>
      {children}
    </span>
  );
}
