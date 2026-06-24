import type { ReactNode } from "react";

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
