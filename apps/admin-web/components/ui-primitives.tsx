import type { CSSProperties, ReactNode } from "react";

// Mock palette tones — all defined in app/mesha-theme.css as `.t-<tone>`. Same literal set as the
// per-domain `Tone` aliases in features/*/process-integrity.ts and features/*/work-state.ts; those
// identical unions remain assignable to this one, so screens can keep their domain-typed tone
// values and still pass them here.
export type Tone = "ok" | "warn" | "dng" | "info" | "mut" | "pur" | "teal";

// Shared status pill. Ported from the mock's `.tag` shape; the only presentational primitive that
// was copy-pasted into ~9 screens. `title` is optional so both the bare and tooltip call sites use
// the same component.
export function Tag({ tone, children, title }: { tone: Tone; children: ReactNode; title?: string }) {
  return (
    <span className={`tag t-${tone}`} title={title}>
      {children}
    </span>
  );
}

// Info popover for a column header / label. CSS-only (see `.tipwrap`/`.tip` in mesha-theme.css): the
// tooltip body is always rendered in the DOM and revealed on hover/focus, so it needs no client JS and
// stays testable. `label` is the accessible name of the "i" trigger; `children` is the popover body.
export function InfoTooltip({ label, children }: { label: string; children: ReactNode }) {
  return (
    <span className="tipwrap">
      <span className="ihelp" role="note" tabIndex={0} aria-label={label}>
        i
      </span>
      <span className="tip" role="tooltip">
        {children}
      </span>
    </span>
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
