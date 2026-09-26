import type { CSSProperties, ReactNode } from "react";
import Tooltip from "@mui/material/Tooltip";
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

// Info popover for a column header / label. CSS-only (see `.tipwrap`/`.tip` in mesha-theme.css): the
// tooltip body is always rendered in the DOM and revealed on hover/focus, so it needs no client JS and
// stays testable. `label` is the accessible name of the "i" trigger; `children` is the popover body.
/**
 * The console's "i": a hover/focus note beside a title.
 *
 * ALIGN IS NOT DECORATION. The panel opens from the badge and is up to 300px wide, while `.card`
 * clips its overflow -- so an "i" sitting at the RIGHT end of a card header opened rightward into
 * the card's edge and was cut off mid-sentence. `align="end"` opens it leftward instead, which is
 * the same thing the table-header rule has always done for a tooltip in the last column.
 */
export function InfoTooltip({
  label,
  children,
  align = "start",
}: {
  label: string;
  children: ReactNode;
  align?: "start" | "end";
}) {
  return (
    <Tooltip title={children} placement="top" slotProps={{ tooltip: { sx: { maxWidth: 300 } } }}>
      <span className={align === "end" ? "tipwrap tip-end-anchor" : "tipwrap"}>
        <span className="ihelp" role="note" tabIndex={0} aria-label={label}>
          i
        </span>
      </span>
    </Tooltip>
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
